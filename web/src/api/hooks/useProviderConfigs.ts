import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminClient } from '../admin'
import type {
  ProviderConfig,
  ProviderConfigList,
  CreateProviderConfigRequest,
  UpdateProviderConfigRequest,
} from '@/types/admin'

// Query keys
export const providerConfigKeys = {
  all: ['provider-configs'] as const,
  lists: () => [...providerConfigKeys.all, 'list'] as const,
  list: (params: { provider?: string; limit?: number }) =>
    [...providerConfigKeys.lists(), params] as const,
  details: () => [...providerConfigKeys.all, 'detail'] as const,
  detail: (id: string) => [...providerConfigKeys.details(), id] as const,
}

// List provider configs
export function useProviderConfigs(params: { provider?: string; limit?: number } = {}) {
  return useQuery({
    queryKey: providerConfigKeys.list(params),
    queryFn: async () => {
      const { data } = await adminClient.get<ProviderConfigList>('/provider-configs', { params })
      return data
    },
  })
}

// Get single provider config
export function useProviderConfig(configId: string) {
  return useQuery({
    queryKey: providerConfigKeys.detail(configId),
    queryFn: async () => {
      const { data } = await adminClient.get<ProviderConfig>(`/provider-configs/${configId}`)
      return data
    },
    enabled: !!configId,
  })
}

// Create provider config
export function useCreateProviderConfig() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (request: CreateProviderConfigRequest) => {
      const { data } = await adminClient.post<ProviderConfig>('/provider-configs', request)
      return data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: providerConfigKeys.lists() })
    },
  })
}

// Update provider config
export function useUpdateProviderConfig() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({
      configId,
      request,
    }: {
      configId: string
      request: UpdateProviderConfigRequest
    }) => {
      const { data } = await adminClient.put<ProviderConfig>(
        `/provider-configs/${configId}`,
        request
      )
      return data
    },
    onSuccess: (_data, { configId }) => {
      queryClient.invalidateQueries({ queryKey: providerConfigKeys.detail(configId) })
      queryClient.invalidateQueries({ queryKey: providerConfigKeys.lists() })
    },
  })
}

// Delete provider config
export function useDeleteProviderConfig() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (configId: string) => {
      await adminClient.delete(`/provider-configs/${configId}`)
    },
    onSuccess: (_data, configId) => {
      queryClient.invalidateQueries({ queryKey: providerConfigKeys.detail(configId) })
      queryClient.invalidateQueries({ queryKey: providerConfigKeys.lists() })
    },
  })
}
