package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
)

// APIKey column list for SELECT queries.
//
//nolint:gosec // G101: This is a column list, not hardcoded credentials
const apiKeyColumns = `id, name, key_hash, key_prefix, hash_version, scopes,
	tenant_id, labels, annotations, created_at, created_by, last_used_at,
	expires_at, revoked_at, revoke_reason`

// RunnerToken column list for SELECT queries.
//
//nolint:gosec // G101: This is a column list, not hardcoded credentials
const runnerTokenColumns = `id, token_hash, token_prefix, hash_version, runner_id, pool_name,
	status, previous_token_hash, rotation_deadline, tenant_id, labels, created_at, created_by,
	last_used_at, expires_at, revoked_at, revoke_reason`

// =============================================================================
// APIKey CRUD
// =============================================================================

// CreateAPIKey creates a new API key.
func (s *Store) CreateAPIKey(ctx context.Context, key *store.APIKey) error {
	return createAPIKey(ctx, s.pool, key)
}

// CreateAPIKey creates a new API key within a transaction.
func (t *Tx) CreateAPIKey(ctx context.Context, key *store.APIKey) error {
	return createAPIKey(ctx, t.tx, key)
}

func createAPIKey(ctx context.Context, q querier, key *store.APIKey) error {
	if key.ID == "" {
		key.ID = id.APIKey()
	}

	query := `
		INSERT INTO api_keys (
			id, name, key_hash, key_prefix, hash_version, scopes,
			tenant_id, labels, annotations, created_at, created_by, last_used_at,
			expires_at, revoked_at, revoke_reason
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), $10, $11, $12, $13, $14
		)
		RETURNING created_at`

	err := q.QueryRow(ctx, query,
		key.ID, key.Name, key.KeyHash, key.KeyPrefix, key.HashVersion, key.Scopes,
		key.TenantID, emptyJSONObject(key.Labels), emptyJSONObject(key.Annotations),
		key.CreatedBy, key.LastUsedAt, key.ExpiresAt, key.RevokedAt, key.RevokeReason,
	).Scan(&key.CreatedAt)

	if err != nil {
		return handlePgError(err, "api_key", key.Name)
	}
	return nil
}

// GetAPIKey retrieves an API key by ID.
func (s *Store) GetAPIKey(ctx context.Context, keyID string) (*store.APIKey, error) {
	return getAPIKey(ctx, s.pool, keyID)
}

// GetAPIKey retrieves an API key by ID within a transaction.
func (t *Tx) GetAPIKey(ctx context.Context, keyID string) (*store.APIKey, error) {
	return getAPIKey(ctx, t.tx, keyID)
}

func getAPIKey(ctx context.Context, q querier, keyID string) (*store.APIKey, error) {
	query := fmt.Sprintf(`SELECT %s FROM api_keys WHERE id = $1`, apiKeyColumns)
	row := q.QueryRow(ctx, query, keyID)
	return scanAPIKey(row, keyID)
}

// GetAPIKeyByHash retrieves an API key by its hash.
func (s *Store) GetAPIKeyByHash(ctx context.Context, hash string) (*store.APIKey, error) {
	return getAPIKeyByHash(ctx, s.pool, hash)
}

// GetAPIKeyByHash retrieves an API key by its hash within a transaction.
func (t *Tx) GetAPIKeyByHash(ctx context.Context, hash string) (*store.APIKey, error) {
	return getAPIKeyByHash(ctx, t.tx, hash)
}

func getAPIKeyByHash(ctx context.Context, q querier, hash string) (*store.APIKey, error) {
	query := fmt.Sprintf(`SELECT %s FROM api_keys WHERE key_hash = $1`, apiKeyColumns)
	row := q.QueryRow(ctx, query, hash)
	return scanAPIKey(row, hash)
}

// ListAPIKeys retrieves API keys with optional filtering.
func (s *Store) ListAPIKeys(ctx context.Context, opts store.ListAPIKeysOptions) (*store.ListResult[store.APIKey], error) {
	return listAPIKeys(ctx, s.pool, opts)
}

