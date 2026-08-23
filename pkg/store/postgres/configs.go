package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
)

// AgentConfig column list for SELECT queries.
const agentConfigColumns = `id, name, agent, api_key_encrypted, model, base_url, extra,
	is_default, tenant_id, labels, annotations, created_at, updated_at`

// ProviderConfig column list for SELECT queries.
const providerConfigColumns = `id, name, provider, config, suspend_config, is_default,
	tenant_id, labels, annotations, created_at, updated_at`

// Profile column list for SELECT queries.
const profileColumns = `id, name, description, provider_config_id, tenant_id, resources, network,
	init_script, cleanup_script, tunnels, selector, labels, annotations, is_builtin, created_at, updated_at`

// =============================================================================
// AgentConfig CRUD
// =============================================================================

// CreateAgentConfig creates a new agent config.
func (s *Store) CreateAgentConfig(ctx context.Context, config *store.AgentConfig) error {
	return createAgentConfig(ctx, s.pool, config)
}

// CreateAgentConfig creates a new agent config within a transaction.
func (t *Tx) CreateAgentConfig(ctx context.Context, config *store.AgentConfig) error {
	return createAgentConfig(ctx, t.tx, config)
}

func createAgentConfig(ctx context.Context, q querier, config *store.AgentConfig) error {
	if config.ID == "" {
		config.ID = id.AgentConfig()
	}

	query := `
		INSERT INTO agent_configs (
			id, name, agent, api_key_encrypted, model, base_url, extra,
			is_default, tenant_id, labels, annotations, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), NOW()
		)
		RETURNING created_at, updated_at`

	err := q.QueryRow(ctx, query,
		config.ID, config.Name, config.Agent, config.APIKeyEncrypted, config.Model, config.BaseURL,
		emptyJSONObject(config.Extra), config.IsDefault, config.TenantID,
		emptyJSONObject(config.Labels), emptyJSONObject(config.Annotations),
	).Scan(&config.CreatedAt, &config.UpdatedAt)

	if err != nil {
		return handlePgError(err, "agent_config", config.Name)
	}
	return nil
}

// GetAgentConfig retrieves an agent config by ID.
func (s *Store) GetAgentConfig(ctx context.Context, configID string) (*store.AgentConfig, error) {
	return getAgentConfig(ctx, s.pool, configID)
}

// GetAgentConfig retrieves an agent config by ID within a transaction.
func (t *Tx) GetAgentConfig(ctx context.Context, configID string) (*store.AgentConfig, error) {
	return getAgentConfig(ctx, t.tx, configID)
}

func getAgentConfig(ctx context.Context, q querier, configID string) (*store.AgentConfig, error) {
	query := fmt.Sprintf(`SELECT %s FROM agent_configs WHERE id = $1`, agentConfigColumns)
	row := q.QueryRow(ctx, query, configID)
	return scanAgentConfig(row, configID)
}

// GetAgentConfigByName retrieves an agent config by name.
func (s *Store) GetAgentConfigByName(ctx context.Context, name string) (*store.AgentConfig, error) {
	return getAgentConfigByName(ctx, s.pool, name)
}

// GetAgentConfigByName retrieves an agent config by name within a transaction.
func (t *Tx) GetAgentConfigByName(ctx context.Context, name string) (*store.AgentConfig, error) {
	return getAgentConfigByName(ctx, t.tx, name)
}

func getAgentConfigByName(ctx context.Context, q querier, name string) (*store.AgentConfig, error) {
	query := fmt.Sprintf(`SELECT %s FROM agent_configs WHERE name = $1`, agentConfigColumns)
	row := q.QueryRow(ctx, query, name)
	return scanAgentConfig(row, name)
}

// GetDefaultAgentConfig retrieves the default agent config for an agent type.
func (s *Store) GetDefaultAgentConfig(ctx context.Context, agent string) (*store.AgentConfig, error) {
	return getDefaultAgentConfig(ctx, s.pool, agent)
}

// GetDefaultAgentConfig retrieves the default agent config within a transaction.
func (t *Tx) GetDefaultAgentConfig(ctx context.Context, agent string) (*store.AgentConfig, error) {
	return getDefaultAgentConfig(ctx, t.tx, agent)
}

