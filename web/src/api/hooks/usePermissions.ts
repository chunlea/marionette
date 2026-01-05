import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '../client'
import type {
  PermissionRequest,
  PermissionList,
  PermissionsQueryParams,
  PermissionResponse,
} from '@/types/api'

// Query keys
export const permissionKeys = {
  all: ['permissions'] as const,
  lists: () => [...permissionKeys.all, 'list'] as const,
  list: (params: PermissionsQueryParams) => [...permissionKeys.lists(), params] as const,
  details: () => [...permissionKeys.all, 'detail'] as const,
  detail: (id: string) => [...permissionKeys.details(), id] as const,
  pending: () => [...permissionKeys.all, 'pending'] as const,
}

// List permissions
export function usePermissions(params: PermissionsQueryParams = {}) {
  return useQuery({
    queryKey: permissionKeys.list(params),
    queryFn: async () => {
      const { data } = await apiClient.get<PermissionList>('/permissions', { params })
      return data
    },
  })
}

// List pending permissions (convenience hook)
export function usePendingPermissions() {
  return useQuery({
    queryKey: permissionKeys.pending(),
    queryFn: async () => {
      const { data } = await apiClient.get<PermissionList>('/permissions', {
        params: { status: ['pending'] },
      })
      return data
    },
    // Refresh frequently for pending permissions
    refetchInterval: 5000,
  })
}

// Get single permission
export function usePermission(permissionId: string) {
  return useQuery({
    queryKey: permissionKeys.detail(permissionId),
    queryFn: async () => {
      const { data } = await apiClient.get<PermissionRequest>(`/permissions/${permissionId}`)
      return data
    },
    enabled: !!permissionId,
  })
}

// Approve permission
export function useApprovePermission() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({
      permissionId,
      response,
    }: {
      permissionId: string
      response?: PermissionResponse
    }) => {
      await apiClient.post(`/permissions/${permissionId}/approve`, response)
    },
    onSuccess: (_data, { permissionId }) => {
      queryClient.invalidateQueries({ queryKey: permissionKeys.detail(permissionId) })
      queryClient.invalidateQueries({ queryKey: permissionKeys.lists() })
      queryClient.invalidateQueries({ queryKey: permissionKeys.pending() })
    },
  })
}

// Deny permission
export function useDenyPermission() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({
      permissionId,
      response,
    }: {
      permissionId: string
      response?: PermissionResponse
    }) => {
      await apiClient.post(`/permissions/${permissionId}/deny`, response)
    },
    onSuccess: (_data, { permissionId }) => {
      queryClient.invalidateQueries({ queryKey: permissionKeys.detail(permissionId) })
      queryClient.invalidateQueries({ queryKey: permissionKeys.lists() })
      queryClient.invalidateQueries({ queryKey: permissionKeys.pending() })
    },
  })
}