// ListAPIKeys retrieves API keys within a transaction.
func (t *Tx) ListAPIKeys(ctx context.Context, opts store.ListAPIKeysOptions) (*store.ListResult[store.APIKey], error) {
	return listAPIKeys(ctx, t.tx, opts)
}

func listAPIKeys(ctx context.Context, q querier, opts store.ListAPIKeysOptions) (*store.ListResult[store.APIKey], error) {
	var conditions []string
	var args []any
	argNum := 1

	if !opts.IncludeRevoked {
		conditions = append(conditions, "revoked_at IS NULL")
	}
	// TODO: Add label filtering with JSONB operators

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := defaultLimit(opts.Limit)
	orderBy, err := apiKeySortColumns.orderClause(opts.OrderBy, opts.OrderDesc)
	if err != nil {
		return nil, err
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM api_keys %s", whereClause)
	var totalCount int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("counting api_keys: %w", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT %s FROM api_keys %s
		ORDER BY %s
		LIMIT $%d`,
		apiKeyColumns, whereClause, orderBy, argNum)
	dataArgs := append(args, limit+1) //nolint:gocritic // intentionally creating new slice

	rows, err := q.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying api_keys: %w", err)
	}
	defer rows.Close()

	var keys []*store.APIKey
	for rows.Next() {
		key, err := scanAPIKeyFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning api_key: %w", err)
		}
		keys = append(keys, key)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating api_keys: %w", err)
	}

	hasMore := len(keys) > limit
	if hasMore {
		keys = keys[:limit]
	}

	return &store.ListResult[store.APIKey]{
		Items:      keys,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}

// UpdateAPIKey updates API key fields.
func (s *Store) UpdateAPIKey(ctx context.Context, keyID string, updates store.APIKeyUpdates) error {
	return updateAPIKey(ctx, s.pool, keyID, updates)
}

// UpdateAPIKey updates API key fields within a transaction.
func (t *Tx) UpdateAPIKey(ctx context.Context, keyID string, updates store.APIKeyUpdates) error {
	return updateAPIKey(ctx, t.tx, keyID, updates)
}

func updateAPIKey(ctx context.Context, q querier, keyID string, updates store.APIKeyUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argNum))
		args = append(args, *updates.Name)
		argNum++
	}
	if updates.Scopes != nil {
		setClauses = append(setClauses, fmt.Sprintf("scopes = $%d", argNum))
		args = append(args, updates.Scopes)
		argNum++
	}
	if updates.Labels != nil {
		setClauses = append(setClauses, fmt.Sprintf("labels = $%d", argNum))
		args = append(args, updates.Labels)
		argNum++
	}
	if updates.Annotations != nil {
		setClauses = append(setClauses, fmt.Sprintf("annotations = $%d", argNum))
		args = append(args, updates.Annotations)
		argNum++
	}
	if updates.LastUsedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("last_used_at = $%d", argNum))
		args = append(args, *updates.LastUsedAt)
		argNum++
	}
	if updates.RevokedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("revoked_at = $%d", argNum))
		args = append(args, *updates.RevokedAt)
		argNum++
	}
	if updates.RevokeReason != nil {
		setClauses = append(setClauses, fmt.Sprintf("revoke_reason = $%d", argNum))
		args = append(args, *updates.RevokeReason)
		argNum++
	}

	if len(setClauses) == 0 {
		return nil
	}

	query := fmt.Sprintf(`UPDATE api_keys SET %s WHERE id = $%d`,
		strings.Join(setClauses, ", "), argNum)
	args = append(args, keyID)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "api_key", keyID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "api_key", ID: keyID}
	}

	return nil
}

// DeleteAPIKey deletes an API key.
func (s *Store) DeleteAPIKey(ctx context.Context, keyID string) error {
	return deleteAPIKey(ctx, s.pool, keyID)
}

// DeleteAPIKey deletes an API key within a transaction.
func (t *Tx) DeleteAPIKey(ctx context.Context, keyID string) error {
	return deleteAPIKey(ctx, t.tx, keyID)
}

func deleteAPIKey(ctx context.Context, q querier, keyID string) error {
	query := `DELETE FROM api_keys WHERE id = $1`
	result, err := q.Exec(ctx, query, keyID)
	if err != nil {
		return handlePgError(err, "api_key", keyID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "api_key", ID: keyID}
	}

	return nil
}

func scanAPIKey(row pgx.Row, identifier string) (*store.APIKey, error) {
	var k store.APIKey
	err := row.Scan(
		&k.ID, &k.Name, &k.KeyHash, &k.KeyPrefix, &k.HashVersion, &k.Scopes,
		&k.TenantID, &k.Labels, &k.Annotations, &k.CreatedAt, &k.CreatedBy, &k.LastUsedAt,
		&k.ExpiresAt, &k.RevokedAt, &k.RevokeReason,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "api_key", ID: identifier}
		}
		return nil, fmt.Errorf("scanning api_key: %w", err)
	}
	return &k, nil
}

func scanAPIKeyFromRows(rows pgx.Rows) (*store.APIKey, error) {
	var k store.APIKey
	err := rows.Scan(
		&k.ID, &k.Name, &k.KeyHash, &k.KeyPrefix, &k.HashVersion, &k.Scopes,
		&k.TenantID, &k.Labels, &k.Annotations, &k.CreatedAt, &k.CreatedBy, &k.LastUsedAt,
		&k.ExpiresAt, &k.RevokedAt, &k.RevokeReason,
	)
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// =============================================================================
// RunnerToken CRUD
// =============================================================================

// CreateRunnerToken creates a new runner token.
func (s *Store) CreateRunnerToken(ctx context.Context, token *store.RunnerToken) error {
	return createRunnerToken(ctx, s.pool, token)
}

// CreateRunnerToken creates a new runner token within a transaction.
func (t *Tx) CreateRunnerToken(ctx context.Context, token *store.RunnerToken) error {
	return createRunnerToken(ctx, t.tx, token)
}

func createRunnerToken(ctx context.Context, q querier, token *store.RunnerToken) error {
	if token.ID == "" {
		token.ID = id.RunnerToken()
	}

	query := `
		INSERT INTO runner_tokens (
			id, token_hash, token_prefix, hash_version, runner_id, pool_name,
			status, previous_token_hash, rotation_deadline, tenant_id, labels, created_at, created_by,
			last_used_at, expires_at, revoked_at, revoke_reason
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), $12, $13, $14, $15, $16
		)
		RETURNING created_at`

	err := q.QueryRow(ctx, query,
		token.ID, token.TokenHash, token.TokenPrefix, token.HashVersion, token.RunnerID, token.PoolName,
		token.Status, token.PreviousTokenHash, token.RotationDeadline, token.TenantID, emptyJSONObject(token.Labels),
		token.CreatedBy, token.LastUsedAt, token.ExpiresAt, token.RevokedAt, token.RevokeReason,
	).Scan(&token.CreatedAt)

	if err != nil {
		return handlePgError(err, "runner_token", token.ID)
	}
	return nil
}

// GetRunnerToken retrieves a runner token by ID.
func (s *Store) GetRunnerToken(ctx context.Context, tokenID string) (*store.RunnerToken, error) {
	return getRunnerToken(ctx, s.pool, tokenID)
}

// GetRunnerToken retrieves a runner token by ID within a transaction.
func (t *Tx) GetRunnerToken(ctx context.Context, tokenID string) (*store.RunnerToken, error) {
	return getRunnerToken(ctx, t.tx, tokenID)
}

func getRunnerToken(ctx context.Context, q querier, tokenID string) (*store.RunnerToken, error) {
	query := fmt.Sprintf(`SELECT %s FROM runner_tokens WHERE id = $1`, runnerTokenColumns)
	row := q.QueryRow(ctx, query, tokenID)
	return scanRunnerToken(row, tokenID)
}

// GetRunnerTokenByHash retrieves a runner token by its hash.
func (s *Store) GetRunnerTokenByHash(ctx context.Context, hash string) (*store.RunnerToken, error) {
	return getRunnerTokenByHash(ctx, s.pool, hash)
}

// GetRunnerTokenByHash retrieves a runner token by its hash within a transaction.
func (t *Tx) GetRunnerTokenByHash(ctx context.Context, hash string) (*store.RunnerToken, error) {
	return getRunnerTokenByHash(ctx, t.tx, hash)
}

func getRunnerTokenByHash(ctx context.Context, q querier, hash string) (*store.RunnerToken, error) {
	query := fmt.Sprintf(`SELECT %s FROM runner_tokens WHERE token_hash = $1`, runnerTokenColumns)
	row := q.QueryRow(ctx, query, hash)
	return scanRunnerToken(row, hash)
}

// ListRunnerTokens retrieves runner tokens with optional filtering.
func (s *Store) ListRunnerTokens(ctx context.Context, opts store.ListRunnerTokensOptions) (*store.ListResult[store.RunnerToken], error) {
	return listRunnerTokens(ctx, s.pool, opts)
}

// ListRunnerTokens retrieves runner tokens within a transaction.
func (t *Tx) ListRunnerTokens(ctx context.Context, opts store.ListRunnerTokensOptions) (*store.ListResult[store.RunnerToken], error) {
	return listRunnerTokens(ctx, t.tx, opts)
}

func listRunnerTokens(ctx context.Context, q querier, opts store.ListRunnerTokensOptions) (*store.ListResult[store.RunnerToken], error) {
	var conditions []string
	var args []any
	argNum := 1

	if opts.PoolName != nil {
		conditions = append(conditions, fmt.Sprintf("pool_name = $%d", argNum))
		args = append(args, *opts.PoolName)
		argNum++
	}
	if opts.RunnerID != nil {
		conditions = append(conditions, fmt.Sprintf("runner_id = $%d", argNum))
		args = append(args, *opts.RunnerID)
		argNum++
	}
	if len(opts.Status) > 0 {
		conditions = append(conditions, fmt.Sprintf("status = ANY($%d)", argNum))
		args = append(args, opts.Status)
		argNum++
	}
	if !opts.IncludeRevoked {
		conditions = append(conditions, "revoked_at IS NULL")
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := defaultLimit(opts.Limit)
	orderBy, err := runnerTokenSortColumns.orderClause(opts.OrderBy, opts.OrderDesc)
	if err != nil {
		return nil, err
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM runner_tokens %s", whereClause)
	var totalCount int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("counting runner_tokens: %w", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT %s FROM runner_tokens %s
		ORDER BY %s
		LIMIT $%d`,
		runnerTokenColumns, whereClause, orderBy, argNum)
	dataArgs := append(args, limit+1) //nolint:gocritic // intentionally creating new slice

	rows, err := q.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying runner_tokens: %w", err)
	}
	defer rows.Close()

	var tokens []*store.RunnerToken
	for rows.Next() {
		token, err := scanRunnerTokenFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning runner_token: %w", err)
		}
		tokens = append(tokens, token)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating runner_tokens: %w", err)
	}

	hasMore := len(tokens) > limit
	if hasMore {
		tokens = tokens[:limit]
	}

	return &store.ListResult[store.RunnerToken]{
		Items:      tokens,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}

