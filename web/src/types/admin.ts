// Admin API types — derived from the admin OpenAPI spec.
//
// These used to be hand-written, because the admin API had no generated
// document. It has one now (`pnpm generate:api`, checked for drift in CI), so
// these are only short names for what the server actually sends.
//
// Add nothing here the server does not describe.

import type { components, operations } from './admin.gen'

type Schemas = components['schemas']

// API keys
export type APIKey = Schemas['APIKey']
export type CreateAPIKeyRequest = Schemas['CreateAPIKeyRequest']
/** The raw token is shown once, at creation, and never again. */
export type CreateAPIKeyResponse = Schemas['CreatedAPIKey']
export type APIKeyList = Schemas['APIKeyList']

// Agent configs
export type AgentConfig = Schemas['AgentConfig']
export type CreateAgentConfigRequest = Schemas['CreateAgentConfigRequest']
export type UpdateAgentConfigRequest = Schemas['UpdateAgentConfigRequest']
export type AgentConfigList = Schemas['AgentConfigList']

// Provider configs
export type ProviderConfig = Schemas['ProviderConfig']
export type CreateProviderConfigRequest = Schemas['CreateProviderConfigRequest']
export type UpdateProviderConfigRequest = Schemas['UpdateProviderConfigRequest']
export type ProviderConfigList = Schemas['ProviderConfigList']

// Profiles
export type Profile = Schemas['Profile']
export type CreateProfileRequest = Schemas['CreateProfileRequest']
export type UpdateProfileRequest = Schemas['UpdateProfileRequest']
export type ProfileList = Schemas['ProfileList']

// Runner tokens
export type RunnerToken = Schemas['RunnerToken']
export type RunnerTokenStatus = RunnerToken['status']
export type CreateRunnerTokenRequest = Schemas['CreateRunnerTokenRequest']
export type CreateRunnerTokenResponse = Schemas['CreatedRunnerToken']
export type RunnerTokenList = Schemas['RunnerTokenList']
export type RunnerTokensQueryParams = operations['getRunnerTokens']['parameters']['query']

// Runners, as the operator sees them: unlike the public view, this one names
// the provider behind each runner.
export type AdminRunner = Schemas['Runner']
export type AdminRunnerList = Schemas['RunnerList']
export type SpawnRunnerRequest = Schemas['SpawnRunnerRequest']

// Webhooks
export type Webhook = Schemas['Webhook']
export type WebhookList = Schemas['WebhookList']
export type CreateWebhookRequest = Schemas['CreateWebhookRequest']
export type CreateWebhookResponse = Schemas['CreatedWebhook']
export type UpdateWebhookRequest = Schemas['UpdateWebhookRequest']
export type RotatedWebhookSecret = Schemas['RotatedWebhookSecret']
export type WebhookEvent = Schemas['WebhookEvent']
export type WebhookEventList = Schemas['WebhookEventList']

// Audit trail
export type ActionLog = Schemas['ActionLog']
export type ActionLogList = Schemas['ActionLogList']

// Service
export type AdminHealth = Schemas['Health']
export type ServiceStatus = Schemas['ServiceStatus']
export type Status = Schemas['Status']
