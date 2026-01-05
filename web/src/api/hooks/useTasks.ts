import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '../client'
import type {
  Task,
  TaskList,
  TaskRunList,
  CreateTaskRequest,
  TasksQueryParams,
  LogList,
} from '@/types/api'

// Query keys
export const taskKeys = {
  all: ['tasks'] as const,
  lists: () => [...taskKeys.all, 'list'] as const,
  list: (params: TasksQueryParams) => [...taskKeys.lists(), params] as const,
  details: () => [...taskKeys.all, 'detail'] as const,
  detail: (id: string) => [...taskKeys.details(), id] as const,
  runs: (id: string) => [...taskKeys.all, 'runs', id] as const,
  logs: (id: string) => [...taskKeys.all, 'logs', id] as const,
}

// List tasks
export function useTasks(params: TasksQueryParams = {}) {
  return useQuery({
    queryKey: taskKeys.list(params),
    queryFn: async () => {
      const { data } = await apiClient.get<TaskList>('/tasks', { params })
      return data
    },
  })
}

// Get single task
export function useTask(taskId: string) {
  return useQuery({
    queryKey: taskKeys.detail(taskId),
    queryFn: async () => {
      const { data } = await apiClient.get<Task>(`/tasks/${taskId}`)
      return data
    },
    enabled: !!taskId,
  })
}

// Get task runs
export function useTaskRuns(taskId: string) {
  return useQuery({
    queryKey: taskKeys.runs(taskId),
    queryFn: async () => {
      const { data } = await apiClient.get<TaskRunList>(`/tasks/${taskId}/runs`)
      return data
    },
    enabled: !!taskId,
  })
}

// Get task logs
export function useTaskLogs(taskId: string, params: { limit?: number; cursor?: string } = {}) {
  return useQuery({
    queryKey: taskKeys.logs(taskId),
    queryFn: async () => {
      const { data } = await apiClient.get<LogList>(`/tasks/${taskId}/logs`, { params })
      return data
    },
    enabled: !!taskId,
  })
}

// Create task
export function useCreateTask() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (request: CreateTaskRequest) => {
      const { data } = await apiClient.post<Task>('/tasks', request)
      return data
    },
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: taskKeys.lists() })
      // Also invalidate the session since its status might change
      queryClient.invalidateQueries({ queryKey: ['sessions', 'detail', data.session_id] })
    },
  })
}

// Cancel task
export function useCancelTask() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (taskId: string) => {
      await apiClient.post(`/tasks/${taskId}/cancel`)
    },
    onSuccess: (_data, taskId) => {
      queryClient.invalidateQueries({ queryKey: taskKeys.detail(taskId) })
      queryClient.invalidateQueries({ queryKey: taskKeys.lists() })
    },
  })
}

// Retry task
export function useRetryTask() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (taskId: string) => {
      const { data } = await apiClient.post<Task>(`/tasks/${taskId}/retry`)
      return data
    },
    onSuccess: (_data, taskId) => {
      queryClient.invalidateQueries({ queryKey: taskKeys.detail(taskId) })
      queryClient.invalidateQueries({ queryKey: taskKeys.lists() })
    },
  })
}
