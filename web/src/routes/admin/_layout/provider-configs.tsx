import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { useProviderConfigs, useCreateProviderConfig, useDeleteProviderConfig } from '@/api/hooks'
import { Button } from '@/components/Button'
import { Input, Textarea } from '@/components/Input'
import { Dialog, DialogHeader, DialogBody, DialogFooter } from '@/components/Dialog'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell, TableEmpty, TableLoading } from '@/components/Table'
import { Card } from '@/components/Card'
import { Badge } from '@/components/Badge'
import { formatRelativeTime } from '@/lib/utils'
import { Plus, Trash2 } from 'lucide-react'
import type { CreateProviderConfigRequest } from '@/types/api'

export const Route = createFileRoute('/admin/_layout/provider-configs')({
  component: ProviderConfigsPage,
})

function ProviderConfigsPage() {
  const { data, isLoading } = useProviderConfigs()
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [showDeleteDialog, setShowDeleteDialog] = useState<string | null>(null)

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Provider Configurations</h1>
          <p className="mt-1 text-sm text-gray-600">
            Manage runner provider configurations (Docker, K8s, E2B, etc.)
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
              <TableHead>Provider</TableHead>
              <TableHead>Suspend Strategy</TableHead>
              <TableHead>Default</TableHead>
              <TableHead>Created</TableHead>
              <TableHead className="w-24">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableLoading colSpan={6} />
            ) : !data?.items?.length ? (
              <TableEmpty colSpan={6} message="No provider configurations found" />
            ) : (
              data.items.map((config) => (
                <TableRow key={config.id}>
                  <TableCell className="font-medium">{config.name}</TableCell>
                  <TableCell>
                    <Badge variant="info">{config.provider}</Badge>
                  </TableCell>
                  <TableCell className="text-gray-500">
                    {(config.suspend_config as { strategy?: string })?.strategy || 'terminate'}
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
      <CreateProviderConfigDialog
        open={showCreateDialog}
        onClose={() => setShowCreateDialog(false)}
      />

      {/* Delete Dialog */}
      <DeleteProviderConfigDialog
        configId={showDeleteDialog}
        onClose={() => setShowDeleteDialog(null)}
      />
    </div>
  )
}

interface CreateProviderConfigDialogProps {
  open: boolean
  onClose: () => void
}

function CreateProviderConfigDialog({ open, onClose }: CreateProviderConfigDialogProps) {
  const createConfig = useCreateProviderConfig()
  const [formData, setFormData] = useState({
    name: '',
    provider: 'docker',
    config: '{}',
    suspend_strategy: 'terminate',
    is_default: false,
  })
  const [configError, setConfigError] = useState('')

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setConfigError('')

    let parsedConfig = {}
    try {
      parsedConfig = JSON.parse(formData.config)
    } catch {
      setConfigError('Invalid JSON configuration')
      return
    }

    const request: CreateProviderConfigRequest = {
      name: formData.name,
      provider: formData.provider,
      config: parsedConfig,
      suspend_config: { strategy: formData.suspend_strategy },
      is_default: formData.is_default,
    }

    try {
      await createConfig.mutateAsync(request)
      handleClose()
    } catch (error) {
      console.error('Failed to create provider config:', error)
    }
  }

  const handleClose = () => {
    setFormData({
      name: '',
      provider: 'docker',
      config: '{}',
      suspend_strategy: 'terminate',
      is_default: false,
    })
    setConfigError('')
    onClose()
  }

  return (
    <Dialog open={open} onClose={handleClose} className="max-w-2xl">
      <DialogHeader onClose={handleClose}>Create Provider Configuration</DialogHeader>
      <form onSubmit={handleSubmit}>
        <DialogBody className="space-y-4">
          <Input
            label="Name"
            value={formData.name}
            onChange={(e) => setFormData({ ...formData, name: e.target.value })}
            placeholder="docker-local"
            required
          />

          <div className="space-y-1">
            <label className="block text-sm font-medium text-gray-700">Provider</label>
            <select
              value={formData.provider}
              onChange={(e) => setFormData({ ...formData, provider: e.target.value })}
              className="block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500"
            >
              <option value="docker">Docker</option>
              <option value="kubernetes">Kubernetes</option>
              <option value="e2b">E2B</option>
              <option value="firecracker">Firecracker</option>
              <option value="pool">Pool</option>
            </select>
          </div>

          <Textarea
            label="Configuration (JSON)"
            value={formData.config}
            onChange={(e) => setFormData({ ...formData, config: e.target.value })}
            error={configError}
            placeholder='{"image": "ghcr.io/chunlea/marionette-runner:latest"}'
            rows={6}
            className="font-mono text-sm"
          />

          <div className="space-y-1">
            <label className="block text-sm font-medium text-gray-700">Suspend Strategy</label>
            <select
              value={formData.suspend_strategy}
              onChange={(e) => setFormData({ ...formData, suspend_strategy: e.target.value })}
              className="block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500"
            >
              <option value="terminate">Terminate</option>
              <option value="pause">Pause (memory preserved)</option>
              <option value="snapshot">Snapshot</option>
              <option value="terminate_preserve_storage">Terminate (preserve storage)</option>
              <option value="release_to_pool">Release to pool</option>
            </select>
          </div>

          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              id="is_default"
              checked={formData.is_default}
              onChange={(e) => setFormData({ ...formData, is_default: e.target.checked })}
              className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
            />
            <label htmlFor="is_default" className="text-sm text-gray-700">
              Set as default for this provider type
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

interface DeleteProviderConfigDialogProps {
  configId: string | null
  onClose: () => void
}

function DeleteProviderConfigDialog({ configId, onClose }: DeleteProviderConfigDialogProps) {
  const deleteConfig = useDeleteProviderConfig()

  const handleDelete = async () => {
    if (!configId) return
    try {
      await deleteConfig.mutateAsync(configId)
      onClose()
    } catch (error) {
      console.error('Failed to delete provider config:', error)
    }
  }

  return (
    <Dialog open={!!configId} onClose={onClose}>
      <DialogHeader onClose={onClose}>Delete Provider Configuration</DialogHeader>
      <DialogBody>
        <p className="text-sm text-gray-600">
          Are you sure you want to delete this provider configuration? Sessions using this
          provider will not be able to spawn new runners.
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