func getDefaultAgentConfig(ctx context.Context, q querier, agent string) (*store.AgentConfig, error) {
	query := fmt.Sprintf(`SELECT %s FROM agent_configs WHERE agent = $1 AND is_default = TRUE`, agentConfigColumns)
	row := q.QueryRow(ctx, query, agent)
	return scanAgentConfig(row, fmt.Sprintf("default:%s", agent))
}

// ListAgentConfigs retrieves agent configs with optional filtering.
func (s *Store) ListAgentConfigs(ctx context.Context, opts store.ListAgentConfigsOptions) (*store.ListResult[store.AgentConfig], error) {
	return listAgentConfigs(ctx, s.pool, opts)
}

// ListAgentConfigs retrieves agent configs within a transaction.
func (t *Tx) ListAgentConfigs(ctx context.Context, opts store.ListAgentConfigsOptions) (*store.ListResult[store.AgentConfig], error) {
	return listAgentConfigs(ctx, t.tx, opts)
}

func listAgentConfigs(ctx context.Context, q querier, opts store.ListAgentConfigsOptions) (*store.ListResult[store.AgentConfig], error) {
	var conditions []string
	var args []any
	argNum := 1

	if opts.Agent != nil {
		conditions = append(conditions, fmt.Sprintf("agent = $%d", argNum))
		args = append(args, *opts.Agent)
		argNum++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := defaultLimit(opts.Limit)
	orderBy, err := agentConfigSortColumns.orderClause(opts.OrderBy, opts.OrderDesc)
	if err != nil {
		return nil, err
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM agent_configs %s", whereClause)
	var totalCount int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("counting agent_configs: %w", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT %s FROM agent_configs %s
		ORDER BY %s
		LIMIT $%d`,
		agentConfigColumns, whereClause, orderBy, argNum)
	dataArgs := append(args, limit+1) //nolint:gocritic // intentionally creating new slice

	rows, err := q.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying agent_configs: %w", err)
	}
	defer rows.Close()

	var configs []*store.AgentConfig
	for rows.Next() {
		config, err := scanAgentConfigFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning agent_config: %w", err)
		}
		configs = append(configs, config)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating agent_configs: %w", err)
	}

	hasMore := len(configs) > limit
	if hasMore {
		configs = configs[:limit]
	}

	return &store.ListResult[store.AgentConfig]{
		Items:      configs,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}

// UpdateAgentConfig updates agent config fields.
func (s *Store) UpdateAgentConfig(ctx context.Context, configID string, updates store.AgentConfigUpdates) error {
	return updateAgentConfig(ctx, s.pool, configID, updates)
}

// UpdateAgentConfig updates agent config fields within a transaction.
func (t *Tx) UpdateAgentConfig(ctx context.Context, configID string, updates store.AgentConfigUpdates) error {
	return updateAgentConfig(ctx, t.tx, configID, updates)
}

func updateAgentConfig(ctx context.Context, q querier, configID string, updates store.AgentConfigUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argNum))
		args = append(args, *updates.Name)
		argNum++
	}
	if updates.APIKeyEncrypted != nil {
		setClauses = append(setClauses, fmt.Sprintf("api_key_encrypted = $%d", argNum))
		args = append(args, *updates.APIKeyEncrypted)
		argNum++
	}
	if updates.Model != nil {
		setClauses = append(setClauses, fmt.Sprintf("model = $%d", argNum))
		args = append(args, *updates.Model)
		argNum++
	}
	if updates.BaseURL != nil {
		setClauses = append(setClauses, fmt.Sprintf("base_url = $%d", argNum))
		args = append(args, *updates.BaseURL)
		argNum++
	}
	if updates.Extra != nil {
		setClauses = append(setClauses, fmt.Sprintf("extra = $%d", argNum))
		args = append(args, updates.Extra)
		argNum++
	}
	if updates.IsDefault != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_default = $%d", argNum))
		args = append(args, *updates.IsDefault)
		argNum++
	}
	if updates.Labels != nil {
		setClauses = append(setClauses, fmt.Sprintf("labels = $%d", argNum))
		args = append(args, updates.Labels)
		argNum++
	}
	if updates.Annotations != nil {
		setClauses = append(setClauses, fmt.Sprintf("annotations = $%d", argNum))
		args = append(args, updates.Annotations)
		argNum++
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`UPDATE agent_configs SET %s WHERE id = $%d`,
		strings.Join(setClauses, ", "), argNum)
	args = append(args, configID)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "agent_config", configID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "agent_config", ID: configID}
	}

	return nil
}

// DeleteAgentConfig deletes an agent config.
func (s *Store) DeleteAgentConfig(ctx context.Context, configID string) error {
	return deleteAgentConfig(ctx, s.pool, configID)
}

// DeleteAgentConfig deletes an agent config within a transaction.
func (t *Tx) DeleteAgentConfig(ctx context.Context, configID string) error {
	return deleteAgentConfig(ctx, t.tx, configID)
}

func deleteAgentConfig(ctx context.Context, q querier, configID string) error {
	query := `DELETE FROM agent_configs WHERE id = $1`
	result, err := q.Exec(ctx, query, configID)
	if err != nil {
		return handlePgError(err, "agent_config", configID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "agent_config", ID: configID}
	}

	return nil
}

func scanAgentConfig(row pgx.Row, identifier string) (*store.AgentConfig, error) {
	var c store.AgentConfig
	err := row.Scan(
		&c.ID, &c.Name, &c.Agent, &c.APIKeyEncrypted, &c.Model, &c.BaseURL, &c.Extra,
		&c.IsDefault, &c.TenantID, &c.Labels, &c.Annotations, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "agent_config", ID: identifier}
		}
		return nil, fmt.Errorf("scanning agent_config: %w", err)
	}
	return &c, nil
}

func scanAgentConfigFromRows(rows pgx.Rows) (*store.AgentConfig, error) {
	var c store.AgentConfig
	err := rows.Scan(
		&c.ID, &c.Name, &c.Agent, &c.APIKeyEncrypted, &c.Model, &c.BaseURL, &c.Extra,
		&c.IsDefault, &c.TenantID, &c.Labels, &c.Annotations, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// =============================================================================
// ProviderConfig CRUD
// =============================================================================

// CreateProviderConfig creates a new provider config.
func (s *Store) CreateProviderConfig(ctx context.Context, config *store.ProviderConfig) error {
	return createProviderConfig(ctx, s.pool, config)
}

// CreateProviderConfig creates a new provider config within a transaction.
func (t *Tx) CreateProviderConfig(ctx context.Context, config *store.ProviderConfig) error {
	return createProviderConfig(ctx, t.tx, config)
}

func createProviderConfig(ctx context.Context, q querier, config *store.ProviderConfig) error {
	if config.ID == "" {
		config.ID = id.ProviderConfig()
	}

	query := `
		INSERT INTO provider_configs (
			id, name, provider, config, suspend_config, is_default,
			tenant_id, labels, annotations, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW()
		)
		RETURNING created_at, updated_at`

	err := q.QueryRow(ctx, query,
		config.ID, config.Name, config.Provider, emptyJSONObject(config.Config),
		emptyJSONObject(config.SuspendConfig), config.IsDefault, config.TenantID,
		emptyJSONObject(config.Labels), emptyJSONObject(config.Annotations),
	).Scan(&config.CreatedAt, &config.UpdatedAt)

	if err != nil {
		return handlePgError(err, "provider_config", config.Name)
	}
	return nil
}

// GetProviderConfig retrieves a provider config by ID.
func (s *Store) GetProviderConfig(ctx context.Context, configID string) (*store.ProviderConfig, error) {
	return getProviderConfig(ctx, s.pool, configID)
}

// GetProviderConfig retrieves a provider config by ID within a transaction.
func (t *Tx) GetProviderConfig(ctx context.Context, configID string) (*store.ProviderConfig, error) {
	return getProviderConfig(ctx, t.tx, configID)
}

func getProviderConfig(ctx context.Context, q querier, configID string) (*store.ProviderConfig, error) {
	query := fmt.Sprintf(`SELECT %s FROM provider_configs WHERE id = $1`, providerConfigColumns)
	row := q.QueryRow(ctx, query, configID)
	return scanProviderConfig(row, configID)
}

// GetProviderConfigByName retrieves a provider config by name.
func (s *Store) GetProviderConfigByName(ctx context.Context, name string) (*store.ProviderConfig, error) {
	return getProviderConfigByName(ctx, s.pool, name)
}

// GetProviderConfigByName retrieves a provider config by name within a transaction.
func (t *Tx) GetProviderConfigByName(ctx context.Context, name string) (*store.ProviderConfig, error) {
	return getProviderConfigByName(ctx, t.tx, name)
}

func getProviderConfigByName(ctx context.Context, q querier, name string) (*store.ProviderConfig, error) {
	query := fmt.Sprintf(`SELECT %s FROM provider_configs WHERE name = $1`, providerConfigColumns)
	row := q.QueryRow(ctx, query, name)
	return scanProviderConfig(row, name)
}

// GetDefaultProviderConfig retrieves the default provider config for a provider type.
func (s *Store) GetDefaultProviderConfig(ctx context.Context, provider string) (*store.ProviderConfig, error) {
	return getDefaultProviderConfig(ctx, s.pool, provider)
}

// GetDefaultProviderConfig retrieves the default provider config within a transaction.
func (t *Tx) GetDefaultProviderConfig(ctx context.Context, provider string) (*store.ProviderConfig, error) {
	return getDefaultProviderConfig(ctx, t.tx, provider)
}

func getDefaultProviderConfig(ctx context.Context, q querier, provider string) (*store.ProviderConfig, error) {
	query := fmt.Sprintf(`SELECT %s FROM provider_configs WHERE provider = $1 AND is_default = TRUE`, providerConfigColumns)
	row := q.QueryRow(ctx, query, provider)
	return scanProviderConfig(row, fmt.Sprintf("default:%s", provider))
}

// ListProviderConfigs retrieves provider configs with optional filtering.
func (s *Store) ListProviderConfigs(ctx context.Context, opts store.ListProviderConfigsOptions) (*store.ListResult[store.ProviderConfig], error) {
	return listProviderConfigs(ctx, s.pool, opts)
}

// ListProviderConfigs retrieves provider configs within a transaction.
func (t *Tx) ListProviderConfigs(ctx context.Context, opts store.ListProviderConfigsOptions) (*store.ListResult[store.ProviderConfig], error) {
	return listProviderConfigs(ctx, t.tx, opts)
}

func listProviderConfigs(ctx context.Context, q querier, opts store.ListProviderConfigsOptions) (*store.ListResult[store.ProviderConfig], error) {
	var conditions []string
	var args []any
	argNum := 1

	if opts.Provider != nil {
		conditions = append(conditions, fmt.Sprintf("provider = $%d", argNum))
		args = append(args, *opts.Provider)
		argNum++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := defaultLimit(opts.Limit)
	orderBy, err := providerConfigSortColumns.orderClause(opts.OrderBy, opts.OrderDesc)
	if err != nil {
		return nil, err
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM provider_configs %s", whereClause)
	var totalCount int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("counting provider_configs: %w", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT %s FROM provider_configs %s
		ORDER BY %s
		LIMIT $%d`,
		providerConfigColumns, whereClause, orderBy, argNum)
	dataArgs := append(args, limit+1) //nolint:gocritic // intentionally creating new slice

	rows, err := q.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying provider_configs: %w", err)
	}
	defer rows.Close()

	var configs []*store.ProviderConfig
	for rows.Next() {
		config, err := scanProviderConfigFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning provider_config: %w", err)
		}
		configs = append(configs, config)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating provider_configs: %w", err)
	}

	hasMore := len(configs) > limit
	if hasMore {
		configs = configs[:limit]
	}

	return &store.ListResult[store.ProviderConfig]{
		Items:      configs,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}

// UpdateProviderConfig updates provider config fields.
func (s *Store) UpdateProviderConfig(ctx context.Context, configID string, updates store.ProviderConfigUpdates) error {
	return updateProviderConfig(ctx, s.pool, configID, updates)
}

// UpdateProviderConfig updates provider config fields within a transaction.
func (t *Tx) UpdateProviderConfig(ctx context.Context, configID string, updates store.ProviderConfigUpdates) error {
	return updateProviderConfig(ctx, t.tx, configID, updates)
}

func updateProviderConfig(ctx context.Context, q querier, configID string, updates store.ProviderConfigUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argNum))
		args = append(args, *updates.Name)
		argNum++
	}
	if updates.Config != nil {
		setClauses = append(setClauses, fmt.Sprintf("config = $%d", argNum))
		args = append(args, updates.Config)
		argNum++
	}
	if updates.SuspendConfig != nil {
		setClauses = append(setClauses, fmt.Sprintf("suspend_config = $%d", argNum))
		args = append(args, updates.SuspendConfig)
		argNum++
	}
	if updates.IsDefault != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_default = $%d", argNum))
		args = append(args, *updates.IsDefault)
		argNum++
	}
	if updates.Labels != nil {
		setClauses = append(setClauses, fmt.Sprintf("labels = $%d", argNum))
		args = append(args, updates.Labels)
		argNum++
	}
	if updates.Annotations != nil {
		setClauses = append(setClauses, fmt.Sprintf("annotations = $%d", argNum))
		args = append(args, updates.Annotations)
		argNum++
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`UPDATE provider_configs SET %s WHERE id = $%d`,
		strings.Join(setClauses, ", "), argNum)
	args = append(args, configID)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "provider_config", configID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "provider_config", ID: configID}
	}

	return nil
}

