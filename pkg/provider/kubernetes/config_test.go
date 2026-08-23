package kubernetes

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/provider"
)

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		errField  string
		checkFunc func(*testing.T, *Config)
	}{
		{
			name:  "defaults",
			input: `{}`,
			checkFunc: func(t *testing.T, cfg *Config) {
				assert.Equal(t, DefaultNamespace, cfg.Namespace)
				assert.Equal(t, DefaultImage, cfg.Image)
				assert.Equal(t, DefaultLabelPrefix, cfg.LabelPrefix)
				assert.Equal(t, DefaultMemory, cfg.Resources.Memory)
				assert.Equal(t, DefaultCPUs, cfg.Resources.CPUs)
				assert.Equal(t, DefaultStorageSize, cfg.Storage.Size)
				assert.Equal(t, "ReadWriteOnce", cfg.Storage.AccessMode)
				assert.Equal(t, DefaultRestartPolicy, cfg.RestartPolicy)
			},
		},
		{
			name: "full config",
			input: `{
				"namespace": "marionette",
				"image": "custom/agent:v1",
				"image_pull_policy": "Always",
				"image_pull_secrets": ["regcred"],
				"service_account": "marionette-sa",
				"resources": {
					"memory": "4Gi",
					"cpus": "4",
					"memory_request": "2Gi",
					"cpu_request": "1",
					"ephemeral_storage": "20Gi"
				},
				"storage": {
					"storage_class": "fast-ssd",
					"size": "50Gi",
					"access_mode": "ReadWriteMany"
				},
				"node_selector": {"node-type": "compute"},
				"tolerations": [
					{"key": "dedicated", "operator": "Equal", "value": "agent", "effect": "NoSchedule"}
				],
				"labels": {"app": "marionette"},
				"annotations": {"description": "test"},
				"label_prefix": "custom.io",
				"cmd": ["/bin/agent"],
				"args": ["--debug"],
				"restart_policy": "OnFailure",
				"kubeconfig": "/path/to/kubeconfig",
				"context": "my-cluster"
			}`,
			checkFunc: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "marionette", cfg.Namespace)
				assert.Equal(t, "custom/agent:v1", cfg.Image)
				assert.Equal(t, "Always", cfg.ImagePullPolicy)
				assert.Equal(t, []string{"regcred"}, cfg.ImagePullSecrets)
				assert.Equal(t, "marionette-sa", cfg.ServiceAccount)
				assert.Equal(t, "4Gi", cfg.Resources.Memory)
				assert.Equal(t, "4", cfg.Resources.CPUs)
				assert.Equal(t, "2Gi", cfg.Resources.MemoryRequest)
				assert.Equal(t, "1", cfg.Resources.CPURequest)
				assert.Equal(t, "20Gi", cfg.Resources.EphemeralStorage)
				assert.Equal(t, "fast-ssd", cfg.Storage.StorageClass)
				assert.Equal(t, "50Gi", cfg.Storage.Size)
				assert.Equal(t, "ReadWriteMany", cfg.Storage.AccessMode)
				assert.Equal(t, map[string]string{"node-type": "compute"}, cfg.NodeSelector)
				assert.Len(t, cfg.Tolerations, 1)
				assert.Equal(t, "dedicated", cfg.Tolerations[0].Key)
				assert.Equal(t, map[string]string{"app": "marionette"}, cfg.Labels)
				assert.Equal(t, map[string]string{"description": "test"}, cfg.Annotations)
				assert.Equal(t, "custom.io", cfg.LabelPrefix)
				assert.Equal(t, []string{"/bin/agent"}, cfg.Cmd)
				assert.Equal(t, []string{"--debug"}, cfg.Args)
				assert.Equal(t, "OnFailure", cfg.RestartPolicy)
				assert.Equal(t, "/path/to/kubeconfig", cfg.Kubeconfig)
				assert.Equal(t, "my-cluster", cfg.Context)
			},
		},
		{
			name:     "invalid namespace",
			input:    `{"namespace": "Invalid_Namespace"}`,
			wantErr:  true,
			errField: "namespace",
		},
		{
			name:     "invalid memory",
			input:    `{"resources": {"memory": "invalid"}}`,
			wantErr:  true,
			errField: "resources.memory",
		},
		{
			name:     "invalid cpus",
			input:    `{"resources": {"cpus": "abc"}}`,
			wantErr:  true,
			errField: "resources.cpus",
		},
		{
			name:     "invalid storage size",
			input:    `{"storage": {"size": "invalid"}}`,
			wantErr:  true,
			errField: "storage.size",
		},
		{
			name:     "invalid access mode",
			input:    `{"storage": {"access_mode": "Invalid"}}`,
			wantErr:  true,
			errField: "storage.access_mode",
		},
		{
			name:     "invalid restart policy",
			input:    `{"restart_policy": "Invalid"}`,
			wantErr:  true,
			errField: "restart_policy",
		},
		{
			name:     "invalid image pull policy",
			input:    `{"image_pull_policy": "Invalid"}`,
			wantErr:  true,
			errField: "image_pull_policy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseConfig(json.RawMessage(tt.input))

			if tt.wantErr {
				require.Error(t, err)
				var invErr *provider.ErrInvalidConfig
				require.ErrorAs(t, err, &invErr)
				assert.Equal(t, tt.errField, invErr.Field)
				return
			}

			require.NoError(t, err)
			if tt.checkFunc != nil {
				tt.checkFunc(t, cfg)
			}
		})
	}
}

