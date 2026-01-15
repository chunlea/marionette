package admin

import (
	"context"
	"encoding/json"
	"time"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
)

// ProfileStore defines the store operations needed for profiles.
type ProfileStore interface {
	CreateProfile(ctx context.Context, profile *store.Profile) error
	GetProfile(ctx context.Context, id string) (*store.Profile, error)
	ListProfiles(ctx context.Context, opts store.ListProfilesOptions) (*store.ListResult[store.Profile], error)
	UpdateProfile(ctx context.Context, id string, updates store.ProfileUpdates) error
	DeleteProfile(ctx context.Context, id string) error
}

// ProfileAdapter adapts a ProfileStore to admin.ProfileService.
type ProfileAdapter struct {
	store ProfileStore
}

// NewProfileAdapter creates a new ProfileAdapter.
func NewProfileAdapter(s ProfileStore) *ProfileAdapter {
	return &ProfileAdapter{store: s}
}

// Create creates a new profile.
func (a *ProfileAdapter) Create(ctx context.Context, opts CreateProfileOptions) (*store.Profile, error) {
	now := time.Now()

	// Convert maps to JSON
	resourcesJSON := json.RawMessage("{}")
	networkJSON := json.RawMessage("{}")
	tunnelsJSON := json.RawMessage("[]")
	selectorJSON := json.RawMessage("{}")
	labelsJSON := json.RawMessage("{}")
	annotationsJSON := json.RawMessage("{}")

	if opts.Resources != nil {
		resourcesJSON, _ = json.Marshal(opts.Resources)
	}
	if opts.Network != nil {
		networkJSON, _ = json.Marshal(opts.Network)
	}
	if opts.Tunnels != nil {
		tunnelsJSON, _ = json.Marshal(opts.Tunnels)
	}
	if opts.Selector != nil {
		selectorJSON, _ = json.Marshal(opts.Selector)
	}
	if opts.Labels != nil {
		labelsJSON, _ = json.Marshal(opts.Labels)
	}
	if opts.Annotations != nil {
		annotationsJSON, _ = json.Marshal(opts.Annotations)
	}

	var description *string
	if opts.Description != "" {
		description = &opts.Description
	}
	var providerConfigID *string
	if opts.ProviderConfigID != "" {
		providerConfigID = &opts.ProviderConfigID
	}
	var initScript *string
	if opts.InitScript != "" {
		initScript = &opts.InitScript
	}
	var cleanupScript *string
	if opts.CleanupScript != "" {
		cleanupScript = &opts.CleanupScript
	}

	profile := &store.Profile{
		ID:               id.New("prof"),
		Name:             opts.Name,
		Description:      description,
		ProviderConfigID: providerConfigID,
		Resources:        resourcesJSON,
		Network:          networkJSON,
		InitScript:       initScript,
		CleanupScript:    cleanupScript,
		Tunnels:          tunnelsJSON,
		Selector:         selectorJSON,
		Labels:           labelsJSON,
		Annotations:      annotationsJSON,
		IsBuiltin:        false,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := a.store.CreateProfile(ctx, profile); err != nil {
		return nil, err
	}

	return profile, nil
}

// Get retrieves a profile by ID.
func (a *ProfileAdapter) Get(ctx context.Context, profileID string) (*store.Profile, error) {
	return a.store.GetProfile(ctx, profileID)
}

// List returns profiles matching the given options.
func (a *ProfileAdapter) List(ctx context.Context, opts ListProfilesOptions) (*ListResult[store.Profile], error) {
	var providerConfigID *string
	if opts.ProviderConfigID != "" {
		providerConfigID = &opts.ProviderConfigID
	}

	storeOpts := store.ListProfilesOptions{
		BaseListOptions: store.BaseListOptions{
			Limit:  opts.Limit,
			Cursor: opts.Cursor,
		},
		ProviderConfigID: providerConfigID,
		IncludeBuiltin:   opts.IncludeBuiltin,
	}

	result, err := a.store.ListProfiles(ctx, storeOpts)
	if err != nil {
		return nil, err
	}

	return &ListResult[store.Profile]{
		Items:      result.Items,
		TotalCount: result.TotalCount,
		NextCursor: result.NextCursor,
	}, nil
}

// Update updates a profile.
func (a *ProfileAdapter) Update(ctx context.Context, profileID string, opts UpdateProfileOptions) (*store.Profile, error) {
	updates := store.ProfileUpdates{
		Name:             opts.Name,
		Description:      opts.Description,
		ProviderConfigID: opts.ProviderConfigID,
		InitScript:       opts.InitScript,
		CleanupScript:    opts.CleanupScript,
	}

	if opts.Resources != nil {
		updates.Resources, _ = json.Marshal(*opts.Resources)
	}
	if opts.Network != nil {
		updates.Network, _ = json.Marshal(*opts.Network)
	}
	if opts.Tunnels != nil {
		updates.Tunnels, _ = json.Marshal(*opts.Tunnels)
	}
	if opts.Selector != nil {
		updates.Selector, _ = json.Marshal(*opts.Selector)
	}
	if opts.Labels != nil {
		updates.Labels, _ = json.Marshal(*opts.Labels)
	}
	if opts.Annotations != nil {
		updates.Annotations, _ = json.Marshal(*opts.Annotations)
	}

	if err := a.store.UpdateProfile(ctx, profileID, updates); err != nil {
		return nil, err
	}

	return a.store.GetProfile(ctx, profileID)
}

// Delete deletes a profile.
func (a *ProfileAdapter) Delete(ctx context.Context, profileID string) error {
	return a.store.DeleteProfile(ctx, profileID)
}

// Ensure ProfileAdapter implements ProfileService.
var _ ProfileService = (*ProfileAdapter)(nil)
