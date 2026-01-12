import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminClient } from '../admin'
import type {
  RunnerToken,
  RunnerTokenList,
  CreateRunnerTokenRequest,
  CreateRunnerTokenResponse,
  RunnerTokensQueryParams,
} from '@/types/api'

// Query keys
export const runnerTokenKeys = {
  all: ['runner-tokens'] as const,
  lists: () => [...runnerTokenKeys.all, 'list'] as const,
  list: (params: RunnerTokensQueryParams) => [...runnerTokenKeys.lists(), params] as const,
  details: () => [...runnerTokenKeys.all, 'detail'] as const,
  detail: (id: string) => [...runnerTokenKeys.details(), id] as const,
}

// List runner tokens
export function useRunnerTokens(params: RunnerTokensQueryParams = {}) {
  return useQuery({
    queryKey: runnerTokenKeys.list(params),
    queryFn: async () => {
      const queryParams = new URLSearchParams()
      if (params.limit) queryParams.set('limit', params.limit.toString())
      if (params.cursor) queryParams.set('cursor', params.cursor)
      if (params.pool_name) queryParams.set('pool_name', params.pool_name)
      if (params.status?.length) queryParams.set('status', params.status.join(','))
      if (params.include_revoked) queryParams.set('include_revoked', 'true')

      const url = queryParams.toString() ? `/runner-tokens?${queryParams}` : '/runner-tokens'
      const { data } = await adminClient.get<RunnerTokenList>(url)
      return data
    },
  })
}

// Get single runner token
export function useRunnerToken(tokenId: string) {
  return useQuery({
    queryKey: runnerTokenKeys.detail(tokenId),
    queryFn: async () => {
      const { data } = await adminClient.get<RunnerToken>(`/runner-tokens/${tokenId}`)
      return data
    },
    enabled: !!tokenId,
  })
}

// Create runner token
export function useCreateRunnerToken() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (request: CreateRunnerTokenRequest) => {
      const { data } = await adminClient.post<CreateRunnerTokenResponse>('/runner-tokens', request)
      return data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: runnerTokenKeys.lists() })
    },
  })
}

// Revoke runner token
export function useRevokeRunnerToken() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({ tokenId, reason }: { tokenId: string; reason?: string }) => {
      await adminClient.delete(`/runner-tokens/${tokenId}`, {
        data: reason ? { reason } : undefined,
      })
    },
    onSuccess: (_data, { tokenId }) => {
      queryClient.invalidateQueries({ queryKey: runnerTokenKeys.detail(tokenId) })
      queryClient.invalidateQueries({ queryKey: runnerTokenKeys.lists() })
    },
  })
}

// Rotate runner token
export function useRotateRunnerToken() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (tokenId: string) => {
      const { data } = await adminClient.post<CreateRunnerTokenResponse>(
        `/runner-tokens/${tokenId}/rotate`
      )
      return data
    },
    onSuccess: (_data, tokenId) => {
      queryClient.invalidateQueries({ queryKey: runnerTokenKeys.detail(tokenId) })
      queryClient.invalidateQueries({ queryKey: runnerTokenKeys.lists() })
    },
  })
}
