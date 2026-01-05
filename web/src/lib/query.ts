import { QueryClient } from '@tanstack/react-query'

// Create a QueryClient instance with default options
export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        // Data is considered fresh for 5 seconds
        staleTime: 5000,
        // Retry failed queries once
        retry: 1,
        // Don't refetch on window focus (can be distracting)
        refetchOnWindowFocus: false,
        // Don't refetch on reconnect automatically
        refetchOnReconnect: 'always',
      },
      mutations: {
        // Retry failed mutations once
        retry: 1,
      },
    },
  })
}

// Singleton instance for the app
export const queryClient = createQueryClient()
