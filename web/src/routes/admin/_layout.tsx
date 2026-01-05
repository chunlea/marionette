import { createFileRoute, Outlet, useNavigate, Link, useRouterState } from '@tanstack/react-router'
import { useEffect } from 'react'
import { isAdminAuthenticated, clearAdminCredentials } from '@/api/admin'
import { Key, Cpu, Network, LogOut, Settings } from 'lucide-react'

export const Route = createFileRoute('/admin/_layout')({
  component: AdminLayout,
})

function AdminLayout() {
  const navigate = useNavigate()
  const router = useRouterState()
  const isLoginPage = router.location.pathname === '/admin/login'

  useEffect(() => {
    // Skip auth check on login page
    if (isLoginPage) return

    // Redirect to login if not authenticated
    if (!isAdminAuthenticated()) {
      navigate({ to: '/admin/login' })
    }
  }, [navigate, isLoginPage])

  // Don't show layout for login page
  if (isLoginPage) {
    return <Outlet />
  }

  // Don't render protected content if not authenticated
  if (!isAdminAuthenticated()) {
    return null
  }

  const handleLogout = () => {
    clearAdminCredentials()
    navigate({ to: '/admin/login' })
  }

  return (
    <div className="min-h-screen bg-gray-50">
      {/* Sidebar */}
      <aside className="fixed inset-y-0 left-0 w-64 border-r border-gray-200 bg-white">
        <div className="flex h-full flex-col">
          {/* Logo */}
          <div className="flex h-16 items-center border-b border-gray-200 px-6">
            <Link to="/" className="flex items-center gap-2">
              <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary-600">
                <span className="font-bold text-white">M</span>
              </div>
              <span className="text-lg font-semibold text-gray-900">Admin</span>
            </Link>
          </div>

          {/* Navigation */}
          <nav className="flex-1 space-y-1 p-4">
            <NavItem to="/admin/api-keys" icon={Key} label="API Keys" />
            <NavItem to="/admin/agent-configs" icon={Cpu} label="Agent Configs" />
            <NavItem to="/admin/provider-configs" icon={Network} label="Provider Configs" />
            <NavItem to="/admin/settings" icon={Settings} label="Settings" />
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
      <main className="pl-64">
        <div className="p-8">
          <Outlet />
        </div>
      </main>
    </div>
  )
}

interface NavItemProps {
  to: string
  icon: React.ComponentType<{ className?: string }>
  label: string
}

function NavItem({ to, icon: Icon, label }: NavItemProps) {
  const router = useRouterState()
  const isActive = router.location.pathname === to

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
      {label}
    </Link>
  )
}
