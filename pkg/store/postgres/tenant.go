package postgres

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chunlea/marionette/pkg/store"
)

// setTenantSQL binds the request's tenant for the current transaction.
//
// SET LOCAL, not SET: the setting dies with the transaction, so a pooled
// connection can never carry one request's tenant into the next request. That
// is the whole reason statements run inside a transaction when a tenant is
// present - a connection-level SET on a pooled connection is a cross-tenant
// data leak waiting for the first missed RESET.
const setTenantSQL = `SELECT set_config('app.tenant_id', $1, true)`

// setSystemSQL grants cross-tenant access for the current transaction.
//
// Transaction-scoped for the same reason as the tenant binding: a
// connection-level grant that outlived its request would turn the next
// request on that pooled connection into an accidental superuser.
const setSystemSQL = `SELECT set_config('app.system', 'on', true)`

// tenantDB is the querier the Store hands to every statement.
//
// With no tenant in context it is the bare pool: single-tenant deployments pay
// nothing and behave exactly as before. With a tenant it wraps each statement
// in a transaction that first binds app.tenant_id, which is what the row level
// security policies from migration 008 read.
//
// The cost is one round trip pair per statement for multi-tenant deployments.
// That is the price of making isolation a property of the database rather than
// of remembering a WHERE clause in 133 queries.
type tenantDB struct {
	pool *pgxpool.Pool
}

// begin opens a transaction with the tenant bound, or with cross-tenant access
// granted when the context carries it.
func (d tenantDB) begin(ctx context.Context, tenantID string) (pgx.Tx, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning tenant transaction: %w", err)
	}

	if store.IsSystemAccess(ctx) {
		if _, err := tx.Exec(ctx, setSystemSQL); err != nil {
			_ = tx.Rollback(ctx)
			return nil, fmt.Errorf("granting system access: %w", err)
		}
		return tx, nil
	}

	if _, err := tx.Exec(ctx, setTenantSQL, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("binding tenant %q: %w", tenantID, err)
	}
	return tx, nil
}

// needsTransaction reports whether a statement has to run inside one to carry
// its tenant binding or its system grant.
func needsTransaction(ctx context.Context) (tenantID string, needed bool) {
	if store.IsSystemAccess(ctx) {
		return "", true
	}
	return store.TenantFromContext(ctx)
}

// Exec runs a statement, bound to the context's tenant when there is one.
func (d tenantDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tenantID, ok := needsTransaction(ctx)
	if !ok {
		return d.pool.Exec(ctx, sql, args...)
	}

	tx, err := d.begin(ctx, tenantID)
	if err != nil {
		return pgconn.CommandTag{}, err
	}

	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		_ = tx.Rollback(ctx)
		return tag, err
	}
	if err := tx.Commit(ctx); err != nil {
		return tag, fmt.Errorf("committing tenant transaction: %w", err)
	}
	return tag, nil
}

// QueryRow runs a single-row query bound to the context's tenant.
//
// The transaction is closed by Scan, because that is when pgx has finished with
// the connection. Every QueryRow in this package is scanned exactly once; a
// result that is never scanned would hold its connection until the context is
// cancelled, which is why the row type is not exported.
func (d tenantDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	tenantID, ok := needsTransaction(ctx)
	if !ok {
		return d.pool.QueryRow(ctx, sql, args...)
	}

	tx, err := d.begin(ctx, tenantID)
	if err != nil {
		return errRow{err: err}
	}

	return &tenantRow{row: tx.QueryRow(ctx, sql, args...), tx: tx, ctx: ctx}
}

// Query runs a multi-row query bound to the context's tenant.
//
// The transaction is closed by Rows.Close, which pgx callers are already
// required to call.
func (d tenantDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	tenantID, ok := needsTransaction(ctx)
	if !ok {
		return d.pool.Query(ctx, sql, args...)
	}

	tx, err := d.begin(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return &tenantRows{Rows: rows, tx: tx, ctx: ctx}, nil
}

// tenantRow closes its transaction when the row is scanned.
type tenantRow struct {
	row  pgx.Row
	tx   pgx.Tx
	ctx  context.Context //nolint:containedctx // the tx outlives the call that made it
	once sync.Once
}

func (r *tenantRow) Scan(dest ...any) error {
	err := r.row.Scan(dest...)
	r.once.Do(func() {
		if err != nil {
			_ = r.tx.Rollback(r.ctx)
			return
		}
		_ = r.tx.Commit(r.ctx)
	})
	return err
}

// tenantRows closes its transaction when the rows are closed.
type tenantRows struct {
	pgx.Rows
	tx   pgx.Tx
	ctx  context.Context //nolint:containedctx // the tx outlives the call that made it
	once sync.Once
}

func (r *tenantRows) Close() {
	r.Rows.Close()
	r.once.Do(func() {
		if err := r.Rows.Err(); err != nil {
			_ = r.tx.Rollback(r.ctx)
			return
		}
		_ = r.tx.Commit(r.ctx)
	})
}

// errRow reports a failure that happened before the query could be sent.
type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }
