package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/chunlea/marionette/pkg/store"
)

// Server replica registry and runner-connection routing.
//
// See migration 014 for why this exists. In short: a runner's control stream
// lives in one process's memory, so a second replica asked to send that runner
// a command has nowhere to write it. These queries record which process holds
// the stream, so the sender can forward instead of failing.
//
// Every statement here either writes the replica row or reads/writes the
// routing pointer on runners. runners has row level security; callers bind
// system access because routing acts for the deployment, not for a tenant.

const serverReplicaColumns = `id, advertise_addr, version, started_at, last_heartbeat_at`

// RegisterServerReplica records this process, refreshing the row if the id is
// already known.
//
// The upsert is not defensive dressing: a process that fails to write here is
// invisible to every other replica, so registration retries on the heartbeat
// tick and must be idempotent.
func (s *Store) RegisterServerReplica(ctx context.Context, replica *store.ServerReplica) error {
	query := `
		INSERT INTO server_replicas (id, advertise_addr, version, started_at, last_heartbeat_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE
		   SET advertise_addr = EXCLUDED.advertise_addr,
		       version = EXCLUDED.version,
		       last_heartbeat_at = NOW()
		RETURNING started_at, last_heartbeat_at`

	err := s.db.QueryRow(ctx, query, replica.ID, replica.AdvertiseAddr, replica.Version).
		Scan(&replica.StartedAt, &replica.LastHeartbeatAt)
	if err != nil {
		return handlePgError(err, "server_replica", replica.ID)
	}
	return nil
}

// HeartbeatServerReplica proves this process is still alive.
//
// A missing row is not an error the caller can act on differently, so it is
// reported as ErrNotFound and the registry re-registers: the row can genuinely
// be gone if another replica reaped this one during a long stall.
func (s *Store) HeartbeatServerReplica(ctx context.Context, id string) error {
	result, err := s.db.Exec(ctx,
		`UPDATE server_replicas SET last_heartbeat_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return handlePgError(err, "server_replica", id)
	}
	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "server_replica", ID: id}
	}
	return nil
}

// DeleteServerReplica removes a replica row. Every runner pointing at it has
// its pointer cleared by the foreign key, in the same statement.
func (s *Store) DeleteServerReplica(ctx context.Context, id string) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM server_replicas WHERE id = $1`, id); err != nil {
		return handlePgError(err, "server_replica", id)
	}
	return nil
}

// DeleteExpiredServerReplicas reaps replicas that stopped heartbeating.
//
// It needs no claim: the delete is idempotent, so every replica may run it and
// the losers simply remove nothing.
func (s *Store) DeleteExpiredServerReplicas(ctx context.Context, olderThan time.Duration) (int, error) {
	if olderThan <= 0 {
		return 0, fmt.Errorf("replica expiry must be positive, got %s", olderThan)
	}

	result, err := s.db.Exec(ctx,
		`DELETE FROM server_replicas WHERE last_heartbeat_at < NOW() - make_interval(secs => $1)`,
		olderThan.Seconds())
	if err != nil {
		return 0, handlePgError(err, "server_replica", "")
	}
	return int(result.RowsAffected()), nil
}

// ListServerReplicas returns every registered replica, most recently seen
// first.
func (s *Store) ListServerReplicas(ctx context.Context) ([]*store.ServerReplica, error) {
	query := fmt.Sprintf(
		`SELECT %s FROM server_replicas ORDER BY last_heartbeat_at DESC`, serverReplicaColumns)

	rows, err := s.db.Query(ctx, query)
	if err != nil {
		return nil, handlePgError(err, "server_replica", "")
	}
	defer rows.Close()

	var replicas []*store.ServerReplica
	for rows.Next() {
		var r store.ServerReplica
		if err := rows.Scan(&r.ID, &r.AdvertiseAddr, &r.Version, &r.StartedAt, &r.LastHeartbeatAt); err != nil {
			return nil, fmt.Errorf("scanning server replica: %w", err)
		}
		replicas = append(replicas, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating server replicas: %w", err)
	}
	return replicas, nil
}

// BindRunnerConnection records that replicaID holds runnerID's control stream.
//
// Unconditional on purpose. The write happens immediately after a stream
// registered successfully, and that makes this process the holder by
// definition; a conditional write would leave the pointer on a replica whose
// stream the runner has already abandoned.
func (s *Store) BindRunnerConnection(ctx context.Context, runnerID, replicaID string) error {
	result, err := s.db.Exec(ctx, `
		UPDATE runners
		   SET connected_replica_id = $2,
		       connected_at = NOW(),
		       updated_at = NOW()
		 WHERE id = $1`, runnerID, replicaID)
	if err != nil {
		return handlePgError(err, "runner", runnerID)
	}
	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "runner", ID: runnerID}
	}
	return nil
}

// ReleaseRunnerConnection clears the pointer only if replicaID still holds it.
//
// The condition is the fence. A stream that flapped from replica A to replica B
// leaves A running its deferred disconnect after B has already bound the
// pointer; an unconditional clear would delete the routing entry that had just
// become correct, and every command to that runner would fail until the next
// reconnect.
func (s *Store) ReleaseRunnerConnection(ctx context.Context, runnerID, replicaID string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE runners
		   SET connected_replica_id = NULL,
		       connected_at = NULL,
		       updated_at = NOW()
		 WHERE id = $1 AND connected_replica_id = $2`, runnerID, replicaID)
	if err != nil {
		return handlePgError(err, "runner", runnerID)
	}
	// Affecting no rows is the normal outcome of losing the fence, not a
	// failure: somebody else holds the runner now and it is theirs to release.
	return nil
}

// GetRunnerConnection resolves a runner to the replica holding its stream.
//
// The join is inner: a pointer to a replica row that no longer exists cannot
// happen (the foreign key clears it), and a runner nothing holds is
// ErrNotFound rather than a row with empty fields.
func (s *Store) GetRunnerConnection(ctx context.Context, runnerID string) (*store.RunnerConnection, error) {
	query := `
		SELECT r.id, sr.id, sr.advertise_addr, r.connected_at, sr.last_heartbeat_at
		  FROM runners r
		  JOIN server_replicas sr ON sr.id = r.connected_replica_id
		 WHERE r.id = $1`

	var conn store.RunnerConnection
	var connectedAt *time.Time
	err := s.db.QueryRow(ctx, query, runnerID).Scan(
		&conn.RunnerID, &conn.ReplicaID, &conn.AdvertiseAddr, &connectedAt, &conn.LastHeartbeatAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "runner_connection", ID: runnerID}
		}
		return nil, handlePgError(err, "runner_connection", runnerID)
	}
	if connectedAt != nil {
		conn.ConnectedAt = *connectedAt
	}
	return &conn, nil
}
