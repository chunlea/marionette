import { createFileRoute, Link } from '@tanstack/react-router'
import { useSessions, useTasks, useRunners, usePendingPermissions } from '@/api/hooks'
import { Card, CardHeader, CardBody } from '@/components/Card'
import { Badge } from '@/components/Badge'
import { formatRelativeTime } from '@/lib/utils'
import { PlayCircle, ListTodo, Server, ShieldAlert, AlertCircle, Clock, CheckCircle2 } from 'lucide-react'

export const Route = createFileRoute('/_layout/')({
  component: DashboardHome,
})

function DashboardHome() {
  const { data: sessions } = useSessions()
  const { data: tasks } = useTasks()
  const { data: runners } = useRunners()
  const { data: permissions } = usePendingPermissions()

  const activeSessions = sessions?.items?.filter((s) => s.status === 'active').length || 0
  const pendingSessions = sessions?.items?.filter((s) => s.status === 'pending').length || 0
  const suspendedSessions = sessions?.items?.filter((s) => s.status === 'suspended').length || 0

  const runningTasks = tasks?.items?.filter((t) => t.status === 'running').length || 0
  const pendingTasks = tasks?.items?.filter((t) => t.status === 'pending').length || 0
  const completedTasks = tasks?.items?.filter((t) => t.status === 'completed').length || 0

  const onlineRunners = runners?.items?.filter((r) => r.status !== 'offline').length || 0
  const busyRunners = runners?.items?.filter((r) => r.status === 'busy').length || 0

  const pendingPerms = permissions?.items?.length || 0

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-900">Dashboard</h1>
        <p className="mt-1 text-sm text-gray-600">
          Overview of your Marionette workspace
        </p>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          title="Sessions"
          value={sessions?.items?.length || 0}
          icon={PlayCircle}
          color="blue"
          subtitle={`${activeSessions} active, ${suspendedSessions} suspended`}
          to="/sessions"
        />
        <StatCard
          title="Tasks"
          value={tasks?.items?.length || 0}
          icon={ListTodo}
          color="green"
          subtitle={`${runningTasks} running, ${pendingTasks} pending`}
          to="/tasks"
        />
        <StatCard
          title="Runners"
          value={runners?.items?.length || 0}
          icon={Server}
          color="purple"
          subtitle={`${onlineRunners} online, ${busyRunners} busy`}
          to="/runners"
        />
        <StatCard
          title="Permissions"
          value={pendingPerms}
          icon={ShieldAlert}
          color={pendingPerms > 0 ? 'red' : 'gray'}
          subtitle={pendingPerms > 0 ? 'Require attention' : 'None pending'}
          to="/permissions"
        />
      </div>

      {/* Recent Activity */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        {/* Recent Sessions */}
        <Card>
          <CardHeader className="flex items-center justify-between">
            <span>Recent Sessions</span>
            <Link
              to="/sessions"
              className="text-sm font-medium text-primary-600 hover:text-primary-700"
            >
              View all
            </Link>
          </CardHeader>
          <CardBody className="divide-y divide-gray-100">
            {!sessions?.items?.length ? (
              <p className="py-4 text-center text-sm text-gray-500">No sessions yet</p>
            ) : (
              sessions.items.slice(0, 5).map((session) => (
                <Link
                  key={session.id}
                  to="/sessions/$sessionId"
                  params={{ sessionId: session.id }}
                  className="flex items-center justify-between py-3 hover:bg-gray-50 -mx-4 px-4 first:-mt-2 last:-mb-2"
                >
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium text-gray-900">
                      {session.name || session.id}
                    </p>
                    <p className="text-xs text-gray-500">
                      {session.agent} · {formatRelativeTime(session.created_at)}
                    </p>
                  </div>
                  <SessionStatusBadge status={session.status} />
                </Link>
              ))
            )}
          </CardBody>
        </Card>

        {/* Recent Tasks */}
        <Card>
          <CardHeader className="flex items-center justify-between">
            <span>Recent Tasks</span>
            <Link
              to="/tasks"
              className="text-sm font-medium text-primary-600 hover:text-primary-700"
            >
              View all
            </Link>
          </CardHeader>
          <CardBody className="divide-y divide-gray-100">
            {!tasks?.items?.length ? (
              <p className="py-4 text-center text-sm text-gray-500">No tasks yet</p>
            ) : (
              tasks.items.slice(0, 5).map((task) => (
                <Link
                  key={task.id}
                  to="/tasks/$taskId"
                  params={{ taskId: task.id }}
                  className="flex items-center justify-between py-3 hover:bg-gray-50 -mx-4 px-4 first:-mt-2 last:-mb-2"
                >
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium text-gray-900">
                      {task.prompt.length > 50 ? task.prompt.slice(0, 50) + '...' : task.prompt}
                    </p>
                    <p className="text-xs text-gray-500">
                      {formatRelativeTime(task.created_at)}
                    </p>
                  </div>
                  <TaskStatusBadge status={task.status} />
                </Link>
              ))
            )}
          </CardBody>
        </Card>
      </div>

      {/* Pending Permissions Alert */}
      {pendingPerms > 0 && (
        <Card className="border-red-200 bg-red-50">
          <CardBody className="flex items-center gap-4">
            <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-full bg-red-100">
              <AlertCircle className="h-5 w-5 text-red-600" />
            </div>
            <div className="flex-1">
              <p className="font-medium text-red-800">
                {pendingPerms} permission request{pendingPerms > 1 ? 's' : ''} pending
              </p>
              <p className="text-sm text-red-600">
                Tasks are waiting for your approval to continue.
              </p>
            </div>
            <Link
              to="/permissions"
              className="rounded-lg bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700"
            >
              Review
            </Link>
          </CardBody>
        </Card>
      )}
    </div>
  )
}

