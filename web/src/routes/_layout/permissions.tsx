import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import {
  usePendingPermissions,
  usePermissions,
  useApprovePermission,
  useDenyPermission,
} from '@/api/hooks'
import { Card, CardHeader } from '@/components/Card'
import { Badge } from '@/components/Badge'
import { Button } from '@/components/Button'
import { Input } from '@/components/Input'
import { Dialog, DialogHeader, DialogBody, DialogFooter } from '@/components/Dialog'
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
  ShieldAlert,
  ShieldCheck,
  ShieldX,
  Clock,
  CheckCircle,
  XCircle,
  Terminal,
  AlertTriangle,
} from 'lucide-react'
import type { PermissionRequest } from '@/types/api'

export const Route = createFileRoute('/_layout/permissions')({
  component: PermissionsPage,
})

function PermissionsPage() {
  const { data: pending, isLoading: pendingLoading } = usePendingPermissions()
  const { data: all, isLoading: allLoading } = usePermissions()
  const [selectedPerm, setSelectedPerm] = useState<PermissionRequest | null>(null)
  const [actionType, setActionType] = useState<'approve' | 'deny' | null>(null)

  const pendingCount = pending?.items?.length || 0
  const approvedCount = all?.items?.filter((p) => p.status === 'approved').length || 0
  const deniedCount = all?.items?.filter((p) => p.status === 'denied').length || 0

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-900">Permission Requests</h1>
        <p className="mt-1 text-sm text-gray-600">
          Review and respond to agent permission requests
        </p>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <Card className="p-4">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-amber-100">
              <Clock className="h-5 w-5 text-amber-600" />
            </div>
            <div>
              <p className="text-sm font-medium text-gray-500">Pending</p>
              <p className="text-2xl font-bold text-amber-600">{pendingCount}</p>
            </div>
          </div>
        </Card>
        <Card className="p-4">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-green-100">
              <ShieldCheck className="h-5 w-5 text-green-600" />
            </div>
            <div>
              <p className="text-sm font-medium text-gray-500">Approved</p>
              <p className="text-2xl font-bold text-green-600">{approvedCount}</p>
            </div>
          </div>
        </Card>
        <Card className="p-4">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-red-100">
              <ShieldX className="h-5 w-5 text-red-600" />
            </div>
            <div>
              <p className="text-sm font-medium text-gray-500">Denied</p>
              <p className="text-2xl font-bold text-red-600">{deniedCount}</p>
            </div>
          </div>
        </Card>
      </div>

      {/* Pending Permissions */}
      {pendingCount > 0 && (
        <Card className="border-amber-200">
          <CardHeader className="flex items-center gap-2 bg-amber-50 text-amber-800">
            <ShieldAlert className="h-5 w-5" />
            Pending Permission Requests ({pendingCount})
          </CardHeader>
          <div className="divide-y divide-gray-100">
            {pendingLoading ? (
              <div className="flex items-center justify-center py-8">
                <div className="h-6 w-6 animate-spin rounded-full border-2 border-primary-600 border-t-transparent" />
              </div>
            ) : (
              pending?.items?.map((perm) => (
                <PermissionCard
                  key={perm.id}
                  permission={perm}
                  onApprove={() => {
                    setSelectedPerm(perm)
                    setActionType('approve')
                  }}
                  onDeny={() => {
                    setSelectedPerm(perm)
                    setActionType('deny')
                  }}
                />
              ))
            )}
          </div>
        </Card>
      )}

      {/* All Permissions History */}
      <Card>
        <CardHeader>Permission History</CardHeader>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Tool</TableHead>
              <TableHead>Action</TableHead>
              <TableHead>Risk</TableHead>
              <TableHead>Session</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Responded</TableHead>
              <TableHead>Created</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {allLoading ? (
              <TableLoading colSpan={7} />
            ) : !all?.items?.length ? (
              <TableEmpty colSpan={7} message="No permission requests" />
            ) : (
              all.items.map((perm) => (
                <TableRow key={perm.id}>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <Terminal className="h-4 w-4 text-gray-400" />
                      <Badge variant="info">{perm.tool}</Badge>
                    </div>
                  </TableCell>
                  <TableCell>
                    <code className="text-xs text-gray-600 truncate max-w-xs block">
                      {perm.action.length > 50 ? perm.action.slice(0, 50) + '...' : perm.action}
                    </code>
                  </TableCell>
                  <TableCell>
                    <RiskBadge level={perm.risk_level} />
                  </TableCell>
                  <TableCell>
                    <Link
                      to="/sessions/$sessionId"
                      params={{ sessionId: perm.session_id }}
                      className="text-sm text-primary-600 hover:text-primary-700 font-mono"
                    >
                      {perm.session_id.slice(0, 16)}...
                    </Link>
                  </TableCell>
                  <TableCell>
                    <PermissionStatusBadge status={perm.status} />
                  </TableCell>
                  <TableCell className="text-gray-500">
                    {perm.responded_at ? formatRelativeTime(perm.responded_at) : '-'}
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

      {/* Action Dialog */}
      <PermissionActionDialog
        permission={selectedPerm}
        actionType={actionType}
        onClose={() => {
          setSelectedPerm(null)
          setActionType(null)
        }}
      />
    </div>
  )
}

interface PermissionCardProps {
  permission: PermissionRequest
  onApprove: () => void
  onDeny: () => void
}

function PermissionCard({ permission, onApprove, onDeny }: PermissionCardProps) {
  return (
    <div className="p-4 hover:bg-gray-50">
      <div className="flex items-start gap-4">
        <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg bg-amber-100">
          <Terminal className="h-5 w-5 text-amber-600" />
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-1">
            <Badge variant="info">{permission.tool}</Badge>
            <RiskBadge level={permission.risk_level} />
          </div>
          <div className="bg-gray-900 rounded-lg p-3 mb-2 max-h-64 overflow-auto">
            <pre className="text-sm text-green-400 whitespace-pre-wrap break-words m-0">{formatAction(permission.action)}</pre>
          </div>
          {permission.context && (
            <p className="text-sm text-gray-600 mb-2">{permission.context}</p>
          )}
          <div className="flex items-center gap-4 text-xs text-gray-500">
            <span>
              Session:{' '}
              <Link
                to="/sessions/$sessionId"
                params={{ sessionId: permission.session_id }}
                className="text-primary-600 hover:text-primary-700"
              >
                {permission.session_id}
              </Link>
            </span>
            <span>
              Task:{' '}
              <Link
                to="/tasks/$taskId"
                params={{ taskId: permission.task_id }}
                className="text-primary-600 hover:text-primary-700"
              >
                {permission.task_id}
              </Link>
            </span>
            <span>{formatRelativeTime(permission.created_at)}</span>
          </div>
        </div>
        <div className="flex flex-shrink-0 gap-2">
          <Button variant="secondary" size="sm" onClick={onDeny}>
            <XCircle className="mr-1 h-4 w-4" />
            Deny
          </Button>
          <Button size="sm" onClick={onApprove}>
            <CheckCircle className="mr-1 h-4 w-4" />
            Approve
          </Button>
        </div>
      </div>
    </div>
  )
}

interface PermissionActionDialogProps {
  permission: PermissionRequest | null
  actionType: 'approve' | 'deny' | null
  onClose: () => void
}

function PermissionActionDialog({
  permission,
  actionType,
  onClose,
}: PermissionActionDialogProps) {
  const approvePermission = useApprovePermission()
  const denyPermission = useDenyPermission()
  const [reason, setReason] = useState('')

  const handleSubmit = async () => {
    if (!permission) return

    try {
      if (actionType === 'approve') {
        await approvePermission.mutateAsync({
          permissionId: permission.id,
          response: reason ? { reason } : undefined,
        })
      } else {
        await denyPermission.mutateAsync({
          permissionId: permission.id,
          response: reason ? { reason } : undefined,
        })
      }
      setReason('')
      onClose()
    } catch (error) {
      console.error('Failed to respond to permission:', error)
    }
  }

  const isLoading = approvePermission.isPending || denyPermission.isPending
  const isApprove = actionType === 'approve'

  return (
    <Dialog open={!!permission && !!actionType} onClose={onClose}>
      <DialogHeader onClose={onClose}>
        <div className="flex items-center gap-2">
          {isApprove ? (
            <ShieldCheck className="h-5 w-5 text-green-600" />
          ) : (
            <ShieldX className="h-5 w-5 text-red-600" />
          )}
          {isApprove ? 'Approve Permission' : 'Deny Permission'}
        </div>
      </DialogHeader>
      <DialogBody className="space-y-4">
        {permission && (
          <>
            <div className="rounded-lg bg-gray-50 p-3">
              <div className="flex items-center gap-2 mb-2">
                <Badge variant="info">{permission.tool}</Badge>
                <RiskBadge level={permission.risk_level} />
              </div>
              <div className="bg-gray-900 rounded p-2 max-h-48 overflow-auto">
                <pre className="text-xs text-green-400 whitespace-pre-wrap break-words m-0">{formatAction(permission.action)}</pre>
              </div>
            </div>

            {permission.risk_level === 'high' || permission.risk_level === 'critical' ? (
              <div className="flex items-start gap-2 rounded-lg bg-red-50 p-3 text-sm text-red-700">
                <AlertTriangle className="h-5 w-5 flex-shrink-0" />
                <div>
                  <p className="font-medium">High-risk action</p>
                  <p>
                    This action has been flagged as {permission.risk_level} risk. Please review
                    carefully before approving.
                  </p>
                </div>
              </div>
            ) : null}

            <Input
              label={`Reason (optional)`}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder={
                isApprove ? 'Reason for approval...' : 'Reason for denial...'
              }
            />
          </>
        )}
      </DialogBody>
      <DialogFooter>
        <Button variant="secondary" onClick={onClose}>
          Cancel
        </Button>
        <Button
          variant={isApprove ? 'primary' : 'danger'}
          onClick={handleSubmit}
          loading={isLoading}
        >
          {isApprove ? 'Approve' : 'Deny'}
        </Button>
      </DialogFooter>
    </Dialog>
  )
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

function formatAction(action: string): string {
  try {
    const parsed = JSON.parse(action)
    return JSON.stringify(parsed, null, 2)
  } catch {
    return action
  }
}

function PermissionStatusBadge({ status }: { status: string }) {
  switch (status) {
    case 'pending':
      return (
        <Badge variant="warning" className="flex items-center gap-1">
          <Clock className="h-3 w-3" />
          Pending
        </Badge>
      )
    case 'approved':
      return (
        <Badge variant="success" className="flex items-center gap-1">
          <ShieldCheck className="h-3 w-3" />
          Approved
        </Badge>
      )
    case 'denied':
      return (
        <Badge variant="danger" className="flex items-center gap-1">
          <ShieldX className="h-3 w-3" />
          Denied
        </Badge>
      )
    case 'canceled':
      return <Badge>Canceled</Badge>
    default:
      return <Badge>{status}</Badge>
  }
}
