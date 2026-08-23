import axios, { AxiosError, AxiosInstance } from 'axios'
import type { APIError } from '@/types/api'

// Storage keys
const API_KEY_STORAGE_KEY = 'marionette_api_key'

// Create public API client instance
export const apiClient: AxiosInstance = axios.create({
  baseURL: import.meta.env.VITE_API_URL || '/api/v1',
  headers: {
    'Content-Type': 'application/json',
  },
  // Repeat array parameters as ?status=a&status=b. axios would otherwise send
  // status[]=a, which Go's r.URL.Query()["status"] does not see at all — every
  // filter was silently ignored, and the sidebar's pending-permission badge
  // counted every permission ever raised.
  paramsSerializer: { indexes: null },
})

// Request interceptor: Add API key to requests
apiClient.interceptors.request.use((config) => {
  const apiKey = getApiKey()
  if (apiKey) {
    config.headers.Authorization = `Bearer ${apiKey}`
  }
  return config
})

// Response interceptor: Handle errors
apiClient.interceptors.response.use(
  (response) => response,
  (error: AxiosError<APIError>) => {
    if (error.response?.status === 401) {
      // Clear stored API key on unauthorized
      clearApiKey()
      // Redirect to login if not already there
      if (window.location.pathname !== '/login') {
        window.location.href = '/login'
      }
    }
    return Promise.reject(error)
  }
)

// API key management functions
export function getApiKey(): string | null {
  return localStorage.getItem(API_KEY_STORAGE_KEY)
}

export function setApiKey(key: string): void {
  localStorage.setItem(API_KEY_STORAGE_KEY, key)
}

export function clearApiKey(): void {
  localStorage.removeItem(API_KEY_STORAGE_KEY)
}

export function isAuthenticated(): boolean {
  return !!getApiKey()
}

// Helper to extract error message from API response
export function getErrorMessage(error: unknown): string {
  if (axios.isAxiosError(error)) {
    const apiError = error.response?.data as APIError | undefined
    if (apiError?.message) {
      return apiError.message
    }
    if (error.message) {
      return error.message
    }
  }
  if (error instanceof Error) {
    return error.message
  }
  return 'An unknown error occurred'
}
