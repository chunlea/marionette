package postgres

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/chunlea/marionette/pkg/store"
)

// sortColumns is the allowlist of columns a list query may be ordered by, and
// the key it paginates on.
//
// BaseListOptions.OrderBy reaches the store straight from the caller and is
// spliced into the SQL text — a column list cannot be parameterised. Every list
// query used to interpolate it unvalidated. No handler sets OrderBy today, so
// this was not exploitable, but the next one to wire it up would have opened an
// injection without touching the store at all.
type sortColumns struct {
	// allowed lists the orderable columns. The first entry is the default and
	// is also the column cursor pagination keys on.
	allowed []string

	// numericKey marks the default column as an integer sequence rather than a
	// timestamp, which changes how a cursor value is encoded and compared.
	numericKey bool
}

// listPage is the resolved ordering and cursor position of one list query.
type listPage struct {
	// orderBy is the validated body of the ORDER BY clause, always with the id
	// tiebreaker appended.
	orderBy string

	// condition is the WHERE predicate that resumes after the cursor, empty
	// when the caller did not pass one.
	condition string

	// args are the placeholder values condition refers to.
	args []any

	// cursorable is false when the caller ordered by something other than the
	// cursor key, in which case no next cursor is offered.
	cursorable bool
}

// page resolves ordering and cursor position for a list query. argNum is the
// next free placeholder number; the returned args occupy argNum onwards.
func (c sortColumns) page(opts store.BaseListOptions, argNum int) (listPage, error) {
	key := c.allowed[0]

	column := key
	if opts.OrderBy != "" {
		if !slices.Contains(c.allowed, opts.OrderBy) {
			return listPage{}, &store.InvalidInputError{
				Field:   "order_by",
				Message: fmt.Sprintf("unknown column %q (allowed: %s)", opts.OrderBy, strings.Join(c.allowed, ", ")),
			}
		}
		column = opts.OrderBy
	}

	direction := "ASC"
	comparison := ">"
	if opts.OrderDesc {
		direction = "DESC"
		comparison = "<"
	}

	page := listPage{
		// The id tiebreaker makes the ordering total. Without it rows sharing a
		// key value come back in whatever order the plan produces, and a page
		// boundary inside such a run silently skips or repeats rows.
		orderBy:    fmt.Sprintf("%s %s, id %s", column, direction, direction),
		cursorable: column == key,
	}

	if opts.Cursor == "" {
		return page, nil
	}

	// A cursor encodes a position in one specific ordering. Honouring it under
	// a different one would return a plausible-looking but wrong page, so say
	// so instead.
	if !page.cursorable {
		return listPage{}, &store.InvalidInputError{
			Field:   "cursor",
			Message: fmt.Sprintf("cursor pagination is only supported when ordering by %q", key),
		}
	}

	value, id, err := decodeCursorValue(opts.Cursor)
	if err != nil {
		return listPage{}, &store.InvalidInputError{Field: "cursor", Message: err.Error()}
	}

	position, err := c.decodeKey(value)
	if err != nil {
		return listPage{}, err
	}

	// The comparison must follow the ORDER BY direction: paging descending with
	// an ascending predicate walks back over rows the caller already had.
	page.condition = fmt.Sprintf("(%s %s $%d OR (%s = $%d AND id %s $%d))",
		key, comparison, argNum, key, argNum+1, comparison, argNum+2)
	page.args = []any{position, position, id}

	return page, nil
}

// decodeKey turns the textual cursor key back into a value the driver can bind
// with the column's own type.
func (c sortColumns) decodeKey(value string) (any, error) {
	if c.numericKey {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, &store.InvalidInputError{Field: "cursor", Message: "invalid cursor position"}
		}
		return n, nil
	}

	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, &store.InvalidInputError{Field: "cursor", Message: "invalid cursor position"}
	}
	return t, nil
}

// where combines the caller's filters with the cursor predicate. The filters
// alone stay on the count query, so TotalCount reports how many rows match the
// filters rather than how many are left after the current page.
func (p listPage) where(filters string) string {
	switch {
	case p.condition == "":
		return filters
	case filters == "":
		return "WHERE " + p.condition
	default:
		return filters + " AND " + p.condition
	}
}

// limitArg is the placeholder number for LIMIT, after the cursor's own args.
func (p listPage) limitArg(argNum int) int {
	return argNum + len(p.args)
}

// nextTime returns the cursor that resumes this listing after the last row of
// the page. It is empty when there is no next page, or when the caller ordered
// by something the cursor cannot describe.
func (p listPage) nextTime(hasMore bool, key time.Time, id string) string {
	if !hasMore || !p.cursorable {
		return ""
	}
	return encodeCursorValue(key.Format(time.RFC3339Nano), id)
}

// nextSeq is nextTime for a listing keyed on an integer sequence.
func (p listPage) nextSeq(hasMore bool, key int64, id string) string {
	if !hasMore || !p.cursorable {
		return ""
	}
	return encodeCursorValue(strconv.FormatInt(key, 10), id)
}

// Per-entity allowlists. Each first entry preserves the default that the
// corresponding list query used before validation was added, and is the column
// that query paginates on.
//
// Columns are deliberately limited to ones worth sorting on: adding a column
// here is a public API change, and every entry is verified against the real
// schema by TestSortColumnsExistInSchema.
var (
	apiKeySortColumns         = sortColumns{allowed: []string{"created_at", "name", "last_used_at", "expires_at", "revoked_at"}}
	runnerTokenSortColumns    = sortColumns{allowed: []string{"created_at", "pool_name", "status", "last_used_at", "expires_at"}}
	agentConfigSortColumns    = sortColumns{allowed: []string{"created_at", "updated_at", "name", "agent"}}
	providerConfigSortColumns = sortColumns{allowed: []string{"created_at", "updated_at", "name", "provider"}}
	profileSortColumns        = sortColumns{allowed: []string{"created_at", "updated_at", "name"}}
	runnerSortColumns         = sortColumns{allowed: []string{"created_at", "updated_at", "name", "status", "last_seen_at"}}
	logSortColumns            = sortColumns{allowed: []string{"sequence", "created_at"}, numericKey: true}
	logArchiveSortColumns     = sortColumns{allowed: []string{"archived_at", "expires_at", "first_log_at", "last_log_at", "log_count"}}
	actionLogSortColumns      = sortColumns{allowed: []string{"created_at", "action", "actor_type", "resource_type"}}
	permissionSortColumns     = sortColumns{allowed: []string{"created_at", "updated_at", "responded_at", "status", "risk_level"}}
	sessionSortColumns        = sortColumns{allowed: []string{"created_at", "updated_at", "name", "status", "last_activity_at", "suspended_at", "resumed_at"}}
	taskSortColumns           = sortColumns{allowed: []string{"created_at", "updated_at", "status"}}
	taskRunSortColumns        = sortColumns{allowed: []string{"queued_at", "updated_at", "started_at", "ended_at", "status", "attempt"}}
	scheduledTaskSortColumns  = sortColumns{allowed: []string{"created_at", "updated_at", "name", "status", "next_run_at", "last_run_at"}}
	snapshotSortColumns       = sortColumns{allowed: []string{"created_at", "name", "expires_at", "size_bytes"}}
	tunnelSortColumns         = sortColumns{allowed: []string{"created_at", "updated_at", "type", "expires_at", "closed_at"}}
	workspaceSortColumns      = sortColumns{allowed: []string{"created_at", "updated_at", "name", "expires_at", "last_synced_at"}}
	streamSortColumns         = sortColumns{allowed: []string{"created_at", "updated_at", "type", "state", "started_at", "stopped_at"}}
)