func TestParseSuspendConfig(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		checkFunc func(*testing.T, *provider.SuspendConfig)
	}{
		{
			name:  "defaults",
			input: `{}`,
			checkFunc: func(t *testing.T, cfg *provider.SuspendConfig) {
				assert.Equal(t, provider.SuspendStrategyTerminatePreserveStorage, cfg.Strategy)
				assert.Equal(t, 60*time.Second, cfg.MinDuration)
				assert.Equal(t, 24*time.Hour, cfg.MaxDuration)
				assert.Equal(t, provider.SuspendStrategyTerminate, cfg.Fallback)
			},
		},
		{
			name: "custom values",
			input: `{
				"strategy": "terminate",
				"min_duration": "30s",
				"max_duration": "12h",
				"fallback": "terminate_preserve_storage"
			}`,
			checkFunc: func(t *testing.T, cfg *provider.SuspendConfig) {
				assert.Equal(t, provider.SuspendStrategyTerminate, cfg.Strategy)
				assert.Equal(t, 30*time.Second, cfg.MinDuration)
				assert.Equal(t, 12*time.Hour, cfg.MaxDuration)
				assert.Equal(t, provider.SuspendStrategyTerminatePreserveStorage, cfg.Fallback)
			},
		},
		{
			name:  "duration as seconds",
			input: `{"min_duration": 120}`,
			checkFunc: func(t *testing.T, cfg *provider.SuspendConfig) {
				assert.Equal(t, 120*time.Second, cfg.MinDuration)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := provider.ParseSuspendConfig(json.RawMessage(tt.input), defaultSuspendConfig())
			require.NoError(t, err)
			tt.checkFunc(t, cfg)
		})
	}
}

func TestParseQuantity(t *testing.T) {
	tests := []struct {
		input   string
		want    int64
		wantErr bool
	}{
		// Decimal SI
		{"100", 100, false},
		{"1K", 1000, false},
		{"1k", 1000, false},
		{"1M", 1000000, false},
		{"1G", 1000000000, false},
		{"1T", 1000000000000, false},

		// Binary SI
		{"1Ki", 1024, false},
		{"1Mi", 1048576, false},
		{"1Gi", 1073741824, false},
		{"2Gi", 2147483648, false},
		{"1Ti", 1099511627776, false},

		// Millicores
		{"500m", 500, false},

		// Floating point
		{"1.5Gi", 1610612736, false},
		{"2.5", 2, false},

		// Invalid
		{"", 0, true},
		{"invalid", 0, true},
		{"1X", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseQuantity(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseCPUs(t *testing.T) {
	tests := []struct {
		input   string
		want    int64 // millicores
		wantErr bool
	}{
		{"1", 1000, false},
		{"2", 2000, false},
		{"0.5", 500, false},
		{"1.5", 1500, false},
		{"500m", 500, false},
		{"2000m", 2000, false},
		{"100m", 100, false},

		// Invalid
		{"", 0, true},
		{"0", 0, true},
		{"-1", 0, true},
		{"abc", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseCPUs(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatCPUs(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{1000, "1"},
		{2000, "2"},
		{500, "500m"},
		{1500, "1500m"},
		{100, "100m"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := FormatCPUs(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsValidDNSLabel(t *testing.T) {
	valid := []string{
		"default",
		"my-namespace",
		"a",
		"a1",
		"test-ns-123",
	}

	invalid := []string{
		"",
		"Invalid",
		"-invalid",
		"invalid-",
		"in_valid",
		"in.valid",
		// Too long (64+ chars)
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	for _, s := range valid {
		t.Run("valid:"+s, func(t *testing.T) {
			assert.True(t, isValidDNSLabel(s))
		})
	}

	for _, s := range invalid {
		name := s
		if name == "" {
			name = "empty"
		}
		t.Run("invalid:"+name, func(t *testing.T) {
			assert.False(t, isValidDNSLabel(s))
		})
	}
}

func TestConfigHelperMethods(t *testing.T) {
	cfg := &Config{
		Resources: ResourceConfig{
			Memory: "2Gi",
			CPUs:   "2",
		},
		Storage: StorageConfig{
			Size: "10Gi",
		},
	}

	memBytes, err := cfg.MemoryBytes()
	require.NoError(t, err)
	assert.Equal(t, int64(2147483648), memBytes)

	cpuMillis, err := cfg.CPUMillicores()
	require.NoError(t, err)
	assert.Equal(t, int64(2000), cpuMillis)

	storageBytes, err := cfg.StorageBytes()
	require.NoError(t, err)
	assert.Equal(t, int64(10737418240), storageBytes)
}
