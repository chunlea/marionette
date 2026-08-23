import { useState } from 'react'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useTask, usePermissions } from '@/api/hooks'
import { Card, CardHeader, CardBody } from '@/components/Card'
import { Badge } from '@/components/Badge'
import { Button } from '@/components/Button'
import { LogViewer } from '@/components/LogViewer'
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
  TableLoading,
} from '@/components/Table'
import { formatRelativeTime } from '@/lib/utils'
import {
  ArrowLeft,
  XCircle,
  RefreshCw,
  Clock,
  CheckCircle2,
  AlertCircle,
  PlayCircle,
  Terminal,
  ShieldAlert,
} from 'lucide-react'

export const Route = createFileRoute('/_layout/tasks/$taskId')({
  component: TaskDetailPage,
})

function TaskDetailPage() {
  const { taskId } = Route.useParams()
  const navigate = useNavigate()
  const [showLogs, setShowLogs] = useState(false)
  const { data: task, isLoading: taskLoading } = useTask(taskId)
  const { data: permissions, isLoading: permsLoading } = usePermissions({ task_id: taskId })

  if (taskLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary-600 border-t-transparent" />
      </div>
    )
  }

  if (!task) {
    return (
      <div className="py-12 text-center">
        <h2 className="text-lg font-medium text-gray-900">Task not found</h2>
        <p className="mt-2 text-sm text-gray-500">
          The task you're looking for doesn't exist.
        </p>
        <Button
          variant="secondary"
          className="mt-4"
          onClick={() => navigate({ to: '/tasks' })}
        >
          Back to Tasks
        </Button>
      </div>
    )
  }

  const pendingPerms = permissions?.items?.filter((p) => p.status === 'pending') || []

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center gap-4">
        <button
          onClick={() => navigate({ to: '/tasks' })}
          className="rounded-lg p-2 text-gray-500 hover:bg-gray-100 hover:text-gray-700"
        >
          <ArrowLeft className="h-5 w-5" />
        </button>
        <div className="flex-1">
          <h1 className="text-2xl font-bold text-gray-900">Task Details</h1>
          <p className="text-sm text-gray-500 font-mono">{task.id}</p>
        </div>
        <TaskStatusBadge status={task.status} />
      </div>

      {/* Pending Permissions Alert */}
      {pendingPerms.length > 0 && (
        <Card className="border-amber-200 bg-amber-50">
          <CardBody className="flex items-center gap-4">
            <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-full bg-amber-100">
              <ShieldAlert className="h-5 w-5 text-amber-600" />
            </div>
            <div className="flex-1">
              <p className="font-medium text-amber-800">
                {pendingPerms.length} pending permission request{pendingPerms.length > 1 ? 's' : ''}
              </p>
              <p className="text-sm text-amber-600">
                This task is waiting for your approval to continue.
              </p>
            </div>
            <Link
              to="/permissions"
              className="rounded-lg bg-amber-600 px-4 py-2 text-sm font-medium text-white hover:bg-amber-700"
            >
              Review
            </Link>
          </CardBody>
        </Card>
      )}

      {/* Task Info */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader>Task Details</CardHeader>
          <CardBody>
            <dl className="space-y-4">
              <div>
                <dt className="text-sm font-medium text-gray-500">Prompt</dt>
                <dd className="mt-1 whitespace-pre-wrap text-sm text-gray-900 bg-gray-50 rounded-lg p-3">
                  {task.prompt}
                </dd>
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <dt className="text-sm font-medium text-gray-500">Status</dt>
                  <dd className="mt-1">
                    <TaskStatusBadge status={task.status} />
                  </dd>
                </div>
                <div>
                  <dt className="text-sm font-medium text-gray-500">Retries</dt>
                  <dd className="mt-1 text-sm text-gray-900">
                    {task.retry_count}/{task.max_retries}
                  </dd>
                </div>
                <div>
                  <dt className="text-sm font-medium text-gray-500">Timeout</dt>
                  <dd className="mt-1 text-sm text-gray-900">
                    {task.timeout_seconds}s
                  </dd>
                </div>
                <div>
                  <dt className="text-sm font-medium text-gray-500">Created</dt>
                  <dd className="mt-1 text-sm text-gray-900">
                    {formatRelativeTime(task.created_at)}
                  </dd>
                </div>
              </div>
            </dl>
          </CardBody>
        </Card>

        <Card>
          <CardHeader>Session Info</CardHeader>
          <CardBody>
            <dl className="space-y-4">
              <div>
                <dt className="text-sm font-medium text-gray-500">Session ID</dt>
                <dd className="mt-1">
                  <Link
                    to="/sessions/$sessionId"
                    params={{ sessionId: task.session_id }}
                    className="text-sm text-primary-600 hover:text-primary-700 font-mono"
                  >
                    {task.session_id}
                  </Link>
                </dd>
              </div>
            </dl>
          </CardBody>
        </Card>
      </div>

      {/* Task Actions */}
      <Card>
        <CardHeader>Actions</CardHeader>
        <CardBody className="flex flex-wrap gap-2">
          {task.status === 'running' && (
            <Button variant="danger">
              <XCircle className="mr-2 h-4 w-4" />
              Cancel Task
            </Button>
          )}
          {task.status === 'failed' && task.retry_count < task.max_retries && (
            <Button>
              <RefreshCw className="mr-2 h-4 w-4" />
              Retry Task
            </Button>
          )}
          <Button variant="secondary" onClick={() => setShowLogs(!showLogs)}>
            <Terminal className="mr-2 h-4 w-4" />
            {showLogs ? 'Hide Logs' : 'View Logs'}
          </Button>
        </CardBody>
      </Card>

      {/* Log Viewer */}
      {showLogs && (
        <Card>
          <CardHeader className="flex items-center gap-2">
            <Terminal className="h-4 w-4" />
            Task Logs
          </CardHeader>
          <CardBody className="p-0">
            <LogViewer taskId={taskId} maxHeight="400px" showTimestamps />
          </CardBody>
        </Card>
      )}

      {/* Permission Requests */}
      {permissions?.items && permissions.items.length > 0 && (
        <Card>
          <CardHeader icon={<ShieldAlert className="h-4 w-4" />}>
            Permission Requests
          </CardHeader>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Tool</TableHead>
                <TableHead>Action</TableHead>
                <TableHead>Risk Level</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {permsLoading ? (
                <TableLoading colSpan={5} />
              ) : (
                permissions.items.map((perm) => (
                  <TableRow key={perm.id}>
                    <TableCell>
                      <Badge variant="info">{perm.tool}</Badge>
                    </TableCell>
                    <TableCell>
                      <code className="text-xs text-gray-600 truncate max-w-xs block">
                        {perm.action}
                      </code>
                    </TableCell>
                    <TableCell>
                      <RiskBadge level={perm.risk_level} />
                    </TableCell>
                    <TableCell>
                      <PermissionStatusBadge status={perm.status} />
                    </TableCell>
                    <TableCell className="text-gray-500">
                      {formatRelativeTime(perm.created_at)}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </Card>
      )}

      {/* Labels & Annotations */}
      {(Object.keys(task.labels || {}).length > 0 ||
        Object.keys(task.annotations || {}).length > 0) && (
        <Card>
          <CardHeader>Labels & Annotations</CardHeader>
          <CardBody className="space-y-4">
            {Object.keys(task.labels || {}).length > 0 && (
              <div>
                <h4 className="text-sm font-medium text-gray-700 mb-2">Labels</h4>
                <div className="flex flex-wrap gap-2">
                  {Object.entries(task.labels || {}).map(([key, value]) => (
                    <Badge key={key}>
                      {key}: {value as string}
                    </Badge>
                  ))}
                </div>
              </div>
            )}
            {Object.keys(task.annotations || {}).length > 0 && (
              <div>
                <h4 className="text-sm font-medium text-gray-700 mb-2">Annotations</h4>
                <div className="flex flex-wrap gap-2">
                  {Object.entries(task.annotations || {}).map(([key, value]) => (
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

function TaskStatusBadge({ status }: { status: string }) {
  switch (status) {
    case 'running':
      return (
        <Badge variant="info" className="flex items-center gap-1">
          <PlayCircle className="h-3 w-3" />
          Running
        </Badge>
      )
    case 'pending':
      return (
        <Badge variant="warning" className="flex items-center gap-1">
          <Clock className="h-3 w-3" />
          Pending
        </Badge>
      )
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
      return (
        <Badge className="flex items-center gap-1">
          <XCircle className="h-3 w-3" />
          Canceled
        </Badge>
      )
    default:
      return <Badge>{status}</Badge>
  }
}

function RiskBadge({ level }: { level: string }) {
  switch (level) {
    case 'low':
      return <Badge variant="success">Low</Badge>
    case 'medium':
      return <Badge variant="warning">Medium</Badge>
    case 'high':
      return <Badge variant="danger">High</Badge>
    case 'critical':
      return (
        <Badge variant="danger" className="bg-red-700">
          Critical
        </Badge>
      )
    default:
      return <Badge>{level}</Badge>
  }
}

function PermissionStatusBadge({ status }: { status: string }) {
  switch (status) {
    case 'pending':
      return <Badge variant="warning">Pending</Badge>
    case 'approved':
      return <Badge variant="success">Approved</Badge>
    case 'denied':
      return <Badge variant="danger">Denied</Badge>
    case 'canceled':
      return <Badge>Canceled</Badge>
    default:
      return <Badge>{status}</Badge>
  }
}
