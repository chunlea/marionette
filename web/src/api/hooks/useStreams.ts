import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '../client'
import type { Stream, StreamList, StartStreamRequest } from '@/types/stream'

// Query keys for caching
export const streamKeys = {
  all: ['streams'] as const,
  lists: () => [...streamKeys.all, 'list'] as const,
  list: (sessionId: string) => [...streamKeys.lists(), sessionId] as const,
  details: () => [...streamKeys.all, 'detail'] as const,
  detail: (streamId: string) => [...streamKeys.details(), streamId] as const,
}

// Fetch a single stream by ID
export function useStream(streamId: string | undefined) {
  return useQuery({
    queryKey: streamKeys.detail(streamId ?? ''),
    queryFn: async () => {
      const { data } = await apiClient.get<Stream>(`/admin/api/v1/streams/${streamId}`)
      return data
    },
    enabled: !!streamId,
    refetchInterval: (query) => {
      // Auto-refresh while stream is starting
      const stream = query.state.data
      if (stream?.status === 'starting' || stream?.status === 'pending') {
        return 2000 // 2 seconds
      }
      return false
    },
  })
}

// Fetch all streams for a session
export function useSessionStreams(sessionId: string | undefined) {
  return useQuery({
    queryKey: streamKeys.list(sessionId ?? ''),
    queryFn: async () => {
      const { data } = await apiClient.get<StreamList>(
        `/admin/api/v1/sessions/${sessionId}/streams`
      )
      return data
    },
    enabled: !!sessionId,
  })
}

// Start a desktop stream for a session
export function useStartDesktopStream() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({
      sessionId,
      config,
    }: {
      sessionId: string
      config?: StartStreamRequest
    }) => {
      const { data } = await apiClient.post<Stream>(
        `/admin/api/v1/sessions/${sessionId}/streams/desktop`,
        config ?? {}
      )
      return data
    },
    onSuccess: (data, variables) => {
      // Invalidate session streams list
      queryClient.invalidateQueries({
        queryKey: streamKeys.list(variables.sessionId),
      })
      // Add the new stream to cache
      queryClient.setQueryData(streamKeys.detail(data.id), data)
    },
  })
}

// Stop a desktop stream
export function useStopDesktopStream() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (streamId: string) => {
      await apiClient.delete(`/admin/api/v1/streams/${streamId}`)
    },
    onSuccess: (_, streamId) => {
      // Invalidate stream detail
      queryClient.invalidateQueries({
        queryKey: streamKeys.detail(streamId),
      })
      // Invalidate all stream lists
      queryClient.invalidateQueries({
        queryKey: streamKeys.lists(),
      })
    },
  })
}

// Get the active desktop stream for a session (if any)
export function useActiveDesktopStream(sessionId: string | undefined) {
  const { data: streams, ...rest } = useSessionStreams(sessionId)

  const activeStream = streams?.items?.find(
    (s) => s.status === 'active' || s.status === 'starting'
  )

  return {
    ...rest,
    data: activeStream,
    streams: streams?.items ?? [],
  }
}
