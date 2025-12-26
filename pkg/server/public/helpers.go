package public

import (
	"encoding/json"
)

// contains checks if a slice contains a value.
func contains(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

// matchLabels checks if the resource labels match the filter labels.
// Filter labels must be a subset of resource labels.
func matchLabels(resourceLabels json.RawMessage, filterLabels map[string]string) bool {
	if len(filterLabels) == 0 {
		return true
	}

	var labels map[string]string
	if err := json.Unmarshal(resourceLabels, &labels); err != nil {
		return false
	}

	for k, v := range filterLabels {
		if labels[k] != v {
			return false
		}
	}
	return true
}
