import { formatDistanceToNow, format, parseISO } from 'date-fns'
import type { SessionStatus, TaskStatus, RunnerStatus, RiskLevel, PermissionStatus } from '@/types/api'

// Class name utility (like clsx)
export function cn(...classes: (string | undefined | null | false)[]): string {
  return classes.filter(Boolean).join(' ')
}

// Date formatting
export function formatRelativeTime(dateString: string | undefined | null): string {
  if (!dateString) return 'Never'
  try {
    return formatDistanceToNow(parseISO(dateString), { addSuffix: true })
  } catch {
    return 'Invalid date'
  }
}

export function formatDateTime(dateString: string | undefined | null): string {
  if (!dateString) return 'N/A'
  try {
    return format(parseISO(dateString), 'PPpp')
  } catch {
    return 'Invalid date'
  }
}

export function formatDate(dateString: string | undefined | null): string {
  if (!dateString) return 'N/A'
  try {
    return format(parseISO(dateString), 'PP')
  } catch {
    return 'Invalid date'
  }
}

// Status colors
export function getSessionStatusColor(status: SessionStatus): string {
  switch (status) {
    case 'active':
      return 'bg-green-100 text-green-800'
    case 'pending':
      return 'bg-yellow-100 text-yellow-800'
    case 'suspended':
      return 'bg-gray-100 text-gray-800'
    case 'resuming':
      return 'bg-blue-100 text-blue-800'
    case 'terminated':
      return 'bg-red-100 text-red-800'
    default:
      return 'bg-gray-100 text-gray-800'
  }
}

export function getTaskStatusColor(status: TaskStatus): string {
  switch (status) {
    case 'running':
      return 'bg-blue-100 text-blue-800'
    case 'pending':
      return 'bg-yellow-100 text-yellow-800'
    case 'completed':
      return 'bg-green-100 text-green-800'
    case 'failed':
      return 'bg-red-100 text-red-800'
    case 'canceled':
      return 'bg-gray-100 text-gray-800'
    default:
      return 'bg-gray-100 text-gray-800'
  }
}

export function getRunnerStatusColor(status: RunnerStatus): string {
  switch (status) {
    case 'idle':
      return 'bg-green-100 text-green-800'
    case 'busy':
      return 'bg-blue-100 text-blue-800'
    case 'paused':
      return 'bg-yellow-100 text-yellow-800'
    case 'offline':
      return 'bg-gray-100 text-gray-800'
    default:
      return 'bg-gray-100 text-gray-800'
  }
}

export function getRiskLevelColor(level: RiskLevel): string {
  switch (level) {
    case 'low':
      return 'bg-green-100 text-green-800'
    case 'medium':
      return 'bg-yellow-100 text-yellow-800'
    case 'high':
      return 'bg-orange-100 text-orange-800'
    case 'critical':
      return 'bg-red-100 text-red-800'
    default:
      return 'bg-gray-100 text-gray-800'
  }
}

export function getPermissionStatusColor(status: PermissionStatus): string {
  switch (status) {
    case 'pending':
      return 'bg-yellow-100 text-yellow-800'
    case 'approved':
      return 'bg-green-100 text-green-800'
    case 'denied':
      return 'bg-red-100 text-red-800'
    case 'canceled':
      return 'bg-gray-100 text-gray-800'
    default:
      return 'bg-gray-100 text-gray-800'
  }
}

// ID utilities
export function truncateId(id: string | undefined | null, length = 12): string {
  if (!id) return ''
  if (id.length <= length) return id
  return `${id.slice(0, length)}...`
}

// Mask sensitive values
export function maskValue(value: string, visibleChars = 4): string {
  if (value.length <= visibleChars) return '*'.repeat(value.length)
  return value.slice(0, visibleChars) + '*'.repeat(Math.min(8, value.length - visibleChars))
}

// Copy to clipboard with fallback
export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    // Fallback for older browsers
    const textArea = document.createElement('textarea')
    textArea.value = text
    textArea.style.position = 'fixed'
    textArea.style.left = '-999999px'
    document.body.appendChild(textArea)
    textArea.select()
    try {
      document.execCommand('copy')
      return true
    } catch {
      return false
    } finally {
      document.body.removeChild(textArea)
    }
  }
}

// Pluralize helper
export function pluralize(count: number, singular: string, plural?: string): string {
  return count === 1 ? singular : (plural || `${singular}s`)
}
