import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminClient } from '../admin'
import type {
  APIKey,
  APIKeyWithSecret,
  APIKeyList,
  CreateAPIKeyRequest,
  PaginationParams,
} from '@/types/api'

// Query keys
export const apiKeyKeys = {
  all: ['api-keys'] as const,
  lists: () => [...apiKeyKeys.all, 'list'] as const,
  list: (params: PaginationParams) => [...apiKeyKeys.lists(), params] as const,
  details: () => [...apiKeyKeys.all, 'detail'] as const,
  detail: (id: string) => [...apiKeyKeys.details(), id] as const,
}

// List API keys
export function useApiKeys(params: PaginationParams = {}) {
  return useQuery({
    queryKey: apiKeyKeys.list(params),
    queryFn: async () => {
      const { data } = await adminClient.get<APIKeyList>('/keys', { params })
      return data
    },
  })
}

// Get single API key
export function useApiKey(keyId: string) {
  return useQuery({
    queryKey: apiKeyKeys.detail(keyId),
    queryFn: async () => {
      const { data } = await adminClient.get<APIKey>(`/keys/${keyId}`)
      return data
    },
    enabled: !!keyId,
  })
}

// Create API key
export function useCreateApiKey() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (request: CreateAPIKeyRequest) => {
      const { data } = await adminClient.post<APIKeyWithSecret>('/keys', request)
      return data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: apiKeyKeys.lists() })
    },
  })
}

// Revoke API key
export function useRevokeApiKey() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({ keyId, reason }: { keyId: string; reason?: string }) => {
      await adminClient.delete(`/keys/${keyId}`, {
        data: reason ? { reason } : undefined,
      })
    },
    onSuccess: (_data, { keyId }) => {
      queryClient.invalidateQueries({ queryKey: apiKeyKeys.detail(keyId) })
      queryClient.invalidateQueries({ queryKey: apiKeyKeys.lists() })
    },
  })
}
