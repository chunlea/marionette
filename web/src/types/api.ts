// API types — derived from the OpenAPI spec.
//
// The header on this file used to be a lie: nothing generated anything, the
// shapes were hand-written, and they had drifted from the server. Now
// `api.gen.ts` really is generated (`pnpm generate:api`, checked for drift in
// CI) and this module only gives its types short names.
//
// Add nothing here that the server does not describe. Admin API types are
// hand-written and live in `./admin`, because the admin API has no generated
// spec yet.

import type { components, operations } from './api.gen'

type Schemas = components['schemas']

// Common
export type Labels = Record<string, string>
export type APIError = Schemas['ErrorResponse']

// Sessions
export type Session = Schemas['Session']
export type SessionStatus = Session['status']
export type LifecycleMode = Session['lifecycle_mode']
export type NetworkPolicy = Session['network_policy']
export type SessionList = Schemas['SessionList']
export type CreateSessionRequest = Schemas['CreateSessionRequest']

// Tasks
export type Task = Schemas['Task']
export type TaskStatus = Task['status']
export type TaskList = Schemas['TaskList']
export type CreateTaskRequest = Schemas['CreateTaskRequest']

// Logs
export type Log = Schemas['Log']
export type LogStream = Log['stream']
export type LogLevel = Log['level']
export type LogList = Schemas['LogList']

// Runners
export type Runner = Schemas['Runner']
export type RunnerStatus = Runner['status']
export type SandboxMode = Runner['sandbox_mode']
export type RunnerList = Schemas['RunnerList']

// Permissions
export type PermissionRequest = Schemas['PermissionRequest']
export type PermissionStatus = PermissionRequest['status']
export type RiskLevel = PermissionRequest['risk_level']
export type PermissionList = Schemas['PermissionRequestList']
export type PermissionResponse = Schemas['ApproveRequest']

// Workspaces
export type Workspace = Schemas['Workspace']
export type WorkspaceList = Schemas['WorkspaceList']
export type CreateWorkspaceRequest = Schemas['CreateWorkspaceRequest']
export type UpdateWorkspaceRequest = Schemas['UpdateWorkspaceRequest']

// Scheduled tasks
export type ScheduledTask = Schemas['ScheduledTask']
export type ScheduledTaskStatus = ScheduledTask['status']
export type ScheduledTaskList = Schemas['ScheduledTaskList']
export type CreateScheduledTaskRequest = Schemas['CreateScheduledTaskRequest']
export type UpdateScheduledTaskRequest = Schemas['UpdateScheduledTaskRequest']

// Tunnels
export type Tunnel = Schemas['Tunnel']
export type TunnelList = Schemas['TunnelList']
export type CreateTunnelRequest = Schemas['CreateTunnelRequest']

// Query parameters, taken from the operations so a renamed or dropped filter
// is a compile error rather than a request the server silently ignores.
export type PaginationParams = { limit?: number; cursor?: string }
export type SessionsQueryParams = operations['getSessions']['parameters']['query']
export type TasksQueryParams = operations['getTasks']['parameters']['query']
export type RunnersQueryParams = operations['getRunners']['parameters']['query']
export type PermissionsQueryParams = operations['getPermissions']['parameters']['query']
export type ScheduledTasksQueryParams = operations['getScheduledTasks']['parameters']['query']
