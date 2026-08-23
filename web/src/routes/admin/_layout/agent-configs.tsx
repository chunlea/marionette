import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { useAgentConfigs, useCreateAgentConfig, useDeleteAgentConfig } from '@/api/hooks'
import { Button } from '@/components/Button'
import { Input } from '@/components/Input'
import { Dialog, DialogHeader, DialogBody, DialogFooter } from '@/components/Dialog'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell, TableEmpty, TableLoading } from '@/components/Table'
import { Card } from '@/components/Card'
import { Badge } from '@/components/Badge'
import { formatRelativeTime } from '@/lib/utils'
import { Plus, Trash2 } from 'lucide-react'
import type { CreateAgentConfigRequest } from '@/types/admin'

export const Route = createFileRoute('/admin/_layout/agent-configs')({
  component: AgentConfigsPage,
})

function AgentConfigsPage() {
  const { data, isLoading } = useAgentConfigs()
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [showDeleteDialog, setShowDeleteDialog] = useState<string | null>(null)

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Agent Configurations</h1>
          <p className="mt-1 text-sm text-gray-600">
            Manage AI agent configurations (Claude, Codex, etc.)
          </p>
        </div>
        <Button onClick={() => setShowCreateDialog(true)}>
          <Plus className="mr-2 h-4 w-4" />
          Create Config
        </Button>
      </div>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Agent</TableHead>
              <TableHead>Model</TableHead>
              <TableHead>Base URL</TableHead>
              <TableHead>Default</TableHead>
              <TableHead>Created</TableHead>
              <TableHead className="w-24">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableLoading colSpan={7} />
            ) : !data?.items?.length ? (
              <TableEmpty colSpan={7} message="No agent configurations found" />
            ) : (
              data.items.map((config) => (
                <TableRow key={config.id}>
                  <TableCell className="font-medium">{config.name}</TableCell>
                  <TableCell>
                    <Badge variant="info">{config.agent}</Badge>
                  </TableCell>
                  <TableCell className="text-gray-500">
                    {config.model || <span className="text-gray-400">Default</span>}
                  </TableCell>
                  <TableCell className="text-gray-500">
                    {config.base_url ? (
                      <code className="rounded bg-gray-100 px-2 py-1 text-xs">
                        {config.base_url}
                      </code>
                    ) : (
                      <span className="text-gray-400">Default</span>
                    )}
                  </TableCell>
                  <TableCell>
                    {config.is_default && <Badge variant="success">Default</Badge>}
                  </TableCell>
                  <TableCell className="text-gray-500">
                    {formatRelativeTime(config.created_at)}
                  </TableCell>
                  <TableCell>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => setShowDeleteDialog(config.id)}
                    >
                      <Trash2 className="h-4 w-4 text-red-500" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </Card>

      {/* Create Dialog */}
      <CreateAgentConfigDialog
        open={showCreateDialog}
        onClose={() => setShowCreateDialog(false)}
      />

      {/* Delete Dialog */}
      <DeleteAgentConfigDialog
        configId={showDeleteDialog}
        onClose={() => setShowDeleteDialog(null)}
      />
    </div>
  )
}

interface CreateAgentConfigDialogProps {
  open: boolean
  onClose: () => void
}

function CreateAgentConfigDialog({ open, onClose }: CreateAgentConfigDialogProps) {
  const createConfig = useCreateAgentConfig()
  const [formData, setFormData] = useState({
    name: '',
    agent: 'claude',
    api_key: '',
    model: '',
    base_url: '',
    is_default: false,
  })

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    const request: CreateAgentConfigRequest = {
      name: formData.name,
      agent: formData.agent,
      api_key: formData.api_key,
      model: formData.model || undefined,
      base_url: formData.base_url || undefined,
      is_default: formData.is_default,
    }

    try {
      await createConfig.mutateAsync(request)
      handleClose()
    } catch (error) {
      console.error('Failed to create agent config:', error)
    }
  }

  const handleClose = () => {
    setFormData({
      name: '',
      agent: 'claude',
      api_key: '',
      model: '',
      base_url: '',
      is_default: false,
    })
    onClose()
  }

  return (
    <Dialog open={open} onClose={handleClose}>
      <DialogHeader onClose={handleClose}>Create Agent Configuration</DialogHeader>
      <form onSubmit={handleSubmit}>
        <DialogBody className="space-y-4">
          <Input
            label="Name"
            value={formData.name}
            onChange={(e) => setFormData({ ...formData, name: e.target.value })}
            placeholder="production-claude"
            required
          />

          <div className="space-y-1">
            <label className="block text-sm font-medium text-gray-700">Agent Type</label>
            <select
              value={formData.agent}
              onChange={(e) => setFormData({ ...formData, agent: e.target.value })}
              className="block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500"
            >
              <option value="claude">Claude</option>
              <option value="codex">Codex</option>
              <option value="other">Other</option>
            </select>
          </div>

          <Input
            label="API Key"
            type="password"
            value={formData.api_key}
            onChange={(e) => setFormData({ ...formData, api_key: e.target.value })}
            placeholder="sk-ant-..."
            required
          />

          <Input
            label="Model (optional)"
            value={formData.model}
            onChange={(e) => setFormData({ ...formData, model: e.target.value })}
            placeholder="claude-3-opus"
            helperText="Leave empty to use the agent's default model"
          />

          <Input
            label="Base URL (optional)"
            value={formData.base_url}
            onChange={(e) => setFormData({ ...formData, base_url: e.target.value })}
            placeholder="https://api.anthropic.com"
            helperText="Custom API endpoint. Leave empty for default."
          />

          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              id="is_default"
              checked={formData.is_default}
              onChange={(e) => setFormData({ ...formData, is_default: e.target.checked })}
              className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
            />
            <label htmlFor="is_default" className="text-sm text-gray-700">
              Set as default for this agent type
            </label>
          </div>
        </DialogBody>
        <DialogFooter>
          <Button variant="secondary" type="button" onClick={handleClose}>
            Cancel
          </Button>
          <Button type="submit" loading={createConfig.isPending}>
            Create Config
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  )
}

interface DeleteAgentConfigDialogProps {
  configId: string | null
  onClose: () => void
}

function DeleteAgentConfigDialog({ configId, onClose }: DeleteAgentConfigDialogProps) {
  const deleteConfig = useDeleteAgentConfig()

  const handleDelete = async () => {
    if (!configId) return
    try {
      await deleteConfig.mutateAsync(configId)
      onClose()
    } catch (error) {
      console.error('Failed to delete agent config:', error)
    }
  }

  return (
    <Dialog open={!!configId} onClose={onClose}>
      <DialogHeader onClose={onClose}>Delete Agent Configuration</DialogHeader>
      <DialogBody>
        <p className="text-sm text-gray-600">
          Are you sure you want to delete this agent configuration? Sessions using this
          configuration will fail to start.
        </p>
      </DialogBody>
      <DialogFooter>
        <Button variant="secondary" onClick={onClose}>
          Cancel
        </Button>
        <Button variant="danger" onClick={handleDelete} loading={deleteConfig.isPending}>
          Delete Config
        </Button>
      </DialogFooter>
    </Dialog>
  )
}
