import { createFileRoute } from '@tanstack/react-router'
import { useRunners } from '@/api/hooks'
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
import { Server, Wifi, WifiOff, Loader2, AlertTriangle } from 'lucide-react'

export const Route = createFileRoute('/_layout/runners')({
  component: RunnersPage,
})

function RunnersPage() {
  const { data, isLoading } = useRunners()

  const onlineCount = data?.items?.filter((r) => r.status !== 'offline').length || 0
  const busyCount = data?.items?.filter((r) => r.status === 'busy').length || 0
  const taintedCount = data?.items?.filter((r) => r.tainted).length || 0

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-900">Runners</h1>
        <p className="mt-1 text-sm text-gray-600">
          View connected runners and their status
        </p>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-4">
        <Card className="p-4">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-gray-100">
              <Server className="h-5 w-5 text-gray-600" />
            </div>
            <div>
              <p className="text-sm font-medium text-gray-500">Total</p>
              <p className="text-2xl font-bold text-gray-900">{data?.items?.length || 0}</p>
            </div>
          </div>
        </Card>
        <Card className="p-4">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-green-100">
              <Wifi className="h-5 w-5 text-green-600" />
            </div>
            <div>
              <p className="text-sm font-medium text-gray-500">Online</p>
              <p className="text-2xl font-bold text-green-600">{onlineCount}</p>
            </div>
          </div>
        </Card>
        <Card className="p-4">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-blue-100">
              <Loader2 className="h-5 w-5 text-blue-600" />
            </div>
            <div>
              <p className="text-sm font-medium text-gray-500">Busy</p>
              <p className="text-2xl font-bold text-blue-600">{busyCount}</p>
            </div>
          </div>
        </Card>
        <Card className="p-4">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-amber-100">
              <AlertTriangle className="h-5 w-5 text-amber-600" />
            </div>
            <div>
              <p className="text-sm font-medium text-gray-500">Tainted</p>
              <p className="text-2xl font-bold text-amber-600">{taintedCount}</p>
            </div>
          </div>
        </Card>
      </div>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name / ID</TableHead>
              <TableHead>Hostname</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Pool</TableHead>
              <TableHead>Sandbox Mode</TableHead>
              <TableHead>Capabilities</TableHead>
              <TableHead>Last Seen</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableLoading colSpan={7} />
            ) : !data?.items?.length ? (
              <TableEmpty colSpan={7} message="No runners found" />
            ) : (
              data.items.map((runner) => (
                <TableRow key={runner.id}>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      {runner.tainted && (
                        <span title="Tainted">
                          <AlertTriangle className="h-4 w-4 text-amber-500" />
                        </span>
                      )}
                      <div>
                        <p className="font-medium text-gray-900">{runner.name}</p>
                        <p className="text-xs text-gray-500 font-mono">{runner.id}</p>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell>
                    <code className="text-xs text-gray-600">{runner.hostname}</code>
                  </TableCell>
                  <TableCell>
                    <RunnerStatusBadge status={runner.status} />
                  </TableCell>
                  <TableCell>
                    {runner.pool_name ? (
                      <Badge variant="info">{runner.pool_name}</Badge>
                    ) : (
                      <span className="text-gray-400">-</span>
                    )}
                  </TableCell>
                  <TableCell>
                    <code className="text-xs text-gray-600">{runner.sandbox_mode}</code>
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1">
                      {runner.capabilities?.length ? (
                        runner.capabilities.slice(0, 3).map((cap) => (
                          <Badge key={cap} variant="info" className="text-xs">
                            {cap}
                          </Badge>
                        ))
                      ) : (
                        <span className="text-gray-400">-</span>
                      )}
                      {runner.capabilities && runner.capabilities.length > 3 && (
                        <Badge className="text-xs">+{runner.capabilities.length - 3}</Badge>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className="text-gray-500">
                    {formatRelativeTime(runner.last_seen_at)}
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

function RunnerStatusBadge({ status }: { status: string }) {
  switch (status) {
    case 'idle':
      return (
        <Badge variant="success" className="flex items-center gap-1">
          <Wifi className="h-3 w-3" />
          Idle
        </Badge>
      )
    case 'busy':
      return (
        <Badge variant="info" className="flex items-center gap-1">
          <Loader2 className="h-3 w-3 animate-spin" />
          Busy
        </Badge>
      )
    case 'paused':
      return <Badge variant="warning">Paused</Badge>
    case 'offline':
      return (
        <Badge variant="danger" className="flex items-center gap-1">
          <WifiOff className="h-3 w-3" />
          Offline
        </Badge>
      )
    default:
      return <Badge>{status}</Badge>
  }
}
