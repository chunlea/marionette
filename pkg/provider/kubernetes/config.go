// Package kubernetes implements a Kubernetes pod provider for Marionette.
// It manages runner lifecycle using the Kubernetes API, supporting pod
// creation, destruction, and PersistentVolumeClaim management for workspaces.
package kubernetes

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chunlea/marionette/pkg/provider"
)

const (
	// DefaultNamespace is the default Kubernetes namespace.
	DefaultNamespace = "default"

	// DefaultImage is the default container image for runners.
	DefaultImage = "marionette/agent:latest"

	// DefaultLabelPrefix is the default prefix for Kubernetes labels.
	DefaultLabelPrefix = "marionette.dev"

	// DefaultStorageClass uses the cluster default storage class.
	DefaultStorageClass = ""

	// DefaultStorageSize is the default PVC size.
	DefaultStorageSize = "10Gi"

	// DefaultMemory is the default memory limit.
	DefaultMemory = "2Gi"

	// DefaultCPUs is the default CPU limit.
	DefaultCPUs = "2"

	// DefaultRestartPolicy is the default pod restart policy.
	DefaultRestartPolicy = "Never"
)

// Config holds Kubernetes provider settings parsed from provider_configs.config JSON.
type Config struct {
	// Namespace is the Kubernetes namespace to create pods in.
	Namespace string `json:"namespace"`

	// Image is the container image to use for runners.
	Image string `json:"image"`

	// ImagePullPolicy specifies when to pull the image (Always, IfNotPresent, Never).
	ImagePullPolicy string `json:"image_pull_policy,omitempty"`

	// ImagePullSecrets lists secrets for pulling private images.
	ImagePullSecrets []string `json:"image_pull_secrets,omitempty"`

	// ServiceAccount is the service account to use for pods.
	ServiceAccount string `json:"service_account,omitempty"`

	// Resources holds default resource limits and requests.
	Resources ResourceConfig `json:"resources"`

	// Storage holds PVC configuration.
	Storage StorageConfig `json:"storage"`

	// NodeSelector constrains pods to nodes with matching labels.
	NodeSelector map[string]string `json:"node_selector,omitempty"`

	// Tolerations allow pods to be scheduled on nodes with taints.
	Tolerations []TolerationConfig `json:"tolerations,omitempty"`

	// Labels are additional labels to add to all resources.
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations are additional annotations to add to all resources.
	Annotations map[string]string `json:"annotations,omitempty"`

	// LabelPrefix is the prefix for provider-managed labels (default: marionette.dev).
	LabelPrefix string `json:"label_prefix"`

	// Cmd is the default command to run in the container.
	Cmd []string `json:"cmd,omitempty"`

	// Args are the default arguments to pass to the command.
	Args []string `json:"args,omitempty"`

	// RestartPolicy is the pod restart policy (Always, OnFailure, Never).
	RestartPolicy string `json:"restart_policy,omitempty"`

	// Kubeconfig is the path to the kubeconfig file (uses in-cluster if empty).
	Kubeconfig string `json:"kubeconfig,omitempty"`

	// Context is the kubeconfig context to use.
	Context string `json:"context,omitempty"`
}

// ResourceConfig holds default resource limits and requests.
type ResourceConfig struct {
	// Memory is the memory limit (e.g., "2Gi", "2048Mi").
	Memory string `json:"memory"`

	// CPUs is the CPU limit (e.g., "2", "1.5").
	CPUs string `json:"cpus"`

	// MemoryRequest is the memory request (defaults to limit if not set).
	MemoryRequest string `json:"memory_request,omitempty"`

	// CPURequest is the CPU request (defaults to limit if not set).
	CPURequest string `json:"cpu_request,omitempty"`

	// EphemeralStorage is the ephemeral storage limit (e.g., "10Gi").
	EphemeralStorage string `json:"ephemeral_storage,omitempty"`
}

