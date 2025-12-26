package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectSandboxCapabilities(t *testing.T) {
	caps := DetectSandboxCapabilities()

	// Should always return at least one capability (even if "none")
	assert.NotEmpty(t, caps.Types)

	// The types should be valid sandbox types
	validTypes := map[string]bool{
		"docker":       true,
		"gvisor":       true,
		"namespace":    true,
		"sandbox-exec": true,
		"none":         true,
	}

	for _, typ := range caps.Types {
		assert.True(t, validTypes[typ], "unexpected sandbox type: %s", typ)
	}
}

func TestSandboxCapabilities_HasCapability(t *testing.T) {
	caps := SandboxCapabilities{
		Types: []string{"docker", "namespace"},
	}

	assert.True(t, caps.HasCapability("docker"))
	assert.True(t, caps.HasCapability("namespace"))
	assert.False(t, caps.HasCapability("gvisor"))
	assert.False(t, caps.HasCapability("none"))
}

func TestSandboxCapabilities_HasCapability_Empty(t *testing.T) {
	caps := SandboxCapabilities{
		Types: []string{},
	}

	assert.False(t, caps.HasCapability("docker"))
	assert.False(t, caps.HasCapability("none"))
}

func TestSandboxCapabilities_HasCapability_None(t *testing.T) {
	caps := SandboxCapabilities{
		Types: []string{"none"},
	}

	assert.True(t, caps.HasCapability("none"))
	assert.False(t, caps.HasCapability("docker"))
}

// Note: The individual *Available() functions are internal and tested
// implicitly through DetectSandboxCapabilities(). They depend on system
// state (installed binaries, permissions) and are difficult to unit test
// without mocking the filesystem and exec.LookPath.
//
// Integration tests would verify these work correctly on different platforms.
