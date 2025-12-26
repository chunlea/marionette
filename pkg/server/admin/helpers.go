package admin

import (
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// logError creates a zap field for an error.
func logError(err error) zap.Field {
	return zap.Error(err)
}

// parseLabels parses a comma-separated key=value string into a map.
// Example: "env=prod,team=backend" -> {"env": "prod", "team": "backend"}
func parseLabels(s string) map[string]string {
	if s == "" {
		return nil
	}

	labels := make(map[string]string)
	pairs := strings.Split(s, ",")
	for _, pair := range pairs {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			labels[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}

	if len(labels) == 0 {
		return nil
	}
	return labels
}

// parseLimit parses a limit string into an integer, with a default of 50.
func parseLimit(s string) int {
	if s == "" {
		return 50
	}
	limit, err := strconv.Atoi(s)
	if err != nil || limit <= 0 {
		return 50
	}
	if limit > 100 {
		return 100
	}
	return limit
}