// UpdateRunnerToken updates runner token fields.
func (s *Store) UpdateRunnerToken(ctx context.Context, tokenID string, updates store.RunnerTokenUpdates) error {
	return updateRunnerToken(ctx, s.pool, tokenID, updates)
}

// UpdateRunnerToken updates runner token fields within a transaction.
func (t *Tx) UpdateRunnerToken(ctx context.Context, tokenID string, updates store.RunnerTokenUpdates) error {
	return updateRunnerToken(ctx, t.tx, tokenID, updates)
}

func updateRunnerToken(ctx context.Context, q querier, tokenID string, updates store.RunnerTokenUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.TokenHash != nil {
		setClauses = append(setClauses, fmt.Sprintf("token_hash = $%d", argNum))
		args = append(args, *updates.TokenHash)
		argNum++
	}
	if updates.TokenPrefix != nil {
		setClauses = append(setClauses, fmt.Sprintf("token_prefix = $%d", argNum))
		args = append(args, *updates.TokenPrefix)
		argNum++
	}
	if updates.RunnerID != nil {
		setClauses = append(setClauses, fmt.Sprintf("runner_id = $%d", argNum))
		args = append(args, *updates.RunnerID)
		argNum++
	}
	if updates.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argNum))
		args = append(args, *updates.Status)
		argNum++
	}
	if updates.PreviousTokenHash != nil {
		setClauses = append(setClauses, fmt.Sprintf("previous_token_hash = $%d", argNum))
		args = append(args, *updates.PreviousTokenHash)
		argNum++
	}
	if updates.RotationDeadline != nil {
		setClauses = append(setClauses, fmt.Sprintf("rotation_deadline = $%d", argNum))
		args = append(args, *updates.RotationDeadline)
		argNum++
	}
	if updates.Labels != nil {
		setClauses = append(setClauses, fmt.Sprintf("labels = $%d", argNum))
		args = append(args, updates.Labels)
		argNum++
	}
	if updates.LastUsedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("last_used_at = $%d", argNum))
		args = append(args, *updates.LastUsedAt)
		argNum++
	}
	if updates.RevokedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("revoked_at = $%d", argNum))
		args = append(args, *updates.RevokedAt)
		argNum++
	}
	if updates.RevokeReason != nil {
		setClauses = append(setClauses, fmt.Sprintf("revoke_reason = $%d", argNum))
		args = append(args, *updates.RevokeReason)
		argNum++
	}

	if len(setClauses) == 0 {
		return nil
	}

	query := fmt.Sprintf(`UPDATE runner_tokens SET %s WHERE id = $%d`,
		strings.Join(setClauses, ", "), argNum)
	args = append(args, tokenID)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "runner_token", tokenID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "runner_token", ID: tokenID}
	}

	return nil
}

