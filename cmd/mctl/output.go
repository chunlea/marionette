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
	default:
		return fmt.Errorf("cannot print type %T as table, use JSON or YAML", v)
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

// PrintSession outputs a single session.
func (p *Printer) PrintSession(s *client.Session) error {
	if p.format == OutputTable {
		headers := []string{"ID", "NAME", "STATUS", "AGENT", "RUNNER", "CREATED"}
		rows := [][]string{sessionToRow(s)}
		return p.PrintTable(headers, rows)
	}
	return p.Print(s)
}

// PrintSessionList outputs a list of sessions.
func (p *Printer) PrintSessionList(sessions []*client.Session) error {
	if p.format == OutputTable {
		headers := []string{"ID", "NAME", "STATUS", "AGENT", "RUNNER", "CREATED"}
		rows := make([][]string, len(sessions))
		for i, s := range sessions {
			rows[i] = sessionToRow(s)
		}
		return p.PrintTable(headers, rows)
	}
	return p.Print(sessions)
}

// PrintTask outputs a single task.
func (p *Printer) PrintTask(t *client.Task) error {
	if p.format == OutputTable {
		headers := []string{"ID", "SESSION", "STATUS", "PROMPT", "CREATED"}
		rows := [][]string{taskToRow(t)}
		return p.PrintTable(headers, rows)
	}
	return p.Print(t)
}

// PrintTaskList outputs a list of tasks.
func (p *Printer) PrintTaskList(tasks []*client.Task) error {
	if p.format == OutputTable {
		headers := []string{"ID", "SESSION", "STATUS", "PROMPT", "CREATED"}
		rows := make([][]string, len(tasks))
		for i, t := range tasks {
			rows[i] = taskToRow(t)
		}
		return p.PrintTable(headers, rows)
	}
	return p.Print(tasks)
}

// sessionToRow converts a session to a table row.
func sessionToRow(s *client.Session) []string {
	name := ""
	if s.Name != nil {
		name = *s.Name
	}
	runner := ""
	if s.RunnerID != nil {
		runner = *s.RunnerID
	}
	return []string{
		s.ID,
		name,
		s.Status,
		s.Agent,
		runner,
		formatTime(s.CreatedAt),
	}
}

// taskToRow converts a task to a table row.
func taskToRow(t *client.Task) []string {
	prompt := t.Prompt
	if len(prompt) > 40 {
		prompt = prompt[:37] + "..."
	}
	return []string{
		t.ID,
		t.SessionID,
		t.Status,
		prompt,
		formatTime(t.CreatedAt),
	}
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
