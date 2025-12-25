package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/store"
)

// =============================================================================
// ProviderConfig Tests
// =============================================================================

func TestProviderConfigCRUD(t *testing.T) {
	ctx := context.Background()

	// Create
	config := &store.ProviderConfig{
		Name:     "test-provider-" + time.Now().Format("150405"),
		Provider: "docker",
	}

	err := testStore.CreateProviderConfig(ctx, config)
	require.NoError(t, err)
	assert.NotEmpty(t, config.ID)

	// Get
	got, err := testStore.GetProviderConfig(ctx, config.ID)
	require.NoError(t, err)
	assert.Equal(t, "docker", got.Provider)

	// Update
	isDefault := true
	err = testStore.UpdateProviderConfig(ctx, config.ID, store.ProviderConfigUpdates{
		IsDefault: &isDefault,
	})
	require.NoError(t, err)

	got, err = testStore.GetProviderConfig(ctx, config.ID)
	require.NoError(t, err)
	assert.True(t, got.IsDefault)

	// Get default
	defaultConfig, err := testStore.GetDefaultProviderConfig(ctx, "docker")
	require.NoError(t, err)
	assert.Equal(t, config.ID, defaultConfig.ID)

	// Delete
	err = testStore.DeleteProviderConfig(ctx, config.ID)
	require.NoError(t, err)
}

// =============================================================================
// AgentConfig Tests
// =============================================================================