// DeleteRunnerToken deletes a runner token.
func (s *Store) DeleteRunnerToken(ctx context.Context, tokenID string) error {
	return deleteRunnerToken(ctx, s.pool, tokenID)
}

// DeleteRunnerToken deletes a runner token within a transaction.
func (t *Tx) DeleteRunnerToken(ctx context.Context, tokenID string) error {
	return deleteRunnerToken(ctx, t.tx, tokenID)
}

func deleteRunnerToken(ctx context.Context, q querier, tokenID string) error {
	query := `DELETE FROM runner_tokens WHERE id = $1`
	result, err := q.Exec(ctx, query, tokenID)
	if err != nil {
		return handlePgError(err, "runner_token", tokenID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "runner_token", ID: tokenID}
	}

	return nil
}

func scanRunnerToken(row pgx.Row, identifier string) (*store.RunnerToken, error) {
	var t store.RunnerToken
	err := row.Scan(
		&t.ID, &t.TokenHash, &t.TokenPrefix, &t.HashVersion, &t.RunnerID, &t.PoolName,
		&t.Status, &t.PreviousTokenHash, &t.RotationDeadline, &t.TenantID, &t.Labels, &t.CreatedAt, &t.CreatedBy,
		&t.LastUsedAt, &t.ExpiresAt, &t.RevokedAt, &t.RevokeReason,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "runner_token", ID: identifier}
		}
		return nil, fmt.Errorf("scanning runner_token: %w", err)
	}
	return &t, nil
}

func scanRunnerTokenFromRows(rows pgx.Rows) (*store.RunnerToken, error) {
	var t store.RunnerToken
	err := rows.Scan(
		&t.ID, &t.TokenHash, &t.TokenPrefix, &t.HashVersion, &t.RunnerID, &t.PoolName,
		&t.Status, &t.PreviousTokenHash, &t.RotationDeadline, &t.TenantID, &t.Labels, &t.CreatedAt, &t.CreatedBy,
		&t.LastUsedAt, &t.ExpiresAt, &t.RevokedAt, &t.RevokeReason,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
