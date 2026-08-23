// Admin API types — hand-written, and knowingly so.
//
// Unlike the public API (see `./api`), the admin API still serializes database
// models straight to JSON and has no generated OpenAPI document, so there is
// nothing to derive these from. They are therefore the one remaining place in
// the frontend where a server-side change can go unnoticed until runtime.
//
// When the admin API gets DTOs and a generated spec, delete this file and
// derive these the same way the public types are derived.

import type { Labels } from './api'

// API keys
export interface APIKey {
  id: string
  name: string
  key_prefix: string
  scopes: string[]
  labels: Labels
  created_at: string
  last_used_at?: string
  revoked_at?: string
}

export interface CreateAPIKeyRequest {
  name: string
  scopes?: string[]
  labels?: Labels
}

/** The raw token is shown once, at creation, and never again. */
export interface CreateAPIKeyResponse {
  key: APIKey
  raw_token: string
}

export interface APIKeyList {
  items: APIKey[]
  next_cursor?: string
}

// Agent configs
export interface AgentConfig {
  id: string
  name: string
  agent: string
  model?: string
  base_url?: string
  is_default: boolean
  labels: Labels
  created_at: string
  updated_at: string
}

export interface CreateAgentConfigRequest {
  name: string
  agent: string
  api_key: string
  model?: string
  base_url?: string
  is_default?: boolean
  labels?: Labels
}

export interface UpdateAgentConfigRequest {
  name?: string
  api_key?: string
  model?: string
  base_url?: string
  is_default?: boolean
  labels?: Labels
}

export interface AgentConfigList {
  items: AgentConfig[]
  next_cursor?: string
}

// Provider configs
export interface ProviderConfig {
  id: string
  name: string
  provider: string
  config: Record<string, unknown>
  suspend_config: Record<string, unknown>
  is_default: boolean
  labels: Labels
  created_at: string
  updated_at: string
}

export interface CreateProviderConfigRequest {
  name: string
  provider: string
  config?: Record<string, unknown>
  suspend_config?: Record<string, unknown>
  is_default?: boolean
  labels?: Labels
}

export interface UpdateProviderConfigRequest {
  name?: string
  config?: Record<string, unknown>
  suspend_config?: Record<string, unknown>
  is_default?: boolean
  labels?: Labels
}

export interface ProviderConfigList {
  items: ProviderConfig[]
  next_cursor?: string
}

// Runner tokens
export type RunnerTokenStatus = 'active' | 'rotating' | 'revoked' | 'expired'

export interface RunnerToken {
  id: string
  token_prefix: string
  runner_id?: string
  pool_name: string
  status: RunnerTokenStatus
  rotation_deadline?: string
  labels: Labels
  created_at: string
  created_by?: string
  last_used_at?: string
  expires_at?: string
  revoked_at?: string
  revoke_reason?: string
}

export interface RunnerTokenList {
  items: RunnerToken[]
  next_cursor?: string
  total_count?: number
}

export interface CreateRunnerTokenRequest {
  pool_name: string
  labels?: Labels
  expires_at?: string
}

export interface CreateRunnerTokenResponse {
  token: RunnerToken
  raw_token: string
}

export interface RunnerTokensQueryParams {
  limit?: number
  cursor?: string
  pool_name?: string
  status?: RunnerTokenStatus[]
  include_revoked?: boolean
}

// Runners (admin view)
export interface SpawnRunnerRequest {
  name?: string
  provider_config_id?: string
  profile_id?: string
  labels?: Labels
}