interface StatCardProps {
  title: string
  value: number
  icon: React.ComponentType<{ className?: string }>
  color: 'blue' | 'green' | 'purple' | 'red' | 'gray'
  subtitle: string
  to: string
}

const colorStyles = {
  blue: 'bg-blue-50 text-blue-600',
  green: 'bg-green-50 text-green-600',
  purple: 'bg-purple-50 text-purple-600',
  red: 'bg-red-50 text-red-600',
  gray: 'bg-gray-50 text-gray-600',
}

function StatCard({ title, value, icon: Icon, color, subtitle, to }: StatCardProps) {
  return (
    <Link to={to}>
      <Card className="transition-shadow hover:shadow-md">
        <CardBody className="flex items-center gap-4">
          <div className={`flex h-12 w-12 flex-shrink-0 items-center justify-center rounded-lg ${colorStyles[color]}`}>
            <Icon className="h-6 w-6" />
          </div>
          <div>
            <p className="text-sm font-medium text-gray-600">{title}</p>
            <p className="text-2xl font-bold text-gray-900">{value}</p>
            <p className="text-xs text-gray-500">{subtitle}</p>
          </div>
        </CardBody>
      </Card>
    </Link>
  )
}

function SessionStatusBadge({ status }: { status: string }) {
  switch (status) {
    case 'active':
      return <Badge variant="success">Active</Badge>
    case 'pending':
      return <Badge variant="warning">Pending</Badge>
    case 'suspended':
      return <Badge variant="info">Suspended</Badge>
    case 'resuming':
      return <Badge variant="info">Resuming</Badge>
    case 'terminated':
      return <Badge variant="danger">Terminated</Badge>
    default:
      return <Badge>{status}</Badge>
  }
}

function TaskStatusBadge({ status }: { status: string }) {
  switch (status) {
    case 'running':
      return (
        <Badge variant="info" className="flex items-center gap-1">
          <Clock className="h-3 w-3" />
          Running
        </Badge>
      )
    case 'pending':
      return <Badge variant="warning">Pending</Badge>
    case 'completed':
      return (
        <Badge variant="success" className="flex items-center gap-1">
          <CheckCircle2 className="h-3 w-3" />
          Completed
        </Badge>
      )
    case 'failed':
      return <Badge variant="danger">Failed</Badge>
    case 'canceled':
      return <Badge>Canceled</Badge>
    default:
      return <Badge>{status}</Badge>
  }
}
