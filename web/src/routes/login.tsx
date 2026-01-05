import { useState } from 'react'
import { createFileRoute, useNavigate, Link } from '@tanstack/react-router'
import { setApiKey } from '@/api/client'
import { Button } from '@/components/Button'
import { Input } from '@/components/Input'

export const Route = createFileRoute('/login')({
  component: LoginPage,
})

function LoginPage() {
  const navigate = useNavigate()
  const [apiKeyInput, setApiKeyInput] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')

    if (!apiKeyInput.trim()) {
      setError('API key is required')
      return
    }

    // Validate API key format
    if (!apiKeyInput.startsWith('mk_')) {
      setError('Invalid API key format. API keys start with mk_')
      return
    }

    setLoading(true)
    try {
      // Store API key and redirect
      setApiKey(apiKeyInput.trim())
      navigate({ to: '/' })
    } catch {
      setError('Failed to authenticate')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-50 px-4">
      <div className="w-full max-w-sm">
        <div className="mb-8 text-center">
          <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-lg bg-primary-600">
            <span className="text-xl font-bold text-white">M</span>
          </div>
          <h1 className="text-2xl font-bold text-gray-900">Welcome to Marionette</h1>
          <p className="mt-2 text-sm text-gray-600">Enter your API key to continue</p>
        </div>

        <div className="rounded-lg border border-gray-200 bg-white p-6 shadow-sm">
          <form onSubmit={handleSubmit} className="space-y-4">
            {error && (
              <div className="rounded-lg bg-red-50 p-3 text-sm text-red-600">{error}</div>
            )}

            <Input
              label="API Key"
              type="password"
              value={apiKeyInput}
              onChange={(e) => setApiKeyInput(e.target.value)}
              placeholder="mk_..."
              required
              autoComplete="off"
            />

            <Button type="submit" className="w-full" loading={loading}>
              Continue
            </Button>
          </form>
        </div>

        <div className="mt-4 text-center">
          <p className="text-sm text-gray-500">
            Need an API key?{' '}
            <Link to="/admin/login" className="font-medium text-primary-600 hover:text-primary-700">
              Create one in Admin
            </Link>
          </p>
        </div>
      </div>
    </div>
  )
}
