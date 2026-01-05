import { createFileRoute } from '@tanstack/react-router'
import { Layout } from '@/components/Layout'

export const Route = createFileRoute('/')({
  component: HomePage,
})

function HomePage() {
  return (
    <Layout>
      <div className="space-y-6">
        <div>
          <h1 className="text-3xl font-bold text-gray-900">Welcome to Marionette</h1>
          <p className="mt-2 text-gray-600">
            Remote agent orchestration and observability platform
          </p>
        </div>

        <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-4">
          <StatCard
            title="Active Sessions"
            value="-"
            description="Sessions currently running"
            icon="play-circle"
          />
          <StatCard
            title="Running Tasks"
            value="-"
            description="Tasks in progress"
            icon="activity"
          />
          <StatCard
            title="Online Runners"
            value="-"
            description="Connected runners"
            icon="server"
          />
          <StatCard
            title="Pending Permissions"
            value="-"
            description="Awaiting approval"
            icon="shield-alert"
          />
        </div>

        <div className="rounded-lg border border-gray-200 bg-white p-6">
          <h2 className="text-lg font-semibold text-gray-900">Quick Start</h2>
          <div className="mt-4 space-y-3 text-sm text-gray-600">
            <p>1. Create an API key in the Admin panel</p>
            <p>2. Configure an agent (Claude, Codex, etc.)</p>
            <p>3. Create a session and start executing tasks</p>
          </div>
        </div>
      </div>
    </Layout>
  )
}

interface StatCardProps {
  title: string
  value: string | number
  description: string
  icon: string
}

function StatCard({ title, value, description }: StatCardProps) {
  return (
    <div className="rounded-lg border border-gray-200 bg-white p-6">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium text-gray-500">{title}</h3>
      </div>
      <div className="mt-2">
        <p className="text-3xl font-semibold text-gray-900">{value}</p>
        <p className="mt-1 text-sm text-gray-500">{description}</p>
      </div>
    </div>
  )
}
