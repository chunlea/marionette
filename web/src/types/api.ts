// API Types - Generated from OpenAPI spec
// These types match the backend API schema

// Common types
export type Labels = Record<string, string>

// Session types
export type SessionStatus = 'pending' | 'active' | 'suspended' | 'resuming' | 'terminated'
export type LifecycleMode = 'on_demand' | 'always_on' | 'scheduled'

export interface Session {
  id: string
  name?: string
  status: SessionStatus
  agent: string
  runner_id?: string
  workspace_id: string
  lifecycle_mode: LifecycleMode
  labels: Labels
  created_at: string
  updated_at: string
}

export interface CreateSessionRequest {
  name?: string
  agent: string
  agent_config_id?: string
  agent_api_key?: string
  lifecycle_mode?: LifecycleMode
  idle_timeout_seconds?: number
  labels?: Labels
}

export interface SessionList {
  items: Session[]
  next_cursor?: string
}

// Task types
export type TaskStatus = 'pending' | 'running' | 'completed' | 'failed' | 'canceled'

export interface Task {
  id: string
  session_id: string
  prompt: string
  status: TaskStatus
  max_retries: number
  retry_count: number
  timeout_seconds: number
  labels: Labels
  created_at: string
  updated_at: string
}

export interface CreateTaskRequest {
  session_id: string
  prompt: string
  continue_from?: string
  max_retries?: number
  timeout_seconds?: number
  labels?: Labels
}

export interface TaskList {
  items: Task[]
  next_cursor?: string
}

// Log types
export type LogStream = 'stdout' | 'stderr' | 'system'
export type LogLevel = 'debug' | 'info' | 'warn' | 'error'

export interface Log {
  id: string
  session_id: string
  task_id: string
  run_id: string
  stream: LogStream
  level: LogLevel
  content: string
  sequence: number
  created_at: string
}

export interface LogList {
  items: Log[]
  next_cursor?: string
}

// Runner types
export type RunnerStatus = 'offline' | 'idle' | 'busy' | 'paused'
export type SandboxMode = 'runner-is-sandbox' | 'runner-creates-sandbox' | 'none'

export interface Runner {
  id: string
  name: string
  hostname: string
  status: RunnerStatus
  pool_name?: string
  sandbox_mode: SandboxMode
  labels: Labels
  last_seen_at?: string
  created_at: string
}

export interface RunnerList {
  items: Runner[]
  next_cursor?: string
}

export interface SpawnRunnerRequest {
  name?: string
  provider_config_id?: string
  profile_id?: string
  labels?: Labels
}

// Permission types
export type RiskLevel = 'low' | 'medium' | 'high' | 'critical'
export type PermissionStatus = 'pending' | 'approved' | 'denied' | 'canceled'

export interface PermissionRequest {
  id: string
  session_id: string
  task_id: string
  run_id: string
  tool: string
  action: string
  context?: string
  risk_level: RiskLevel
  status: PermissionStatus
  responded_by?: string
  response_reason?: string
  responded_at?: string
  created_at: string
}

export interface PermissionList {
  items: PermissionRequest[]
  next_cursor?: string
}

export interface PermissionResponse {
  reason?: string
}

// API Key types
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

// Response from creating an API key - includes the raw token shown once
export interface CreateAPIKeyResponse {
  key: APIKey
  raw_token: string
}

export interface CreateAPIKeyRequest {
  name: string
  scopes?: string[]
  labels?: Labels
}

export interface APIKeyList {
  items: APIKey[]
  next_cursor?: string
}

// Agent Config types
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

// Provider Config types
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

// Error types
export interface APIError {
  code: string
  message: string
}

// Query parameters
export interface PaginationParams {
  limit?: number
  cursor?: string
}

export interface SessionsQueryParams extends PaginationParams {
  status?: SessionStatus[]
  agent?: string
}

export interface TasksQueryParams extends PaginationParams {
  session_id?: string
  status?: TaskStatus[]
}

export interface RunnersQueryParams extends PaginationParams {
  status?: RunnerStatus[]
  pool_name?: string
}

export interface PermissionsQueryParams extends PaginationParams {
  session_id?: string
  task_id?: string
  status?: PermissionStatus[]
  risk_level?: RiskLevel[]
}
