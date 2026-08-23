import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminClient } from '../admin'
import type {
  AgentConfig,
  AgentConfigList,
  CreateAgentConfigRequest,
  UpdateAgentConfigRequest,
} from '@/types/admin'

// Query keys
export const agentConfigKeys = {
  all: ['agent-configs'] as const,
  lists: () => [...agentConfigKeys.all, 'list'] as const,
  list: (params: { agent?: string; limit?: number }) => [...agentConfigKeys.lists(), params] as const,
  details: () => [...agentConfigKeys.all, 'detail'] as const,
  detail: (id: string) => [...agentConfigKeys.details(), id] as const,
}

// List agent configs
export function useAgentConfigs(params: { agent?: string; limit?: number } = {}) {
  return useQuery({
    queryKey: agentConfigKeys.list(params),
    queryFn: async () => {
      const { data } = await adminClient.get<AgentConfigList>('/agent-configs', { params })
      return data
    },
  })
}

// Get single agent config
export function useAgentConfig(configId: string) {
  return useQuery({
    queryKey: agentConfigKeys.detail(configId),
    queryFn: async () => {
      const { data } = await adminClient.get<AgentConfig>(`/agent-configs/${configId}`)
      return data
    },
    enabled: !!configId,
  })
}

// Create agent config
export function useCreateAgentConfig() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (request: CreateAgentConfigRequest) => {
      const { data } = await adminClient.post<AgentConfig>('/agent-configs', request)
      return data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: agentConfigKeys.lists() })
    },
  })
}

// Update agent config
export function useUpdateAgentConfig() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({
      configId,
      request,
    }: {
      configId: string
      request: UpdateAgentConfigRequest
    }) => {
      const { data } = await adminClient.put<AgentConfig>(`/agent-configs/${configId}`, request)
      return data
    },
    onSuccess: (_data, { configId }) => {
      queryClient.invalidateQueries({ queryKey: agentConfigKeys.detail(configId) })
      queryClient.invalidateQueries({ queryKey: agentConfigKeys.lists() })
    },
  })
}

// Delete agent config
export function useDeleteAgentConfig() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (configId: string) => {
      await adminClient.delete(`/agent-configs/${configId}`)
    },
    onSuccess: (_data, configId) => {
      queryClient.invalidateQueries({ queryKey: agentConfigKeys.detail(configId) })
      queryClient.invalidateQueries({ queryKey: agentConfigKeys.lists() })
    },
  })
}