// StorageConfig holds PVC configuration.
type StorageConfig struct {
	// StorageClass is the storage class for PVCs (empty = cluster default).
	StorageClass string `json:"storage_class,omitempty"`

	// Size is the default PVC size (e.g., "10Gi").
	Size string `json:"size"`

	// AccessMode is the PVC access mode (ReadWriteOnce, ReadWriteMany, ReadOnlyMany).
	AccessMode string `json:"access_mode,omitempty"`

	// ReclaimPolicy specifies what happens to PVC on workspace delete (Retain, Delete).
	ReclaimPolicy string `json:"reclaim_policy,omitempty"`
}

// TolerationConfig represents a Kubernetes toleration.
type TolerationConfig struct {
	Key               string `json:"key,omitempty"`
	Operator          string `json:"operator,omitempty"`
	Value             string `json:"value,omitempty"`
	Effect            string `json:"effect,omitempty"`
	TolerationSeconds *int64 `json:"toleration_seconds,omitempty"`
}

// ParseConfig parses raw JSON into Config with defaults applied.
func ParseConfig(data json.RawMessage) (*Config, error) {
	var cfg Config
	if len(data) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parsing kubernetes config: %w", err)
		}
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Namespace == "" {
		c.Namespace = DefaultNamespace
	}
	if c.Image == "" {
		c.Image = DefaultImage
	}
	if c.LabelPrefix == "" {
		c.LabelPrefix = DefaultLabelPrefix
	}
	if c.Resources.Memory == "" {
		c.Resources.Memory = DefaultMemory
	}
	if c.Resources.CPUs == "" {
		c.Resources.CPUs = DefaultCPUs
	}
	if c.Storage.Size == "" {
		c.Storage.Size = DefaultStorageSize
	}
	if c.Storage.AccessMode == "" {
		c.Storage.AccessMode = "ReadWriteOnce"
	}
	if c.RestartPolicy == "" {
		c.RestartPolicy = DefaultRestartPolicy
	}
}

func (c *Config) validate() error {
	if c.Image == "" {
		return &provider.ErrInvalidConfig{Field: "image", Reason: "required"}
	}

	// Validate namespace format (DNS label)
	if !isValidDNSLabel(c.Namespace) {
		return &provider.ErrInvalidConfig{
			Field:  "namespace",
			Reason: "must be a valid DNS label (lowercase alphanumeric and hyphens)",
		}
	}

	// Validate memory format
	if _, err := ParseQuantity(c.Resources.Memory); err != nil {
		return &provider.ErrInvalidConfig{Field: "resources.memory", Reason: err.Error()}
	}

	// Validate CPU format
	if _, err := ParseCPUs(c.Resources.CPUs); err != nil {
		return &provider.ErrInvalidConfig{Field: "resources.cpus", Reason: err.Error()}
	}

	// Validate storage size format
	if _, err := ParseQuantity(c.Storage.Size); err != nil {
		return &provider.ErrInvalidConfig{Field: "storage.size", Reason: err.Error()}
	}

	// Validate access mode
	validAccessModes := map[string]bool{
		"ReadWriteOnce": true,
		"ReadWriteMany": true,
		"ReadOnlyMany":  true,
	}
	if !validAccessModes[c.Storage.AccessMode] {
		return &provider.ErrInvalidConfig{
			Field:  "storage.access_mode",
			Reason: "must be ReadWriteOnce, ReadWriteMany, or ReadOnlyMany",
		}
	}

	// Validate restart policy
	validRestartPolicies := map[string]bool{
		"Always":    true,
		"OnFailure": true,
		"Never":     true,
	}
	if !validRestartPolicies[c.RestartPolicy] {
		return &provider.ErrInvalidConfig{
			Field:  "restart_policy",
			Reason: "must be Always, OnFailure, or Never",
		}
	}

	// Validate image pull policy if set
	if c.ImagePullPolicy != "" {
		validPullPolicies := map[string]bool{
			"Always":       true,
			"IfNotPresent": true,
			"Never":        true,
		}
		if !validPullPolicies[c.ImagePullPolicy] {
			return &provider.ErrInvalidConfig{
				Field:  "image_pull_policy",
				Reason: "must be Always, IfNotPresent, or Never",
			}
		}
	}

	return nil
}

