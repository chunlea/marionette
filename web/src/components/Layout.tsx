import { Link, useRouterState } from '@tanstack/react-router'
import {
  LayoutDashboard,
  PlayCircle,
  ListTodo,
  Server,
  ShieldAlert,
  Settings,
  Key,
  Cpu,
  Network,
  Menu,
  X,
} from 'lucide-react'
import { useState } from 'react'

interface LayoutProps {
  children: React.ReactNode
}

export function Layout({ children }: LayoutProps) {
  const [sidebarOpen, setSidebarOpen] = useState(false)

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
        className={`fixed inset-y-0 left-0 z-50 w-64 transform bg-white border-r border-gray-200 transition-transform duration-200 ease-in-out lg:translate-x-0 ${
          sidebarOpen ? 'translate-x-0' : '-translate-x-full'
        }`}
      >
        <div className="flex h-full flex-col">
          {/* Logo */}
          <div className="flex h-16 items-center justify-between px-4 border-b border-gray-200">
            <Link to="/" className="flex items-center space-x-2">
              <div className="h-8 w-8 rounded-lg bg-primary-600 flex items-center justify-center">
                <span className="text-white font-bold text-lg">M</span>
              </div>
              <span className="text-lg font-semibold text-gray-900">Marionette</span>
            </Link>
            <button
              className="lg:hidden p-2 text-gray-500 hover:text-gray-700"
              onClick={() => setSidebarOpen(false)}
            >
              <X className="h-5 w-5" />
            </button>
          </div>

          {/* Navigation */}
          <nav className="flex-1 overflow-y-auto p-4 space-y-1">
            <NavSection title="Dashboard">
              <NavItem to="/" icon={LayoutDashboard} label="Overview" />
              <NavItem to="/sessions" icon={PlayCircle} label="Sessions" />
              <NavItem to="/tasks" icon={ListTodo} label="Tasks" />
              <NavItem to="/runners" icon={Server} label="Runners" />
              <NavItem to="/permissions" icon={ShieldAlert} label="Permissions" />
            </NavSection>

            <NavSection title="Admin">
              <NavItem to="/admin/api-keys" icon={Key} label="API Keys" />
              <NavItem to="/admin/agent-configs" icon={Cpu} label="Agent Configs" />
              <NavItem to="/admin/provider-configs" icon={Network} label="Provider Configs" />
              <NavItem to="/admin/settings" icon={Settings} label="Settings" />
            </NavSection>
          </nav>

          {/* Footer */}
          <div className="border-t border-gray-200 p-4">
            <div className="text-xs text-gray-500">
              <p>Marionette v0.1.0</p>
            </div>
          </div>
        </div>
      </aside>

      {/* Main content */}
      <div className="lg:pl-64">
        {/* Top bar */}
        <header className="sticky top-0 z-30 flex h-16 items-center gap-4 border-b border-gray-200 bg-white px-4 sm:px-6">
          <button
            className="lg:hidden p-2 text-gray-500 hover:text-gray-700"
            onClick={() => setSidebarOpen(true)}
          >
            <Menu className="h-5 w-5" />
          </button>
          <div className="flex-1" />
          {/* Add user menu, notifications, etc. here */}
        </header>

        {/* Page content */}
        <main className="p-4 sm:p-6 lg:p-8">{children}</main>
      </div>
    </div>
  )
}

interface NavSectionProps {
  title: string
  children: React.ReactNode
}

function NavSection({ title, children }: NavSectionProps) {
  return (
    <div className="space-y-1">
      <h3 className="px-3 text-xs font-semibold uppercase tracking-wider text-gray-500">
        {title}
      </h3>
      {children}
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
