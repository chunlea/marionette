import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '../client'
import type {
  Session,
  SessionList,
  CreateSessionRequest,
  SessionsQueryParams,
} from '@/types/api'

// Query keys
export const sessionKeys = {
  all: ['sessions'] as const,
  lists: () => [...sessionKeys.all, 'list'] as const,
  list: (params: SessionsQueryParams) => [...sessionKeys.lists(), params] as const,
  details: () => [...sessionKeys.all, 'detail'] as const,
  detail: (id: string) => [...sessionKeys.details(), id] as const,
}

// List sessions
export function useSessions(params: SessionsQueryParams = {}) {
  return useQuery({
    queryKey: sessionKeys.list(params),
    queryFn: async () => {
      const { data } = await apiClient.get<SessionList>('/sessions', { params })
      return data
    },
  })
}

// Get single session
export function useSession(sessionId: string) {
  return useQuery({
    queryKey: sessionKeys.detail(sessionId),
    queryFn: async () => {
      const { data } = await apiClient.get<Session>(`/sessions/${sessionId}`)
      return data
    },
    enabled: !!sessionId,
  })
}

// Create session
export function useCreateSession() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (request: CreateSessionRequest) => {
      const { data } = await apiClient.post<Session>('/sessions', request)
      return data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: sessionKeys.lists() })
    },
  })
}

// Suspend session
export function useSuspendSession() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (sessionId: string) => {
      await apiClient.post(`/sessions/${sessionId}/suspend`)
    },
    onSuccess: (_data, sessionId) => {
      queryClient.invalidateQueries({ queryKey: sessionKeys.detail(sessionId) })
      queryClient.invalidateQueries({ queryKey: sessionKeys.lists() })
    },
  })
}

// Resume session
export function useResumeSession() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (sessionId: string) => {
      await apiClient.post(`/sessions/${sessionId}/resume`)
    },
    onSuccess: (_data, sessionId) => {
      queryClient.invalidateQueries({ queryKey: sessionKeys.detail(sessionId) })
      queryClient.invalidateQueries({ queryKey: sessionKeys.lists() })
    },
  })
}

// Terminate session
export function useTerminateSession() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (sessionId: string) => {
      await apiClient.delete(`/sessions/${sessionId}`)
    },
    onSuccess: (_data, sessionId) => {
      queryClient.invalidateQueries({ queryKey: sessionKeys.detail(sessionId) })
      queryClient.invalidateQueries({ queryKey: sessionKeys.lists() })
    },
  })
}
