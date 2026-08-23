package postgres

import (
	"fmt"
	"slices"
	"strings"

	"github.com/chunlea/marionette/pkg/store"
)

// sortColumns is the allowlist of columns a list query may be ordered by.
// The first entry is the default when the caller does not ask for one.
//
// BaseListOptions.OrderBy reaches the store straight from the caller and is
// spliced into the SQL text — a column list cannot be parameterised. Every list
// query used to interpolate it unvalidated. No handler sets OrderBy today, so
// this was not exploitable, but the next one to wire it up would have opened an
// injection without touching the store at all.
type sortColumns []string

// orderClause renders the validated body of an ORDER BY clause, e.g.
// "created_at DESC". Unknown columns are rejected, never interpolated.
func (c sortColumns) orderClause(requested string, desc bool) (string, error) {
	column := c[0]

	if requested != "" {
		if !slices.Contains(c, requested) {
			return "", &store.InvalidInputError{
				Field:   "order_by",
				Message: fmt.Sprintf("unknown column %q (allowed: %s)", requested, strings.Join(c, ", ")),
			}
		}
		column = requested
	}

	if desc {
		return column + " DESC", nil
	}
	return column + " ASC", nil
}

// Per-entity allowlists. Each first entry preserves the default that the
// corresponding list query used before validation was added.
//
// Columns are deliberately limited to ones worth sorting on: adding a column
// here is a public API change, and every entry is verified against the real
// schema by TestSortColumnsExistInSchema.
var (
	apiKeySortColumns         = sortColumns{"created_at", "name", "last_used_at", "expires_at", "revoked_at"}
	runnerTokenSortColumns    = sortColumns{"created_at", "pool_name", "status", "last_used_at", "expires_at"}
	agentConfigSortColumns    = sortColumns{"created_at", "updated_at", "name", "agent"}
	providerConfigSortColumns = sortColumns{"created_at", "updated_at", "name", "provider"}
	profileSortColumns        = sortColumns{"created_at", "updated_at", "name"}
	runnerSortColumns         = sortColumns{"created_at", "updated_at", "name", "status", "last_seen_at"}
	logSortColumns            = sortColumns{"sequence", "created_at"}
	logArchiveSortColumns     = sortColumns{"archived_at", "expires_at", "first_log_at", "last_log_at", "log_count"}
	actionLogSortColumns      = sortColumns{"created_at", "action", "actor_type", "resource_type"}
	permissionSortColumns     = sortColumns{"created_at", "updated_at", "responded_at", "status", "risk_level"}
	sessionSortColumns        = sortColumns{"created_at", "updated_at", "name", "status", "last_activity_at", "suspended_at", "resumed_at"}
	taskSortColumns           = sortColumns{"created_at", "updated_at", "status"}
	taskRunSortColumns        = sortColumns{"queued_at", "updated_at", "started_at", "ended_at", "status", "attempt"}
	scheduledTaskSortColumns  = sortColumns{"created_at", "updated_at", "name", "status", "next_run_at", "last_run_at"}
	snapshotSortColumns       = sortColumns{"created_at", "name", "expires_at", "size_bytes"}
	tunnelSortColumns         = sortColumns{"created_at", "updated_at", "type", "expires_at", "closed_at"}
	workspaceSortColumns      = sortColumns{"created_at", "updated_at", "name", "expires_at", "last_synced_at"}
	streamSortColumns         = sortColumns{"created_at", "updated_at", "type", "state", "started_at", "stopped_at"}
)
