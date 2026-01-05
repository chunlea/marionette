import { cn } from '@/lib/utils'

export interface BadgeProps {
  children: React.ReactNode
  variant?: 'default' | 'success' | 'warning' | 'danger' | 'info'
  className?: string
}

export function Badge({ children, variant = 'default', className }: BadgeProps) {
  const variants = {
    default: 'bg-gray-100 text-gray-800',
    success: 'bg-green-100 text-green-800',
    warning: 'bg-yellow-100 text-yellow-800',
    danger: 'bg-red-100 text-red-800',
    info: 'bg-blue-100 text-blue-800',
  }

  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium',
        variants[variant],
        className
      )}
    >
      {children}
    </span>
  )
}

// Status badge with predefined status mappings
export interface StatusBadgeProps {
  status: string
  className?: string
}

export function StatusBadge({ status, className }: StatusBadgeProps) {
  const getVariant = (): BadgeProps['variant'] => {
    switch (status.toLowerCase()) {
      case 'active':
      case 'completed':
      case 'approved':
      case 'idle':
        return 'success'
      case 'pending':
      case 'resuming':
      case 'paused':
        return 'warning'
      case 'running':
      case 'busy':
        return 'info'
      case 'failed':
      case 'denied':
      case 'terminated':
      case 'canceled':
      case 'offline':
        return 'danger'
      default:
        return 'default'
    }
  }

  return (
    <Badge variant={getVariant()} className={className}>
      {status}
    </Badge>
  )
}
