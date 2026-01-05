import { createFileRoute } from '@tanstack/react-router'
import { Card, CardHeader, CardBody } from '@/components/Card'

export const Route = createFileRoute('/admin/_layout/settings')({
  component: SettingsPage,
})

function SettingsPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-900">Settings</h1>
        <p className="mt-1 text-sm text-gray-600">
          System configuration and preferences
        </p>
      </div>

      <Card>
        <CardHeader>Server Information</CardHeader>
        <CardBody>
          <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <dt className="text-sm font-medium text-gray-500">Version</dt>
              <dd className="mt-1 text-sm text-gray-900">0.1.0</dd>
            </div>
            <div>
              <dt className="text-sm font-medium text-gray-500">Environment</dt>
              <dd className="mt-1 text-sm text-gray-900">Development</dd>
            </div>
            <div>
              <dt className="text-sm font-medium text-gray-500">API Endpoint</dt>
              <dd className="mt-1 text-sm text-gray-900">
                <code className="rounded bg-gray-100 px-2 py-1 text-xs">
                  http://localhost:8080
                </code>
              </dd>
            </div>
            <div>
              <dt className="text-sm font-medium text-gray-500">Admin Endpoint</dt>
              <dd className="mt-1 text-sm text-gray-900">
                <code className="rounded bg-gray-100 px-2 py-1 text-xs">
                  http://localhost:8081
                </code>
              </dd>
            </div>
          </dl>
        </CardBody>
      </Card>

      <Card>
        <CardHeader>Documentation</CardHeader>
        <CardBody className="space-y-2">
          <p className="text-sm text-gray-600">
            View the API documentation and explore available endpoints.
          </p>
          <div className="flex gap-2">
            <a
              href="/docs"
              target="_blank"
              rel="noopener noreferrer"
              className="text-sm font-medium text-primary-600 hover:text-primary-700"
            >
              Admin API Docs →
            </a>
          </div>
        </CardBody>
      </Card>
    </div>
  )
}
