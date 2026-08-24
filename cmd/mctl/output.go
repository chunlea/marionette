package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/chunlea/marionette/pkg/client"
	"gopkg.in/yaml.v3"
)

// OutputFormat represents the output format type.
type OutputFormat string

const (
	OutputTable OutputFormat = "table"
	OutputJSON  OutputFormat = "json"
	OutputYAML  OutputFormat = "yaml"
)

// Printer handles output formatting.
type Printer struct {
	format OutputFormat
	writer io.Writer
}

// NewPrinter creates a new Printer with the specified format and writer.
func NewPrinter(format string, writer io.Writer) *Printer {
	return &Printer{
		format: OutputFormat(format),
		writer: writer,
	}
}

// Print outputs the data in the configured format.
func (p *Printer) Print(v any) error {
	switch p.format {
	case OutputJSON:
		return p.printJSON(v)
	case OutputYAML:
		return p.printYAML(v)
	case OutputTable:
		// Values that have a table layout go through the typed Print* helpers
		// below; reaching here means this one has none.
		return fmt.Errorf("no table layout for %T; use -o json or -o yaml", v)
	default:
		// This used to share the table branch, so `-o wide` was reported as
		// "cannot print type X as table": a type problem for what is really a
		// mistyped flag, and advice to use JSON when the user had not asked
		// for a table in the first place.
		return fmt.Errorf("unknown output format %q; use table, json or yaml", p.format)
	}
}

