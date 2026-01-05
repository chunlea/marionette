import { createFileRoute, Link } from '@tanstack/react-router'
import { useSessions } from '@/api/hooks'
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
import { ExternalLink } from 'lucide-react'

export const Route = createFileRoute('/_layout/sessions/')({
  component: SessionsPage,
})

function SessionsPage() {
  const { data, isLoading } = useSessions()

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-900">Sessions</h1>
        <p className="mt-1 text-sm text-gray-600">
          View and manage your agent sessions
        </p>
      </div>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name / ID</TableHead>
              <TableHead>Agent</TableHead>
              <TableHead>Runner</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Last Activity</TableHead>
              <TableHead>Created</TableHead>
              <TableHead className="w-16">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableLoading colSpan={7} />
            ) : !data?.items?.length ? (
              <TableEmpty colSpan={7} message="No sessions found" />
            ) : (
              data.items.map((session) => (
                <TableRow key={session.id}>
                  <TableCell>
                    <div>
                      <p className="font-medium text-gray-900">
                        {session.name || 'Unnamed Session'}
                      </p>
                      <p className="text-xs text-gray-500 font-mono">{session.id}</p>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge variant="info">{session.agent}</Badge>
                  </TableCell>
                  <TableCell>
                    {session.runner_id ? (
                      <code className="text-xs text-gray-600">{session.runner_id}</code>
                    ) : (
                      <span className="text-gray-400">-</span>
                    )}
                  </TableCell>
                  <TableCell>
                    <SessionStatusBadge status={session.status} />
                  </TableCell>
                  <TableCell className="text-gray-500">
                    {formatRelativeTime(session.last_activity_at)}
                  </TableCell>
                  <TableCell className="text-gray-500">
                    {formatRelativeTime(session.created_at)}
                  </TableCell>
                  <TableCell>
                    <Link
                      to="/sessions/$sessionId"
                      params={{ sessionId: session.id }}
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
