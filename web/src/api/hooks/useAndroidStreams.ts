import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '../client'
import type {
  AndroidStream,
  AndroidStreamList,
  AndroidDeviceList,
  CreateAndroidStreamRequest,
  AndroidInputEvent,
  AndroidStreamsQueryParams,
} from '@/types/api'

// Query keys
export const androidStreamKeys = {
  all: ['androidStreams'] as const,
  lists: () => [...androidStreamKeys.all, 'list'] as const,
  list: (sessionId: string, params: AndroidStreamsQueryParams) =>
    [...androidStreamKeys.lists(), sessionId, params] as const,
  details: () => [...androidStreamKeys.all, 'detail'] as const,
  detail: (sessionId: string, streamId: string) =>
    [...androidStreamKeys.details(), sessionId, streamId] as const,
  devices: (sessionId: string) => [...androidStreamKeys.all, 'devices', sessionId] as const,
}

// List Android streams for a session
export function useAndroidStreams(sessionId: string, params: AndroidStreamsQueryParams = {}) {
  return useQuery({
    queryKey: androidStreamKeys.list(sessionId, params),
    queryFn: async () => {
      const { data } = await apiClient.get<AndroidStreamList>(
        `/sessions/${sessionId}/android/streams`,
        { params }
      )
      return data
    },
    enabled: !!sessionId,
  })
}

// Get single Android stream
export function useAndroidStream(sessionId: string, streamId: string) {
  return useQuery({
    queryKey: androidStreamKeys.detail(sessionId, streamId),
    queryFn: async () => {
      const { data } = await apiClient.get<AndroidStream>(
        `/sessions/${sessionId}/android/streams/${streamId}`
      )
      return data
    },
    enabled: !!sessionId && !!streamId,
    refetchInterval: (query) => {
      // Refetch frequently while stream is starting or active
      const state = query.state.data?.state
      if (state === 'starting' || state === 'active') {
        return 5000
      }
      return false
    },
  })
}

// List available Android devices
export function useAndroidDevices(sessionId: string) {
  return useQuery({
    queryKey: androidStreamKeys.devices(sessionId),
    queryFn: async () => {
      const { data } = await apiClient.get<AndroidDeviceList>(
        `/sessions/${sessionId}/android/devices`
      )
      return data
    },
    enabled: !!sessionId,
  })
}

// Start Android stream
export function useStartAndroidStream(sessionId: string) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (request: CreateAndroidStreamRequest) => {
      const { data } = await apiClient.post<AndroidStream>(
        `/sessions/${sessionId}/android/streams`,
        request
      )
      return data
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: androidStreamKeys.lists() })
    },
  })
}

// Stop Android stream
export function useStopAndroidStream(sessionId: string) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (streamId: string) => {
      await apiClient.delete(`/sessions/${sessionId}/android/streams/${streamId}`)
    },
    onSuccess: (_data, streamId) => {
      queryClient.invalidateQueries({
        queryKey: androidStreamKeys.detail(sessionId, streamId),
      })
      queryClient.invalidateQueries({ queryKey: androidStreamKeys.lists() })
    },
  })
}

// Send input to Android device
export function useSendAndroidInput(sessionId: string, streamId: string) {
  return useMutation({
    mutationFn: async (input: AndroidInputEvent) => {
      await apiClient.post(`/sessions/${sessionId}/android/streams/${streamId}/input`, input)
    },
  })
}
