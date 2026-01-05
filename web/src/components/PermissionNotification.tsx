import { useState, useEffect, useCallback } from 'react'
import { Link } from '@tanstack/react-router'
import { useApprovePermission, useDenyPermission } from '@/api/hooks'
import { Button } from './Button'
import { Badge } from './Badge'
import type { PermissionRequest } from '@/types/api'
import {
  ShieldAlert,
  CheckCircle,
  XCircle,
  Terminal,
  X,
  AlertTriangle,
} from 'lucide-react'

interface PermissionNotificationProps {
  permission: PermissionRequest
  onDismiss?: () => void
  autoHideDelay?: number
}

export function PermissionNotification({
  permission,
  onDismiss,
  autoHideDelay,
}: PermissionNotificationProps) {
  const [isVisible, setIsVisible] = useState(true)
  const [isProcessing, setIsProcessing] = useState(false)
  const approvePermission = useApprovePermission()
  const denyPermission = useDenyPermission()

  const handleDismiss = useCallback(() => {
    setIsVisible(false)
    setTimeout(() => onDismiss?.(), 300)
  }, [onDismiss])

  useEffect(() => {
    if (autoHideDelay && autoHideDelay > 0) {
      const timer = setTimeout(() => {
        handleDismiss()
      }, autoHideDelay)
      return () => clearTimeout(timer)
    }
  }, [autoHideDelay, handleDismiss])

  const handleApprove = async () => {
    setIsProcessing(true)
    try {
      await approvePermission.mutateAsync({ permissionId: permission.id })
      handleDismiss()
    } catch (error) {
      console.error('Failed to approve:', error)
      setIsProcessing(false)
    }
  }

  const handleDeny = async () => {
    setIsProcessing(true)
    try {
      await denyPermission.mutateAsync({ permissionId: permission.id })
      handleDismiss()
    } catch (error) {
      console.error('Failed to deny:', error)
      setIsProcessing(false)
    }
  }

  if (!isVisible) return null

  const isHighRisk = permission.risk_level === 'high' || permission.risk_level === 'critical'

  return (
    <div
      className={`
        fixed bottom-4 right-4 z-50 w-96 max-w-[calc(100vw-2rem)]
        rounded-lg border shadow-lg transition-all duration-300
        ${isVisible ? 'translate-y-0 opacity-100' : 'translate-y-4 opacity-0'}
        ${isHighRisk ? 'border-red-300 bg-red-50' : 'border-amber-300 bg-amber-50'}
      `}
    >
      {/* Header */}
      <div
        className={`
          flex items-center justify-between rounded-t-lg px-4 py-2
          ${isHighRisk ? 'bg-red-100' : 'bg-amber-100'}
        `}
      >
        <div className="flex items-center gap-2">
          <ShieldAlert className={`h-5 w-5 ${isHighRisk ? 'text-red-600' : 'text-amber-600'}`} />
          <span className={`font-medium ${isHighRisk ? 'text-red-800' : 'text-amber-800'}`}>
            Permission Request
          </span>
          <RiskBadge level={permission.risk_level} />
        </div>
        <button
          onClick={handleDismiss}
          className="rounded p-1 text-gray-500 hover:bg-white/50 hover:text-gray-700"
        >
          <X className="h-4 w-4" />
        </button>
      </div>

      {/* Body */}
      <div className="p-4">
        <div className="mb-3 flex items-center gap-2">
          <Terminal className="h-4 w-4 text-gray-400" />
          <Badge variant="info">{permission.tool}</Badge>
        </div>

        <div className="mb-3 rounded bg-gray-900 p-2">
          <code className="text-xs text-green-400 break-all">
            {permission.action.length > 100
              ? permission.action.slice(0, 100) + '...'
              : permission.action}
          </code>
        </div>

        {permission.context && (
          <p className="mb-3 text-sm text-gray-600">{permission.context}</p>
        )}

        {isHighRisk && (
          <div className="mb-3 flex items-start gap-2 rounded bg-red-100 p-2 text-xs text-red-700">
            <AlertTriangle className="mt-0.5 h-4 w-4 flex-shrink-0" />
            <span>This action has been flagged as {permission.risk_level} risk. Review carefully.</span>
          </div>
        )}

        <div className="mb-3 flex items-center gap-2 text-xs text-gray-500">
          <Link
            to="/sessions/$sessionId"
            params={{ sessionId: permission.session_id }}
            className="text-primary-600 hover:text-primary-700"
          >
            View Session
          </Link>
          <span>·</span>
          <Link
            to="/tasks/$taskId"
            params={{ taskId: permission.task_id }}
            className="text-primary-600 hover:text-primary-700"
          >
            View Task
          </Link>
        </div>

        {/* Actions */}
        <div className="flex gap-2">
          <Button
            variant="secondary"
            size="sm"
            onClick={handleDeny}
            loading={isProcessing && denyPermission.isPending}
            disabled={isProcessing}
            className="flex-1"
          >
            <XCircle className="mr-1 h-4 w-4" />
            Deny
          </Button>
          <Button
            size="sm"
            onClick={handleApprove}
            loading={isProcessing && approvePermission.isPending}
            disabled={isProcessing}
            className="flex-1"
          >
            <CheckCircle className="mr-1 h-4 w-4" />
            Approve
          </Button>
        </div>
      </div>
    </div>
  )
}

function RiskBadge({ level }: { level: string }) {
  switch (level) {
    case 'low':
      return <Badge variant="success" className="text-xs">Low</Badge>
    case 'medium':
      return <Badge variant="warning" className="text-xs">Medium</Badge>
    case 'high':
      return <Badge variant="danger" className="text-xs">High</Badge>
    case 'critical':
      return <Badge variant="danger" className="bg-red-700 text-xs">Critical</Badge>
    default:
      return <Badge className="text-xs">{level}</Badge>
  }
}

// Toast-like container for multiple notifications
interface PermissionNotificationStackProps {
  permissions: PermissionRequest[]
  onDismiss?: (id: string) => void
}

export function PermissionNotificationStack({
  permissions,
  onDismiss,
}: PermissionNotificationStackProps) {
  return (
    <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2">
      {permissions.map((permission, index) => (
        <div
          key={permission.id}
          style={{
            transform: `translateY(${index * -8}px)`,
            zIndex: permissions.length - index,
          }}
        >
          <PermissionNotification
            permission={permission}
            onDismiss={() => onDismiss?.(permission.id)}
          />
        </div>
      ))}
    </div>
  )
}

export default PermissionNotification
