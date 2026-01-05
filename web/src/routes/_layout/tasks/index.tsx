import { createFileRoute, Link } from '@tanstack/react-router'
import { useTasks } from '@/api/hooks'
import { Card } from '@/components/Card'
import { Badge } from '@/components/Badge'
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
import { ExternalLink, Clock, CheckCircle2, AlertCircle, XCircle } from 'lucide-react'

export const Route = createFileRoute('/_layout/tasks/')({
  component: TasksPage,
})

function TasksPage() {
  const { data, isLoading } = useTasks()

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-900">Tasks</h1>
        <p className="mt-1 text-sm text-gray-600">
          View and manage task executions
        </p>
      </div>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Prompt</TableHead>
              <TableHead>Session</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Retries</TableHead>
              <TableHead>Created</TableHead>
              <TableHead className="w-16">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableLoading colSpan={6} />
            ) : !data?.items?.length ? (
              <TableEmpty colSpan={6} message="No tasks found" />
            ) : (
              data.items.map((task) => (
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
                    <Link
                      to="/sessions/$sessionId"
                      params={{ sessionId: task.session_id }}
                      className="text-sm text-primary-600 hover:text-primary-700 font-mono"
                    >
                      {task.session_id}
                    </Link>
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
    </div>
  )
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
