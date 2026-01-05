import { useQuery } from '@tanstack/react-query'
import { apiClient } from '../client'
import type { Runner, RunnerList, RunnersQueryParams } from '@/types/api'

// Query keys
export const runnerKeys = {
  all: ['runners'] as const,
  lists: () => [...runnerKeys.all, 'list'] as const,
  list: (params: RunnersQueryParams) => [...runnerKeys.lists(), params] as const,
  details: () => [...runnerKeys.all, 'detail'] as const,
  detail: (id: string) => [...runnerKeys.details(), id] as const,
}

// List runners
export function useRunners(params: RunnersQueryParams = {}) {
  return useQuery({
    queryKey: runnerKeys.list(params),
    queryFn: async () => {
      const { data } = await apiClient.get<RunnerList>('/runners', { params })
      return data
    },
  })
}

// Get single runner
export function useRunner(runnerId: string) {
  return useQuery({
    queryKey: runnerKeys.detail(runnerId),
    queryFn: async () => {
      const { data } = await apiClient.get<Runner>(`/runners/${runnerId}`)
      return data
    },
    enabled: !!runnerId,
  })
}
