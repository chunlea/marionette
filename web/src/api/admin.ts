import axios, { AxiosError, AxiosInstance } from 'axios'
import type { APIError } from '@/types/api'

// Storage keys
const ADMIN_CREDENTIALS_KEY = 'marionette_admin_credentials'

// Create admin API client instance
export const adminClient: AxiosInstance = axios.create({
  baseURL: import.meta.env.VITE_ADMIN_URL || '/admin/api/v1',
  headers: {
    'Content-Type': 'application/json',
  },
  // Repeat array parameters as ?status=a&status=b. axios would otherwise send
  // status[]=a, which Go's r.URL.Query()["status"] does not see at all — every
  // filter was silently ignored, and the sidebar's pending-permission badge
  // counted every permission ever raised.
  paramsSerializer: { indexes: null },
})

// Request interceptor: Add Basic Auth
adminClient.interceptors.request.use((config) => {
  const credentials = getAdminCredentials()
  if (credentials) {
    config.headers.Authorization = `Basic ${credentials}`
  }
  return config
})

// Response interceptor: Handle errors
adminClient.interceptors.response.use(
  (response) => response,
  (error: AxiosError<APIError>) => {
    if (error.response?.status === 401) {
      // Clear stored credentials on unauthorized
      clearAdminCredentials()
      // Redirect to admin login if not already there
      if (window.location.pathname !== '/admin/login') {
        window.location.href = '/admin/login'
      }
    }
    return Promise.reject(error)
  }
)

// Credential management functions
export function getAdminCredentials(): string | null {
  return localStorage.getItem(ADMIN_CREDENTIALS_KEY)
}

export function setAdminCredentials(username: string, password: string): void {
  const encoded = btoa(`${username}:${password}`)
  localStorage.setItem(ADMIN_CREDENTIALS_KEY, encoded)
}

export function clearAdminCredentials(): void {
  localStorage.removeItem(ADMIN_CREDENTIALS_KEY)
}

export function isAdminAuthenticated(): boolean {
  return !!getAdminCredentials()
}

// Validate admin credentials by making a test request
export async function validateAdminCredentials(
  username: string,
  password: string
): Promise<boolean> {
  try {
    const encoded = btoa(`${username}:${password}`)
    await axios.get(
      `${import.meta.env.VITE_ADMIN_URL || '/admin/api/v1'}/keys`,
      {
        headers: {
          Authorization: `Basic ${encoded}`,
        },
        params: { limit: 1 },
      }
    )
    return true
  } catch (error) {
    if (axios.isAxiosError(error) && error.response?.status === 401) {
      return false
    }
    // Re-throw other errors (network issues, etc.)
    throw error
  }
}