// printJSON outputs data as indented JSON.
func (p *Printer) printJSON(v any) error {
	encoder := json.NewEncoder(p.writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}

// printYAML outputs data as YAML.
func (p *Printer) printYAML(v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	_, err = p.writer.Write(data)
	return err
}

// PrintTable outputs data as a table.
func (p *Printer) PrintTable(headers []string, rows [][]string) error {
	w := tabwriter.NewWriter(p.writer, 0, 0, 2, ' ', 0)

	// Print headers
	_, _ = fmt.Fprintln(w, strings.Join(headers, "\t"))

	// Print rows
	for _, row := range rows {
		_, _ = fmt.Fprintln(w, strings.Join(row, "\t"))
	}

	return w.Flush()
}

// tableView is how one resource renders as a table: the column headers, and
// how to turn a value into a row.
//
// Declaring it once per resource is what stops the single and list renderings
// drifting apart; they used to be separate methods that each repeated the
// header slice literally.
type tableView[T any] struct {
	headers []string
	row     func(*T) []string
}

// printOne renders a single value as a one-row table, or as JSON/YAML.
// These are functions rather than methods because Go methods cannot be generic.
func printOne[T any](p *Printer, v *T, view tableView[T]) error {
	if p.format != OutputTable {
		return p.Print(v)
	}
	return p.PrintTable(view.headers, [][]string{view.row(v)})
}

// printList renders a slice of values as a table, or as JSON/YAML.
func printList[T any](p *Printer, items []*T, view tableView[T]) error {
	if p.format != OutputTable {
		return p.Print(items)
	}
	rows := make([][]string, len(items))
	for i, item := range items {
		rows[i] = view.row(item)
	}
	return p.PrintTable(view.headers, rows)
}

var sessionView = tableView[client.Session]{
	headers: []string{"ID", "NAME", "STATUS", "AGENT", "RUNNER", "CREATED"},
	row: func(s *client.Session) []string {
		name := ""
		if s.Name != nil {
			name = *s.Name
		}
		return []string{
			s.ID,
			name,
			s.Status,
			s.Agent,
			derefString(s.RunnerID),
			formatTime(s.CreatedAt),
		}
	},
}

var taskView = tableView[client.Task]{
	headers: []string{"ID", "SESSION", "STATUS", "PROMPT", "CREATED"},
	row: func(t *client.Task) []string {
		return []string{
			t.ID,
			t.SessionID,
			t.Status,
			truncate(t.Prompt, 40),
			formatTime(t.CreatedAt),
		}
	},
}

var runnerView = tableView[client.Runner]{
	headers: []string{"ID", "NAME", "STATUS", "POOL", "SANDBOX", "LAST SEEN"},
	row: func(r *client.Runner) []string {
		lastSeen := ""
		if r.LastSeenAt != nil {
			lastSeen = formatTime(*r.LastSeenAt)
		}
		return []string{
			r.ID,
			r.Name,
			r.Status,
			derefString(r.PoolName),
			r.SandboxMode,
			lastSeen,
		}
	},
}

// adminRunnerView is the operator's runner row. It differs from runnerView by
// the two columns the admin API exists to expose - the provider config behind
// the runner, and whether it has been tainted out of service.
var adminRunnerView = tableView[client.AdminRunner]{
	headers: []string{"ID", "NAME", "STATUS", "POOL", "PROVIDER", "SANDBOX", "LAST SEEN"},
	row: func(r *client.AdminRunner) []string {
		lastSeen := ""
		if r.LastSeenAt != nil {
			lastSeen = formatTime(*r.LastSeenAt)
		}
		status := r.Status
		if r.Tainted {
			status += " (tainted)"
		}
		return []string{
			r.ID,
			r.Name,
			status,
			derefString(r.PoolName),
			derefString(r.ProviderConfigID),
			r.SandboxMode,
			lastSeen,
		}
	},
}

var permissionView = tableView[client.PermissionRequest]{
	headers: []string{"ID", "SESSION", "TASK", "TOOL", "STATUS", "RISK", "CREATED"},
	row: func(perm *client.PermissionRequest) []string {
		return []string{
			perm.ID,
			perm.SessionID,
			perm.TaskID,
			perm.Tool,
			perm.Status,
			perm.RiskLevel,
			formatTime(perm.CreatedAt),
		}
	},
}

var tunnelView = tableView[client.Tunnel]{
	headers: []string{"ID", "SESSION", "TYPE", "PORT", "PUBLIC", "PUBLIC_URL", "EXPIRES"},
	row: func(t *client.Tunnel) []string {
		expires := "-"
		if !t.ExpiresAt.IsZero() {
			expires = formatTime(t.ExpiresAt)
		}
		isPublic := "no"
		if t.IsPublic {
			isPublic = "yes"
		}
		return []string{
			t.ID,
			t.SessionID,
			t.Type,
			fmt.Sprintf("%d", t.LocalPort),
			isPublic,
			derefString(t.PublicURL),
			expires,
		}
	},
}

var scheduledTaskView = tableView[client.ScheduledTask]{
	headers: []string{"ID", "SESSION", "NAME", "STATUS", "CRON", "NEXT RUN", "CREATED"},
	row: func(t *client.ScheduledTask) []string {
		nextRun := "-"
		if t.NextRunAt != nil {
			nextRun = formatTime(*t.NextRunAt)
		}
		return []string{
			t.ID,
			t.SessionID,
			truncate(t.Name, 20),
			t.Status,
			truncate(t.CronExpression, 15),
			nextRun,
			formatTime(t.CreatedAt),
		}
	},
}

// PrintSession outputs a single session.
func (p *Printer) PrintSession(s *client.Session) error { return printOne(p, s, sessionView) }

// PrintSessionList outputs a list of sessions.
func (p *Printer) PrintSessionList(sessions []*client.Session) error {
	return printList(p, sessions, sessionView)
}

// PrintTask outputs a single task.
func (p *Printer) PrintTask(t *client.Task) error { return printOne(p, t, taskView) }

// PrintTaskList outputs a list of tasks.
func (p *Printer) PrintTaskList(tasks []*client.Task) error {
	return printList(p, tasks, taskView)
}

// PrintRunner outputs a single runner.
func (p *Printer) PrintRunner(r *client.Runner) error { return printOne(p, r, runnerView) }

// PrintRunnerList outputs a list of runners.
func (p *Printer) PrintRunnerList(runners []*client.Runner) error {
	return printList(p, runners, runnerView)
}

// PrintAdminRunner outputs a single runner in the operator's view.
func (p *Printer) PrintAdminRunner(r *client.AdminRunner) error {
	return printOne(p, r, adminRunnerView)
}

// PrintAdminRunnerList outputs a list of runners in the operator's view.
func (p *Printer) PrintAdminRunnerList(runners []*client.AdminRunner) error {
	return printList(p, runners, adminRunnerView)
}

// PrintPermission outputs a single permission request.
func (p *Printer) PrintPermission(perm *client.PermissionRequest) error {
	return printOne(p, perm, permissionView)
}

// PrintPermissionList outputs a list of permission requests.
func (p *Printer) PrintPermissionList(perms []*client.PermissionRequest) error {
	return printList(p, perms, permissionView)
}

// PrintScheduledTask outputs a single scheduled task.
func (p *Printer) PrintScheduledTask(t *client.ScheduledTask) error {
	return printOne(p, t, scheduledTaskView)
}

// PrintScheduledTaskList outputs a list of scheduled tasks.
func (p *Printer) PrintScheduledTaskList(tasks []*client.ScheduledTask) error {
	return printList(p, tasks, scheduledTaskView)
}

// PrintTunnelList outputs a list of tunnels.
func (p *Printer) PrintTunnelList(tunnels []*client.Tunnel) error {
	return printList(p, tunnels, tunnelView)
}

// PrintTunnel outputs a single tunnel, followed by how to reach it.
//
// This is the one resource whose single view is more than its row: the token
// is readable exactly once, in the create response, so the command that
// receives it has to tell the user what to do with it.
func (p *Printer) PrintTunnel(t *client.Tunnel) error {
	if p.format != OutputTable {
		return p.Print(t)
	}
	if err := printOne(p, t, tunnelView); err != nil {
		return err
	}

	url := derefString(t.PublicURL)
	switch {
	case t.IsPublic:
		_, _ = fmt.Fprintf(p.writer, "\nAccess the tunnel (public, no auth required):\n")
		_, _ = fmt.Fprintf(p.writer, "  curl %s/\n", url)
	case t.Token != "":
		_, _ = fmt.Fprintf(p.writer, "\nAccess the tunnel:\n")
		_, _ = fmt.Fprintf(p.writer, "  curl -H \"X-Marionette-Tunnel-Token: %s\" %s/\n", t.Token, url)
		_, _ = fmt.Fprintf(p.writer, "\nOr open in browser (will prompt for password):\n")
		_, _ = fmt.Fprintf(p.writer, "  %s/\n", url)
		_, _ = fmt.Fprintf(p.writer, "  (leave username empty, enter token as password)\n")
	}
	return nil
}

// derefString renders an optional string, using the empty string for unset.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// formatTime formats a time value for display.
func formatTime(t time.Time) string {
	duration := time.Since(t)

	switch {
	case duration < time.Minute:
		return "just now"
	case duration < time.Hour:
		mins := int(duration.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case duration < 24*time.Hour:
		hours := int(duration.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case duration < 7*24*time.Hour:
		days := int(duration.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	default:
		return t.Format("2006-01-02")
	}
}
