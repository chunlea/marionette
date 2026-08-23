/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_URL: string
  readonly VITE_ADMIN_URL: string
  readonly VITE_WS_URL: string
  /** "true" enables the frozen streaming UI; see src/lib/features.ts. */
  readonly VITE_ENABLE_STREAMING: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