// quantityPattern matches Kubernetes quantity strings like "2Gi", "500m", "1.5".
var quantityPattern = regexp.MustCompile(`^(\d+(?:\.\d+)?)(E|P|T|G|M|K|Ei|Pi|Ti|Gi|Mi|Ki|m|k)?$`)

// ParseQuantity converts a Kubernetes quantity string to bytes.
// Supports formats: "2Gi", "2G", "2048Mi", "500m" (millicores), "2147483648"
func ParseQuantity(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty quantity value")
	}

	matches := quantityPattern.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("invalid quantity format: %s", s)
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid quantity value: %s", s)
	}

	unit := matches[2]
	switch unit {
	case "E":
		return int64(value * 1e18), nil
	case "P":
		return int64(value * 1e15), nil
	case "T":
		return int64(value * 1e12), nil
	case "G":
		return int64(value * 1e9), nil
	case "M":
		return int64(value * 1e6), nil
	case "K", "k":
		return int64(value * 1e3), nil
	case "Ei":
		return int64(value * 1152921504606846976), nil // 2^60
	case "Pi":
		return int64(value * 1125899906842624), nil // 2^50
	case "Ti":
		return int64(value * 1099511627776), nil // 2^40
	case "Gi":
		return int64(value * 1073741824), nil // 2^30
	case "Mi":
		return int64(value * 1048576), nil // 2^20
	case "Ki":
		return int64(value * 1024), nil // 2^10
	case "m":
		// Millicores or milli-units
		return int64(value), nil
	case "":
		return int64(value), nil
	default:
		return 0, fmt.Errorf("unknown quantity unit: %s", unit)
	}
}

// ParseCPUs converts a CPU string to millicores.
// Supports formats: "2", "1.5", "500m"
func ParseCPUs(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty CPU value")
	}

	// Check for millicores suffix
	if millis, found := strings.CutSuffix(s, "m"); found {
		value, err := strconv.ParseInt(millis, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid CPU value: %s", s)
		}
		return value, nil
	}

	// Parse as cores
	value, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid CPU value: %s", s)
	}

	if value <= 0 {
		return 0, fmt.Errorf("CPU value must be positive: %s", s)
	}

	// Convert to millicores
	return int64(value * 1000), nil
}

// FormatCPUs formats millicores as a Kubernetes quantity string.
func FormatCPUs(millicores int64) string {
	if millicores%1000 == 0 {
		return fmt.Sprintf("%d", millicores/1000)
	}
	return fmt.Sprintf("%dm", millicores)
}

// isValidDNSLabel checks if a string is a valid DNS label.
func isValidDNSLabel(s string) bool {
	if len(s) == 0 || len(s) > 63 {
		return false
	}
	// Must start with lowercase letter and contain only lowercase alphanumeric and hyphens
	dnsLabelPattern := regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$|^[a-z]$`)
	return dnsLabelPattern.MatchString(s)
}

// MemoryBytes returns the memory limit in bytes.
func (c *Config) MemoryBytes() (int64, error) {
	return ParseQuantity(c.Resources.Memory)
}

// CPUMillicores returns the CPU limit in millicores.
func (c *Config) CPUMillicores() (int64, error) {
	return ParseCPUs(c.Resources.CPUs)
}

// StorageBytes returns the storage size in bytes.
func (c *Config) StorageBytes() (int64, error) {
	return ParseQuantity(c.Storage.Size)
}

// defaultSuspendConfig returns Kubernetes' suspend defaults. Kubernetes cannot
// pause a pod, so the default strategy terminates while preserving the PVC.
func defaultSuspendConfig() provider.SuspendConfig {
	return provider.SuspendConfig{
		Strategy:    provider.SuspendStrategyTerminatePreserveStorage,
		MinDuration: 60 * time.Second,
		MaxDuration: 24 * time.Hour,
		Fallback:    provider.SuspendStrategyTerminate,
	}
}