func TestAgentConfigCRUD(t *testing.T) {
	ctx := context.Background()

	// Create
	config := &store.AgentConfig{
		Name:            "test-agent-config-" + time.Now().Format("150405"),
		Agent:           "claude",
		APIKeyEncrypted: "encrypted-key-data",
		Model:           strPtr("claude-3-opus"),
		IsDefault:       false,
	}

	err := testStore.CreateAgentConfig(ctx, config)
	require.NoError(t, err)
	assert.NotEmpty(t, config.ID)
	assert.NotZero(t, config.CreatedAt)
	assert.NotZero(t, config.UpdatedAt)

	// Get
	got, err := testStore.GetAgentConfig(ctx, config.ID)
	require.NoError(t, err)
	assert.Equal(t, config.Name, got.Name)
	assert.Equal(t, "claude", got.Agent)
	assert.Equal(t, "encrypted-key-data", got.APIKeyEncrypted)
	assert.Equal(t, "claude-3-opus", *got.Model)

	// Get by name
	got, err = testStore.GetAgentConfigByName(ctx, config.Name)
	require.NoError(t, err)
	assert.Equal(t, config.ID, got.ID)

	// Update
	newModel := "claude-3-sonnet"
	err = testStore.UpdateAgentConfig(ctx, config.ID, store.AgentConfigUpdates{
		Model: &newModel,
	})
	require.NoError(t, err)

	got, err = testStore.GetAgentConfig(ctx, config.ID)
	require.NoError(t, err)
	assert.Equal(t, "claude-3-sonnet", *got.Model)

	// Delete
	err = testStore.DeleteAgentConfig(ctx, config.ID)
	require.NoError(t, err)

	_, err = testStore.GetAgentConfig(ctx, config.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestAgentConfigDefaultAgent(t *testing.T) {
	ctx := context.Background()

	// Create default config
	config := &store.AgentConfig{
		Name:            "default-claude-" + time.Now().Format("150405"),
		Agent:           "claude",
		APIKeyEncrypted: "encrypted-default-key",
		IsDefault:       true,
	}

	err := testStore.CreateAgentConfig(ctx, config)
	require.NoError(t, err)

	// Get default
	got, err := testStore.GetDefaultAgentConfig(ctx, "claude")
	require.NoError(t, err)
	assert.Equal(t, config.ID, got.ID)
	assert.True(t, got.IsDefault)

	// Cleanup
	err = testStore.DeleteAgentConfig(ctx, config.ID)
	require.NoError(t, err)
}

// =============================================================================
// Profile Tests
// =============================================================================

func TestProfileCRUD(t *testing.T) {
	ctx := context.Background()

	// Create a provider config first (optional for profile)
	providerConfig := &store.ProviderConfig{
		Name:     "profile-test-provider-" + time.Now().Format("150405"),
		Provider: "docker",
	}
	err := testStore.CreateProviderConfig(ctx, providerConfig)
	require.NoError(t, err)

	// Create profile
	profile := &store.Profile{
		Name:             "test-profile-" + time.Now().Format("150405"),
		Description:      strPtr("Test profile description"),
		ProviderConfigID: &providerConfig.ID,
		IsBuiltin:        false,
	}

	err = testStore.CreateProfile(ctx, profile)
	require.NoError(t, err)
	assert.NotEmpty(t, profile.ID)
	assert.NotZero(t, profile.CreatedAt)
	assert.NotZero(t, profile.UpdatedAt)

	// Get
	got, err := testStore.GetProfile(ctx, profile.ID)
	require.NoError(t, err)
	assert.Equal(t, profile.Name, got.Name)
	assert.Equal(t, "Test profile description", *got.Description)
	assert.Equal(t, providerConfig.ID, *got.ProviderConfigID)

	// Get by name
	got, err = testStore.GetProfileByName(ctx, profile.Name)
	require.NoError(t, err)
	assert.Equal(t, profile.ID, got.ID)

	// Update
	newDescription := "Updated description"
	err = testStore.UpdateProfile(ctx, profile.ID, store.ProfileUpdates{
		Description: &newDescription,
	})
	require.NoError(t, err)

	got, err = testStore.GetProfile(ctx, profile.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated description", *got.Description)

	// Delete
	err = testStore.DeleteProfile(ctx, profile.ID)
	require.NoError(t, err)

	_, err = testStore.GetProfile(ctx, profile.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// Cleanup provider config
	err = testStore.DeleteProviderConfig(ctx, providerConfig.ID)
	require.NoError(t, err)
}

func TestProfileWithoutProviderConfig(t *testing.T) {
	ctx := context.Background()

	// Create profile without provider config
	profile := &store.Profile{
		Name:      "standalone-profile-" + time.Now().Format("150405"),
		IsBuiltin: false,
	}

	err := testStore.CreateProfile(ctx, profile)
	require.NoError(t, err)
	assert.NotEmpty(t, profile.ID)

	// Get
	got, err := testStore.GetProfile(ctx, profile.ID)
	require.NoError(t, err)
	assert.Equal(t, profile.Name, got.Name)
	assert.Nil(t, got.ProviderConfigID)

	// Cleanup
	err = testStore.DeleteProfile(ctx, profile.ID)
	require.NoError(t, err)
}

func TestGetProviderConfigByName(t *testing.T) {
	ctx := context.Background()

	// Create a provider config with unique name
	config := &store.ProviderConfig{
		Name:     "test-provider-byname-" + time.Now().Format("150405"),
		Provider: "docker",
	}

	err := testStore.CreateProviderConfig(ctx, config)
	require.NoError(t, err)
	assert.NotEmpty(t, config.ID)

	// Get by name
	got, err := testStore.GetProviderConfigByName(ctx, config.Name)
	require.NoError(t, err)
	assert.Equal(t, config.ID, got.ID)
	assert.Equal(t, config.Name, got.Name)
	assert.Equal(t, "docker", got.Provider)

	// Test that non-existent name returns ErrNotFound
	_, err = testStore.GetProviderConfigByName(ctx, "non-existent-provider-name")
	assert.ErrorIs(t, err, store.ErrNotFound)

	// Cleanup
	err = testStore.DeleteProviderConfig(ctx, config.ID)
	require.NoError(t, err)
}

func TestListProviderConfigs(t *testing.T) {
	ctx := context.Background()

	// Create 3 provider configs
	configs := []*store.ProviderConfig{
		{
			Name:     "test-provider-list-1-" + time.Now().Format("150405"),
			Provider: "docker",
		},
		{
			Name:     "test-provider-list-2-" + time.Now().Format("150405"),
			Provider: "kubernetes",
		},
		{
			Name:     "test-provider-list-3-" + time.Now().Format("150405"),
			Provider: "docker",
		},
	}

	for _, config := range configs {
		err := testStore.CreateProviderConfig(ctx, config)
		require.NoError(t, err)
	}

	// List all provider configs
	allConfigs, err := testStore.ListProviderConfigs(ctx, store.ListProviderConfigsOptions{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(allConfigs.Items), 3, "Should have at least 3 provider configs")

	// Test filtering by provider (docker)
	dockerProvider := "docker"
	dockerConfigs, err := testStore.ListProviderConfigs(ctx, store.ListProviderConfigsOptions{
		Provider: &dockerProvider,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(dockerConfigs.Items), 2, "Should have at least 2 docker configs")
	for _, config := range dockerConfigs.Items {
		assert.Equal(t, "docker", config.Provider)
	}

	// Test filtering by provider (kubernetes)
	k8sProvider := "kubernetes"
	k8sConfigs, err := testStore.ListProviderConfigs(ctx, store.ListProviderConfigsOptions{
		Provider: &k8sProvider,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(k8sConfigs.Items), 1, "Should have at least 1 kubernetes config")
	for _, config := range k8sConfigs.Items {
		assert.Equal(t, "kubernetes", config.Provider)
	}

	// Cleanup
	for _, config := range configs {
		err := testStore.DeleteProviderConfig(ctx, config.ID)
		require.NoError(t, err)
	}
}

func TestListAgentConfigs(t *testing.T) {
	ctx := context.Background()

	// Create 3 agent configs
	configs := []*store.AgentConfig{
		{
			Name:            "test-agent-list-1-" + time.Now().Format("150405"),
			Agent:           "claude",
			APIKeyEncrypted: "encrypted-key-1",
		},
		{
			Name:            "test-agent-list-2-" + time.Now().Format("150405"),
			Agent:           "openai",
			APIKeyEncrypted: "encrypted-key-2",
		},
		{
			Name:            "test-agent-list-3-" + time.Now().Format("150405"),
			Agent:           "claude",
			APIKeyEncrypted: "encrypted-key-3",
		},
	}

	for _, config := range configs {
		err := testStore.CreateAgentConfig(ctx, config)
		require.NoError(t, err)
	}

	// List all agent configs
	allConfigs, err := testStore.ListAgentConfigs(ctx, store.ListAgentConfigsOptions{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(allConfigs.Items), 3, "Should have at least 3 agent configs")

	// Test filtering by agent (claude)
	claudeAgent := "claude"
	claudeConfigs, err := testStore.ListAgentConfigs(ctx, store.ListAgentConfigsOptions{
		Agent: &claudeAgent,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(claudeConfigs.Items), 2, "Should have at least 2 claude configs")
	for _, config := range claudeConfigs.Items {
		assert.Equal(t, "claude", config.Agent)
	}

	// Test filtering by agent (openai)
	openaiAgent := "openai"
	openaiConfigs, err := testStore.ListAgentConfigs(ctx, store.ListAgentConfigsOptions{
		Agent: &openaiAgent,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(openaiConfigs.Items), 1, "Should have at least 1 openai config")
	for _, config := range openaiConfigs.Items {
		assert.Equal(t, "openai", config.Agent)
	}

	// Cleanup
	for _, config := range configs {
		err := testStore.DeleteAgentConfig(ctx, config.ID)
		require.NoError(t, err)
	}
}

func TestListProfiles(t *testing.T) {
	ctx := context.Background()

	// Create 3 profiles
	profiles := []*store.Profile{
		{
			Name:      "test-profile-list-1-" + time.Now().Format("150405"),
			IsBuiltin: false,
		},
		{
			Name:      "test-profile-list-2-" + time.Now().Format("150405"),
			IsBuiltin: false,
		},
		{
			Name:      "test-profile-list-3-" + time.Now().Format("150405"),
			IsBuiltin: false,
		},
	}

	for _, profile := range profiles {
		err := testStore.CreateProfile(ctx, profile)
		require.NoError(t, err)
	}

	// List all profiles
	allProfiles, err := testStore.ListProfiles(ctx, store.ListProfilesOptions{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(allProfiles.Items), 3, "Should have at least 3 profiles")

	// Cleanup
	for _, profile := range profiles {
		err := testStore.DeleteProfile(ctx, profile.ID)
		require.NoError(t, err)
	}
}

// =============================================================================
// Helper Functions
// =============================================================================