// DeleteProviderConfig deletes a provider config.
func (s *Store) DeleteProviderConfig(ctx context.Context, configID string) error {
	return deleteProviderConfig(ctx, s.pool, configID)
}

// DeleteProviderConfig deletes a provider config within a transaction.
func (t *Tx) DeleteProviderConfig(ctx context.Context, configID string) error {
	return deleteProviderConfig(ctx, t.tx, configID)
}

func deleteProviderConfig(ctx context.Context, q querier, configID string) error {
	query := `DELETE FROM provider_configs WHERE id = $1`
	result, err := q.Exec(ctx, query, configID)
	if err != nil {
		return handlePgError(err, "provider_config", configID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "provider_config", ID: configID}
	}

	return nil
}

func scanProviderConfig(row pgx.Row, identifier string) (*store.ProviderConfig, error) {
	var c store.ProviderConfig
	err := row.Scan(
		&c.ID, &c.Name, &c.Provider, &c.Config, &c.SuspendConfig, &c.IsDefault,
		&c.TenantID, &c.Labels, &c.Annotations, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "provider_config", ID: identifier}
		}
		return nil, fmt.Errorf("scanning provider_config: %w", err)
	}
	return &c, nil
}

func scanProviderConfigFromRows(rows pgx.Rows) (*store.ProviderConfig, error) {
	var c store.ProviderConfig
	err := rows.Scan(
		&c.ID, &c.Name, &c.Provider, &c.Config, &c.SuspendConfig, &c.IsDefault,
		&c.TenantID, &c.Labels, &c.Annotations, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// =============================================================================
// Profile CRUD
// =============================================================================

// CreateProfile creates a new profile.
func (s *Store) CreateProfile(ctx context.Context, profile *store.Profile) error {
	return createProfile(ctx, s.pool, profile)
}

// CreateProfile creates a new profile within a transaction.
func (t *Tx) CreateProfile(ctx context.Context, profile *store.Profile) error {
	return createProfile(ctx, t.tx, profile)
}

func createProfile(ctx context.Context, q querier, profile *store.Profile) error {
	if profile.ID == "" {
		profile.ID = id.Profile()
	}

	query := `
		INSERT INTO profiles (
			id, name, description, provider_config_id, tenant_id, resources, network,
			init_script, cleanup_script, tunnels, selector, labels, annotations, is_builtin,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, NOW(), NOW()
		)
		RETURNING created_at, updated_at`

	err := q.QueryRow(ctx, query,
		profile.ID, profile.Name, profile.Description, profile.ProviderConfigID, profile.TenantID,
		emptyJSONObject(profile.Resources), emptyJSONObject(profile.Network),
		profile.InitScript, profile.CleanupScript, emptyJSONArray(profile.Tunnels),
		emptyJSONObject(profile.Selector), emptyJSONObject(profile.Labels), emptyJSONObject(profile.Annotations),
		profile.IsBuiltin,
	).Scan(&profile.CreatedAt, &profile.UpdatedAt)

	if err != nil {
		return handlePgError(err, "profile", profile.Name)
	}
	return nil
}

// GetProfile retrieves a profile by ID.
func (s *Store) GetProfile(ctx context.Context, profileID string) (*store.Profile, error) {
	return getProfile(ctx, s.pool, profileID)
}

// GetProfile retrieves a profile by ID within a transaction.
func (t *Tx) GetProfile(ctx context.Context, profileID string) (*store.Profile, error) {
	return getProfile(ctx, t.tx, profileID)
}

func getProfile(ctx context.Context, q querier, profileID string) (*store.Profile, error) {
	query := fmt.Sprintf(`SELECT %s FROM profiles WHERE id = $1`, profileColumns)
	row := q.QueryRow(ctx, query, profileID)
	return scanProfile(row, profileID)
}

// GetProfileByName retrieves a profile by name.
func (s *Store) GetProfileByName(ctx context.Context, name string) (*store.Profile, error) {
	return getProfileByName(ctx, s.pool, name)
}

// GetProfileByName retrieves a profile by name within a transaction.
func (t *Tx) GetProfileByName(ctx context.Context, name string) (*store.Profile, error) {
	return getProfileByName(ctx, t.tx, name)
}

func getProfileByName(ctx context.Context, q querier, name string) (*store.Profile, error) {
	query := fmt.Sprintf(`SELECT %s FROM profiles WHERE name = $1`, profileColumns)
	row := q.QueryRow(ctx, query, name)
	return scanProfile(row, name)
}

// ListProfiles retrieves profiles with optional filtering.
func (s *Store) ListProfiles(ctx context.Context, opts store.ListProfilesOptions) (*store.ListResult[store.Profile], error) {
	return listProfiles(ctx, s.pool, opts)
}

// ListProfiles retrieves profiles within a transaction.
func (t *Tx) ListProfiles(ctx context.Context, opts store.ListProfilesOptions) (*store.ListResult[store.Profile], error) {
	return listProfiles(ctx, t.tx, opts)
}

func listProfiles(ctx context.Context, q querier, opts store.ListProfilesOptions) (*store.ListResult[store.Profile], error) {
	var conditions []string
	var args []any
	argNum := 1

	if opts.ProviderConfigID != nil {
		conditions = append(conditions, fmt.Sprintf("provider_config_id = $%d", argNum))
		args = append(args, *opts.ProviderConfigID)
		argNum++
	}
	if !opts.IncludeBuiltin {
		conditions = append(conditions, "is_builtin = FALSE")
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := defaultLimit(opts.Limit)
	orderBy, err := profileSortColumns.orderClause(opts.OrderBy, opts.OrderDesc)
	if err != nil {
		return nil, err
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM profiles %s", whereClause)
	var totalCount int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("counting profiles: %w", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT %s FROM profiles %s
		ORDER BY %s
		LIMIT $%d`,
		profileColumns, whereClause, orderBy, argNum)
	dataArgs := append(args, limit+1) //nolint:gocritic // intentionally creating new slice

	rows, err := q.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying profiles: %w", err)
	}
	defer rows.Close()

	var profiles []*store.Profile
	for rows.Next() {
		profile, err := scanProfileFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning profile: %w", err)
		}
		profiles = append(profiles, profile)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating profiles: %w", err)
	}

	hasMore := len(profiles) > limit
	if hasMore {
		profiles = profiles[:limit]
	}

	return &store.ListResult[store.Profile]{
		Items:      profiles,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}

// UpdateProfile updates profile fields.
func (s *Store) UpdateProfile(ctx context.Context, profileID string, updates store.ProfileUpdates) error {
	return updateProfile(ctx, s.pool, profileID, updates)
}

// UpdateProfile updates profile fields within a transaction.
func (t *Tx) UpdateProfile(ctx context.Context, profileID string, updates store.ProfileUpdates) error {
	return updateProfile(ctx, t.tx, profileID, updates)
}

func updateProfile(ctx context.Context, q querier, profileID string, updates store.ProfileUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argNum))
		args = append(args, *updates.Name)
		argNum++
	}
	if updates.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argNum))
		args = append(args, *updates.Description)
		argNum++
	}
	if updates.ProviderConfigID != nil {
		setClauses = append(setClauses, fmt.Sprintf("provider_config_id = $%d", argNum))
		args = append(args, *updates.ProviderConfigID)
		argNum++
	}
	if updates.Resources != nil {
		setClauses = append(setClauses, fmt.Sprintf("resources = $%d", argNum))
		args = append(args, updates.Resources)
		argNum++
	}
	if updates.Network != nil {
		setClauses = append(setClauses, fmt.Sprintf("network = $%d", argNum))
		args = append(args, updates.Network)
		argNum++
	}
	if updates.InitScript != nil {
		setClauses = append(setClauses, fmt.Sprintf("init_script = $%d", argNum))
		args = append(args, *updates.InitScript)
		argNum++
	}
	if updates.CleanupScript != nil {
		setClauses = append(setClauses, fmt.Sprintf("cleanup_script = $%d", argNum))
		args = append(args, *updates.CleanupScript)
		argNum++
	}
	if updates.Tunnels != nil {
		setClauses = append(setClauses, fmt.Sprintf("tunnels = $%d", argNum))
		args = append(args, updates.Tunnels)
		argNum++
	}
	if updates.Selector != nil {
		setClauses = append(setClauses, fmt.Sprintf("selector = $%d", argNum))
		args = append(args, updates.Selector)
		argNum++
	}
	if updates.Labels != nil {
		setClauses = append(setClauses, fmt.Sprintf("labels = $%d", argNum))
		args = append(args, updates.Labels)
		argNum++
	}
	if updates.Annotations != nil {
		setClauses = append(setClauses, fmt.Sprintf("annotations = $%d", argNum))
		args = append(args, updates.Annotations)
		argNum++
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`UPDATE profiles SET %s WHERE id = $%d`,
		strings.Join(setClauses, ", "), argNum)
	args = append(args, profileID)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "profile", profileID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "profile", ID: profileID}
	}

	return nil
}

// DeleteProfile deletes a profile.
func (s *Store) DeleteProfile(ctx context.Context, profileID string) error {
	return deleteProfile(ctx, s.pool, profileID)
}

// DeleteProfile deletes a profile within a transaction.
func (t *Tx) DeleteProfile(ctx context.Context, profileID string) error {
	return deleteProfile(ctx, t.tx, profileID)
}

func deleteProfile(ctx context.Context, q querier, profileID string) error {
	query := `DELETE FROM profiles WHERE id = $1`
	result, err := q.Exec(ctx, query, profileID)
	if err != nil {
		return handlePgError(err, "profile", profileID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "profile", ID: profileID}
	}

	return nil
}

func scanProfile(row pgx.Row, identifier string) (*store.Profile, error) {
	var p store.Profile
	err := row.Scan(
		&p.ID, &p.Name, &p.Description, &p.ProviderConfigID, &p.TenantID, &p.Resources, &p.Network,
		&p.InitScript, &p.CleanupScript, &p.Tunnels, &p.Selector, &p.Labels, &p.Annotations,
		&p.IsBuiltin, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "profile", ID: identifier}
		}
		return nil, fmt.Errorf("scanning profile: %w", err)
	}
	return &p, nil
}

func scanProfileFromRows(rows pgx.Rows) (*store.Profile, error) {
	var p store.Profile
	err := rows.Scan(
		&p.ID, &p.Name, &p.Description, &p.ProviderConfigID, &p.TenantID, &p.Resources, &p.Network,
		&p.InitScript, &p.CleanupScript, &p.Tunnels, &p.Selector, &p.Labels, &p.Annotations,
		&p.IsBuiltin, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
