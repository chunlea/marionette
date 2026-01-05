import { createFileRoute, Outlet, useNavigate, Link, useRouterState } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { isAuthenticated, clearApiKey } from '@/api/client'
import {
  LayoutDashboard,
  PlayCircle,
  ListTodo,
  Server,
  ShieldAlert,
  Settings,
  LogOut,
  Menu,
  X,
} from 'lucide-react'
import { usePendingPermissions } from '@/api/hooks'

export const Route = createFileRoute('/_layout')({
  component: DashboardLayout,
})

function DashboardLayout() {
  const navigate = useNavigate()
  const router = useRouterState()
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const { data: pendingPerms } = usePendingPermissions()

  const pendingCount = pendingPerms?.items?.length || 0

  useEffect(() => {
    // Redirect to login if not authenticated
    if (!isAuthenticated()) {
      navigate({ to: '/login' })
    }
  }, [navigate])

  // Don't render if not authenticated
  if (!isAuthenticated()) {
    return null
  }

  const handleLogout = () => {
    clearApiKey()
    navigate({ to: '/login' })
  }

  return (
    <div className="min-h-screen bg-gray-50">
      {/* Mobile sidebar backdrop */}
      {sidebarOpen && (
        <div
          className="fixed inset-0 z-40 bg-gray-900/50 lg:hidden"
          onClick={() => setSidebarOpen(false)}
        />
      )}

      {/* Sidebar */}
      <aside
        className={`fixed inset-y-0 left-0 z-50 w-64 transform border-r border-gray-200 bg-white transition-transform duration-200 ease-in-out lg:translate-x-0 ${
          sidebarOpen ? 'translate-x-0' : '-translate-x-full'
        }`}
      >
        <div className="flex h-full flex-col">
          {/* Logo */}
          <div className="flex h-16 items-center justify-between border-b border-gray-200 px-6">
            <Link to="/" className="flex items-center gap-2">
              <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary-600">
                <span className="font-bold text-white">M</span>
              </div>
              <span className="text-lg font-semibold text-gray-900">Marionette</span>
            </Link>
            <button
              className="p-2 text-gray-500 hover:text-gray-700 lg:hidden"
              onClick={() => setSidebarOpen(false)}
            >
              <X className="h-5 w-5" />
            </button>
          </div>

          {/* Navigation */}
          <nav className="flex-1 space-y-1 overflow-y-auto p-4">
            <NavItem to="/" icon={LayoutDashboard} label="Dashboard" exact />
            <NavItem to="/sessions" icon={PlayCircle} label="Sessions" />
            <NavItem to="/tasks" icon={ListTodo} label="Tasks" />
            <NavItem to="/runners" icon={Server} label="Runners" />
            <NavItem
              to="/permissions"
              icon={ShieldAlert}
              label="Permissions"
              badge={pendingCount > 0 ? pendingCount : undefined}
            />

            <div className="my-4 border-t border-gray-200" />

            <NavItem to="/admin/api-keys" icon={Settings} label="Admin" />
          </nav>

          {/* Footer */}
          <div className="border-t border-gray-200 p-4">
            <button
              onClick={handleLogout}
              className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-100"
            >
              <LogOut className="h-5 w-5 text-gray-400" />
              Sign out
            </button>
          </div>
        </div>
      </aside>

      {/* Main content */}
      <div className="lg:pl-64">
        {/* Top bar */}
        <header className="sticky top-0 z-30 flex h-16 items-center gap-4 border-b border-gray-200 bg-white px-4 sm:px-6">
          <button
            className="p-2 text-gray-500 hover:text-gray-700 lg:hidden"
            onClick={() => setSidebarOpen(true)}
          >
            <Menu className="h-5 w-5" />
          </button>
          <div className="flex-1" />
        </header>

        {/* Page content */}
        <main className="p-4 sm:p-6 lg:p-8">
          <Outlet />
        </main>
      </div>
    </div>
  )
}

interface NavItemProps {
  to: string
  icon: React.ComponentType<{ className?: string }>
  label: string
  exact?: boolean
  badge?: number
}

function NavItem({ to, icon: Icon, label, exact, badge }: NavItemProps) {
  const router = useRouterState()
  const isActive = exact
    ? router.location.pathname === to
    : router.location.pathname.startsWith(to)

  return (
    <Link
      to={to}
      className={`flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors ${
        isActive
          ? 'bg-primary-50 text-primary-700'
          : 'text-gray-700 hover:bg-gray-100 hover:text-gray-900'
      }`}
    >
      <Icon className={`h-5 w-5 ${isActive ? 'text-primary-600' : 'text-gray-400'}`} />
      <span className="flex-1">{label}</span>
      {badge !== undefined && (
        <span className="flex h-5 min-w-5 items-center justify-center rounded-full bg-red-100 px-1.5 text-xs font-medium text-red-700">
          {badge}
        </span>
      )}
    </Link>
  )
}
