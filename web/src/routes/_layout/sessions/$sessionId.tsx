import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useSession, useTasks } from '@/api/hooks'
import { Card, CardHeader, CardBody } from '@/components/Card'
import { Badge } from '@/components/Badge'
import { Button } from '@/components/Button'
import { DesktopStreamCard } from '@/components/DesktopStreamCard'
import { streamingEnabled } from '@/lib/features'
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
  TableEmpty,
  TableLoading,
} from '@/components/Table'
import { formatRelativeTime } from '@/lib/utils'
import {
  ArrowLeft,
  PlayCircle,
  PauseCircle,
  StopCircle,
  Server,
  Clock,
  CheckCircle2,
  AlertCircle,
  ExternalLink,
} from 'lucide-react'

export const Route = createFileRoute('/_layout/sessions/$sessionId')({
  component: SessionDetailPage,
})

function SessionDetailPage() {
  const { sessionId } = Route.useParams()
  const navigate = useNavigate()
  const { data: session, isLoading: sessionLoading } = useSession(sessionId)
  const { data: tasks, isLoading: tasksLoading } = useTasks({ session_id: sessionId })

  if (sessionLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary-600 border-t-transparent" />
      </div>
    )
  }

  if (!session) {
    return (
      <div className="py-12 text-center">
        <h2 className="text-lg font-medium text-gray-900">Session not found</h2>
        <p className="mt-2 text-sm text-gray-500">
          The session you're looking for doesn't exist.
        </p>
        <Button
          variant="secondary"
          className="mt-4"
          onClick={() => navigate({ to: '/sessions' })}
        >
          Back to Sessions
        </Button>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center gap-4">
        <button
          onClick={() => navigate({ to: '/sessions' })}
          className="rounded-lg p-2 text-gray-500 hover:bg-gray-100 hover:text-gray-700"
        >
          <ArrowLeft className="h-5 w-5" />
        </button>
        <div className="flex-1">
          <h1 className="text-2xl font-bold text-gray-900">
            {session.name || 'Unnamed Session'}
          </h1>
          <p className="text-sm text-gray-500 font-mono">{session.id}</p>
        </div>
        <SessionStatusBadge status={session.status} />
      </div>

      {/* Session Info */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader>Session Details</CardHeader>
          <CardBody>
            <dl className="grid grid-cols-2 gap-4">
              <div>
                <dt className="text-sm font-medium text-gray-500">Agent</dt>
                <dd className="mt-1">
                  <Badge variant="info">{session.agent}</Badge>
                </dd>
              </div>
              <div>
                <dt className="text-sm font-medium text-gray-500">Status</dt>
                <dd className="mt-1">
                  <SessionStatusBadge status={session.status} />
                </dd>
              </div>
              <div>
                <dt className="text-sm font-medium text-gray-500">Lifecycle Mode</dt>
                <dd className="mt-1 text-sm text-gray-900">{session.lifecycle_mode}</dd>
              </div>
              <div>
                <dt className="text-sm font-medium text-gray-500">Network Policy</dt>
                <dd className="mt-1 text-sm text-gray-900">{session.network_policy}</dd>
              </div>
              <div>
                <dt className="text-sm font-medium text-gray-500">Created</dt>
                <dd className="mt-1 text-sm text-gray-900">
                  {formatRelativeTime(session.created_at)}
                </dd>
              </div>
              <div>
                <dt className="text-sm font-medium text-gray-500">Last Activity</dt>
                <dd className="mt-1 text-sm text-gray-900">
                  {formatRelativeTime(session.last_activity_at)}
                </dd>
              </div>
            </dl>
          </CardBody>
        </Card>

        <Card>
          <CardHeader icon={<Server className="h-4 w-4" />}>
            Runner Info
          </CardHeader>
          <CardBody>
            {session.runner_id ? (
              <dl className="grid grid-cols-2 gap-4">
                <div className="col-span-2">
                  <dt className="text-sm font-medium text-gray-500">Runner ID</dt>
                  <dd className="mt-1">
                    <code className="rounded bg-gray-100 px-2 py-1 text-xs">
                      {session.runner_id}
                    </code>
                  </dd>
                </div>
                <div>
                  <dt className="text-sm font-medium text-gray-500">Workspace ID</dt>
                  <dd className="mt-1">
                    <code className="rounded bg-gray-100 px-2 py-1 text-xs">
                      {session.workspace_id}
                    </code>
                  </dd>
                </div>
                <div>
                  <dt className="text-sm font-medium text-gray-500">BYOK</dt>
                  <dd className="mt-1 text-sm text-gray-900">
                    {session.is_byok ? 'Yes' : 'No'}
                  </dd>
                </div>
              </dl>
            ) : (
              <div className="py-4 text-center text-sm text-gray-500">
                No runner attached
              </div>
            )}
          </CardBody>
        </Card>

        {/* Desktop stream — frozen subsystem, off unless enabled. */}
        {streamingEnabled && (
          <DesktopStreamCard
            sessionId={session.id}
            sessionStatus={session.status}
            runnerId={session.runner_id}
          />
        )}
      </div>

      {/* Session Actions */}
      <Card>
        <CardHeader>Actions</CardHeader>
        <CardBody className="flex flex-wrap gap-2">
          {session.status === 'suspended' && (
            <Button>
              <PlayCircle className="mr-2 h-4 w-4" />
              Resume Session
            </Button>
          )}
          {session.status === 'active' && (
            <Button variant="secondary">
              <PauseCircle className="mr-2 h-4 w-4" />
              Suspend Session
            </Button>
          )}
          {session.status !== 'terminated' && (
            <Button variant="danger">
              <StopCircle className="mr-2 h-4 w-4" />
              Terminate Session
            </Button>
          )}
        </CardBody>
      </Card>

      {/* Tasks List */}
      <Card>
        <CardHeader
          action={
            <span className="text-sm font-normal text-gray-500">
              {tasks?.items?.length || 0} total
            </span>
          }
        >
          Tasks
        </CardHeader>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Prompt</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Retries</TableHead>
              <TableHead>Created</TableHead>
              <TableHead className="w-16">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {tasksLoading ? (
              <TableLoading colSpan={5} />
            ) : !tasks?.items?.length ? (
              <TableEmpty colSpan={5} message="No tasks for this session" />
            ) : (
              tasks.items.map((task) => (
                <TableRow key={task.id}>
                  <TableCell>
                    <div>
                      <p className="font-medium text-gray-900 truncate max-w-md">
                        {task.prompt.length > 80
                          ? task.prompt.slice(0, 80) + '...'
                          : task.prompt}
                      </p>
                      <p className="text-xs text-gray-500 font-mono">{task.id}</p>
                    </div>
                  </TableCell>
                  <TableCell>
                    <TaskStatusBadge status={task.status} />
                  </TableCell>
                  <TableCell className="text-gray-500">
                    {task.retry_count}/{task.max_retries}
                  </TableCell>
                  <TableCell className="text-gray-500">
                    {formatRelativeTime(task.created_at)}
                  </TableCell>
                  <TableCell>
                    <Link
                      to="/tasks/$taskId"
                      params={{ taskId: task.id }}
                      className="inline-flex items-center gap-1 text-sm text-primary-600 hover:text-primary-700"
                    >
                      <ExternalLink className="h-4 w-4" />
                    </Link>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </Card>

      {/* Labels & Annotations */}
      {(Object.keys(session.labels || {}).length > 0 ||
        Object.keys(session.annotations || {}).length > 0) && (
        <Card>
          <CardHeader>Labels & Annotations</CardHeader>
          <CardBody className="space-y-4">
            {Object.keys(session.labels || {}).length > 0 && (
              <div>
                <h4 className="text-sm font-medium text-gray-700 mb-2">Labels</h4>
                <div className="flex flex-wrap gap-2">
                  {Object.entries(session.labels || {}).map(([key, value]) => (
                    <Badge key={key}>
                      {key}: {value as string}
                    </Badge>
                  ))}
                </div>
              </div>
            )}
            {Object.keys(session.annotations || {}).length > 0 && (
              <div>
                <h4 className="text-sm font-medium text-gray-700 mb-2">Annotations</h4>
                <div className="flex flex-wrap gap-2">
                  {Object.entries(session.annotations || {}).map(([key, value]) => (
                    <Badge key={key} variant="info">
                      {key}: {String(value)}
                    </Badge>
                  ))}
                </div>
              </div>
            )}
          </CardBody>
        </Card>
      )}
    </div>
  )
}

function SessionStatusBadge({ status }: { status: string }) {
  switch (status) {
    case 'active':
      return (
        <Badge variant="success" className="flex items-center gap-1">
          <PlayCircle className="h-3 w-3" />
          Active
        </Badge>
      )
    case 'pending':
      return (
        <Badge variant="warning" className="flex items-center gap-1">
          <Clock className="h-3 w-3" />
          Pending
        </Badge>
      )
    case 'suspended':
      return (
        <Badge variant="info" className="flex items-center gap-1">
          <PauseCircle className="h-3 w-3" />
          Suspended
        </Badge>
      )
    case 'resuming':
      return (
        <Badge variant="info" className="flex items-center gap-1">
          <PlayCircle className="h-3 w-3" />
          Resuming
        </Badge>
      )
    case 'terminated':
      return (
        <Badge variant="danger" className="flex items-center gap-1">
          <StopCircle className="h-3 w-3" />
          Terminated
        </Badge>
      )
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
      return (
        <Badge variant="danger" className="flex items-center gap-1">
          <AlertCircle className="h-3 w-3" />
          Failed
        </Badge>
      )
    case 'canceled':
      return <Badge>Canceled</Badge>
    default:
      return <Badge>{status}</Badge>
  }
}
