package postgres

import (
	"bufio"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/chunlea/marionette/pkg/store"
)

func TestSortColumnsOrderClause(t *testing.T) {
	columns := sortColumns{"created_at", "name", "status"}

	tests := []struct {
		name      string
		requested string
		desc      bool
		want      string
		wantErr   bool
	}{
		{name: "defaults to the first column, ascending", want: "created_at ASC"},
		{name: "default column, descending", desc: true, want: "created_at DESC"},
		{name: "allowed column", requested: "name", want: "name ASC"},
		{name: "allowed column, descending", requested: "status", desc: true, want: "status DESC"},
		{name: "unknown column", requested: "secret", wantErr: true},
		{name: "injection attempt", requested: "created_at; DROP TABLE sessions--", wantErr: true},
		{name: "expression", requested: "(SELECT 1)", wantErr: true},
		{name: "direction smuggled into the column", requested: "name DESC", wantErr: true},
		{name: "qualified column", requested: "sessions.name", wantErr: true},
		{name: "case mismatch", requested: "NAME", wantErr: true},
		{name: "whitespace", requested: " name", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := columns.orderClause(tt.requested, tt.desc)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("orderClause(%q) = %q, want an error", tt.requested, got)
				}
				if !errors.Is(err, store.ErrInvalidInput) {
					t.Errorf("orderClause(%q) error = %v, want ErrInvalidInput", tt.requested, err)
				}
				if got != "" {
					t.Errorf("orderClause(%q) returned %q alongside an error", tt.requested, got)
				}
				return
			}

			if err != nil {
				t.Fatalf("orderClause(%q) error = %v", tt.requested, err)
			}
			if got != tt.want {
				t.Errorf("orderClause(%q) = %q, want %q", tt.requested, got, tt.want)
			}
		})
	}
}

// allSortColumns pairs every allowlist with the table it orders, so the schema
// check below cannot silently miss one.
func allSortColumns() map[string]sortColumns {
	return map[string]sortColumns{
		"api_keys":            apiKeySortColumns,
		"runner_tokens":       runnerTokenSortColumns,
		"agent_configs":       agentConfigSortColumns,
		"provider_configs":    providerConfigSortColumns,
		"profiles":            profileSortColumns,
		"runners":             runnerSortColumns,
		"logs":                logSortColumns,
		"log_archives":        logArchiveSortColumns,
		"action_logs":         actionLogSortColumns,
		"permission_requests": permissionSortColumns,
		"sessions":            sessionSortColumns,
		"tasks":               taskSortColumns,
		"task_runs":           taskRunSortColumns,
		"scheduled_tasks":     scheduledTaskSortColumns,
		"snapshots":           snapshotSortColumns,
		"tunnels":             tunnelSortColumns,
		"workspaces":          workspaceSortColumns,
		"streams":             streamSortColumns,
	}
}

func TestSortColumnsAreNonEmptyAndUnique(t *testing.T) {
	for table, columns := range allSortColumns() {
		if len(columns) == 0 {
			t.Errorf("%s: allowlist is empty, orderClause would panic on the default", table)
			continue
		}
		seen := make(map[string]bool, len(columns))
		for _, c := range columns {
			if seen[c] {
				t.Errorf("%s: duplicate column %q", table, c)
			}
			seen[c] = true
		}
	}
}

// TestSortColumnsExistInSchema keeps the allowlists honest: an entry naming a
// column that does not exist would turn every ordered list query into a runtime
// error the moment a caller asked for it.
//
// docs/schema.sql is generated from migrations/ and CI fails on drift
// (make schema-check), so checking against it is equivalent to checking against
// a live database — without needing one.
func TestSortColumnsExistInSchema(t *testing.T) {
	schema := parseSchemaColumns(t, "../../../docs/schema.sql")

	for table, columns := range allSortColumns() {
		tableColumns, ok := schema[table]
		if !ok {
			t.Errorf("table %q is not in docs/schema.sql", table)
			continue
		}
		for _, column := range columns {
			if !tableColumns[column] {
				t.Errorf("%s.%s does not exist in docs/schema.sql", table, column)
			}
		}
	}
}

var (
	schemaTableRe  = regexp.MustCompile(`^CREATE TABLE public\.(\w+) \($`)
	schemaColumnRe = regexp.MustCompile(`^([a-z_][a-z0-9_]*) `)
)

// parseSchemaColumns reads table -> column names out of pg_dump output.
func parseSchemaColumns(t *testing.T, path string) map[string]map[string]bool {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening schema: %v", err)
	}
	defer func() { _ = f.Close() }()

	tables := make(map[string]map[string]bool)
	var current string

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()

		if m := schemaTableRe.FindStringSubmatch(line); m != nil {
			current = m[1]
			tables[current] = make(map[string]bool)
			continue
		}
		if current == "" {
			continue
		}
		if strings.HasPrefix(line, ")") {
			current = ""
			continue
		}

		field := strings.TrimSpace(line)
		if strings.HasPrefix(field, "CONSTRAINT") {
			continue
		}
		if m := schemaColumnRe.FindStringSubmatch(field); m != nil {
			tables[current][m[1]] = true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading schema: %v", err)
	}

	if len(tables) == 0 {
		t.Fatalf("no tables parsed from %s", path)
	}
	return tables
}
