import { useState, useCallback } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { useApiKeys, useCreateApiKey, useRevokeApiKey } from '@/api/hooks'
import { Button } from '@/components/Button'
import { Input } from '@/components/Input'
import { Dialog, DialogHeader, DialogBody, DialogFooter } from '@/components/Dialog'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell, TableEmpty, TableLoading } from '@/components/Table'
import { Card } from '@/components/Card'
import { Badge } from '@/components/Badge'
import { formatRelativeTime, copyToClipboard } from '@/lib/utils'
import { Plus, Copy, Check, Trash2 } from 'lucide-react'
import type { CreateAPIKeyRequest } from '@/types/api'

// Available scopes organized by resource
const AVAILABLE_SCOPES = {
  sessions: {
    label: 'Sessions',
    scopes: [
      { value: 'sessions:read', label: 'Read', description: 'List and view sessions' },
      { value: 'sessions:write', label: 'Write', description: 'Create, suspend, resume, terminate sessions' },
    ],
  },
  tasks: {
    label: 'Tasks',
    scopes: [
      { value: 'tasks:read', label: 'Read', description: 'List, view tasks and logs' },
      { value: 'tasks:write', label: 'Write', description: 'Create, execute, cancel, retry tasks' },
    ],
  },
  runners: {
    label: 'Runners',
    scopes: [
      { value: 'runners:read', label: 'Read', description: 'List and view runners' },
    ],
  },
  permissions: {
    label: 'Permissions',
    scopes: [
      { value: 'permissions:read', label: 'Read', description: 'List and view permission requests' },
      { value: 'permissions:write', label: 'Write', description: 'Approve or deny permission requests' },
    ],
  },
} as const

export const Route = createFileRoute('/admin/_layout/api-keys')({
  component: ApiKeysPage,
})

function ApiKeysPage() {
  const { data, isLoading } = useApiKeys()
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [showRevokeDialog, setShowRevokeDialog] = useState<string | null>(null)

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">API Keys</h1>
          <p className="mt-1 text-sm text-gray-600">
            Manage API keys for accessing the Marionette API
          </p>
        </div>
        <Button onClick={() => setShowCreateDialog(true)}>
          <Plus className="mr-2 h-4 w-4" />
          Create Key
        </Button>
      </div>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Key Prefix</TableHead>
              <TableHead>Scopes</TableHead>
              <TableHead>Last Used</TableHead>
              <TableHead>Created</TableHead>
              <TableHead>Status</TableHead>
              <TableHead className="w-24">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableLoading colSpan={7} />
            ) : !data?.items?.length ? (
              <TableEmpty colSpan={7} message="No API keys found" />
            ) : (
              data.items.map((key) => (
                <TableRow key={key.id}>
                  <TableCell className="font-medium">{key.name}</TableCell>
                  <TableCell>
                    <code className="rounded bg-gray-100 px-2 py-1 text-xs">
                      {key.key_prefix}...
                    </code>
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1">
                      {key.scopes?.length ? (
                        key.scopes.slice(0, 3).map((scope) => (
                          <Badge key={scope} variant="info">
                            {scope}
                          </Badge>
                        ))
                      ) : (
                        <span className="text-gray-400">All</span>
                      )}
                      {key.scopes?.length > 3 && (
                        <Badge>+{key.scopes.length - 3}</Badge>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className="text-gray-500">
                    {formatRelativeTime(key.last_used_at)}
                  </TableCell>
                  <TableCell className="text-gray-500">
                    {formatRelativeTime(key.created_at)}
                  </TableCell>
                  <TableCell>
                    {key.revoked_at ? (
                      <Badge variant="danger">Revoked</Badge>
                    ) : (
                      <Badge variant="success">Active</Badge>
                    )}
                  </TableCell>
                  <TableCell>
                    {!key.revoked_at && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => setShowRevokeDialog(key.id)}
                      >
                        <Trash2 className="h-4 w-4 text-red-500" />
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </Card>

      {/* Create Dialog */}
      <CreateApiKeyDialog
        open={showCreateDialog}
        onClose={() => setShowCreateDialog(false)}
      />

      {/* Revoke Dialog */}
      <RevokeApiKeyDialog
        keyId={showRevokeDialog}
        onClose={() => setShowRevokeDialog(null)}
      />
    </div>
  )
}

interface CreateApiKeyDialogProps {
  open: boolean
  onClose: () => void
}

function CreateApiKeyDialog({ open, onClose }: CreateApiKeyDialogProps) {
  const createApiKey = useCreateApiKey()
  const [name, setName] = useState('')
  const [selectedScopes, setSelectedScopes] = useState<Set<string>>(new Set())
  const [fullAccess, setFullAccess] = useState(true)
  const [createdKey, setCreatedKey] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const toggleScope = useCallback((scope: string) => {
    setSelectedScopes((prev) => {
      const next = new Set(prev)
      if (next.has(scope)) {
        next.delete(scope)
      } else {
        next.add(scope)
      }
      return next
    })
  }, [])

  const toggleResourceAll = useCallback((resource: keyof typeof AVAILABLE_SCOPES) => {
    const resourceScopes = AVAILABLE_SCOPES[resource].scopes.map((s) => s.value)
    setSelectedScopes((prev) => {
      const next = new Set(prev)
      const allSelected = resourceScopes.every((s) => prev.has(s))
      if (allSelected) {
        resourceScopes.forEach((s) => next.delete(s))
      } else {
        resourceScopes.forEach((s) => next.add(s))
      }
      return next
    })
  }, [])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    const request: CreateAPIKeyRequest = {
      name,
      scopes: fullAccess ? undefined : Array.from(selectedScopes),
    }

    try {
      const result = await createApiKey.mutateAsync(request)
      setCreatedKey(result.key)
    } catch (error) {
      console.error('Failed to create API key:', error)
    }
  }

  const handleCopy = async () => {
    if (createdKey) {
      const success = await copyToClipboard(createdKey)
      if (success) {
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
      }
    }
  }

  const handleClose = () => {
    setName('')
    setSelectedScopes(new Set())
    setFullAccess(true)
    setCreatedKey(null)
    setCopied(false)
    onClose()
  }

  return (
    <Dialog open={open} onClose={handleClose}>
      <DialogHeader onClose={handleClose}>
        {createdKey ? 'API Key Created' : 'Create API Key'}
      </DialogHeader>

      {createdKey ? (
        <>
          <DialogBody>
            <div className="space-y-4">
              <div className="rounded-lg bg-yellow-50 p-4">
                <p className="text-sm text-yellow-800">
                  Make sure to copy your API key now. You won't be able to see it again!
                </p>
              </div>
              <div className="flex items-center gap-2">
                <code className="flex-1 rounded-lg bg-gray-100 p-3 font-mono text-sm break-all">
                  {createdKey}
                </code>
                <Button variant="secondary" size="sm" onClick={handleCopy}>
                  {copied ? (
                    <Check className="h-4 w-4 text-green-500" />
                  ) : (
                    <Copy className="h-4 w-4" />
                  )}
                </Button>
              </div>
            </div>
          </DialogBody>
          <DialogFooter>
            <Button onClick={handleClose}>Done</Button>
          </DialogFooter>
        </>
      ) : (
        <form onSubmit={handleSubmit}>
          <DialogBody className="space-y-4">
            <Input
              label="Name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="My API Key"
              required
            />

            {/* Permissions Section */}
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-2">
                Permissions
              </label>

              {/* Full Access Toggle */}
              <label className="flex items-center gap-2 p-3 rounded-lg border border-gray-200 bg-gray-50 cursor-pointer hover:bg-gray-100 mb-3">
                <input
                  type="checkbox"
                  checked={fullAccess}
                  onChange={(e) => setFullAccess(e.target.checked)}
                  className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                />
                <div>
                  <span className="font-medium text-gray-900">Full Access</span>
                  <p className="text-xs text-gray-500">Grant access to all current and future API endpoints</p>
                </div>
              </label>

              {/* Scope Selection */}
              {!fullAccess && (
                <div className="space-y-3 border rounded-lg p-3 bg-white">
                  {(Object.keys(AVAILABLE_SCOPES) as Array<keyof typeof AVAILABLE_SCOPES>).map((resource) => {
                    const { label, scopes } = AVAILABLE_SCOPES[resource]
                    const allSelected = scopes.every((s) => selectedScopes.has(s.value))
                    const someSelected = scopes.some((s) => selectedScopes.has(s.value))

                    return (
                      <div key={resource} className="space-y-1">
                        <label className="flex items-center gap-2 cursor-pointer">
                          <input
                            type="checkbox"
                            checked={allSelected}
                            ref={(el) => {
                              if (el) el.indeterminate = someSelected && !allSelected
                            }}
                            onChange={() => toggleResourceAll(resource)}
                            className="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                          />
                          <span className="font-medium text-gray-900">{label}</span>
                        </label>
                        <div className="ml-6 space-y-1">
                          {scopes.map((scope) => (
                            <label
                              key={scope.value}
                              className="flex items-center gap-2 cursor-pointer text-sm"
                            >
                              <input
                                type="checkbox"
                                checked={selectedScopes.has(scope.value)}
                                onChange={() => toggleScope(scope.value)}
                                className="h-3.5 w-3.5 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                              />
                              <span className="text-gray-700">{scope.label}</span>
                              <span className="text-gray-400">- {scope.description}</span>
                            </label>
                          ))}
                        </div>
                      </div>
                    )
                  })}

                  {selectedScopes.size === 0 && (
                    <p className="text-sm text-amber-600 mt-2">
                      Select at least one permission or enable Full Access
                    </p>
                  )}
                </div>
              )}
            </div>
          </DialogBody>
          <DialogFooter>
            <Button variant="secondary" type="button" onClick={handleClose}>
              Cancel
            </Button>
            <Button
              type="submit"
              loading={createApiKey.isPending}
              disabled={!fullAccess && selectedScopes.size === 0}
            >
              Create Key
            </Button>
          </DialogFooter>
        </form>
      )}
    </Dialog>
  )
}

interface RevokeApiKeyDialogProps {
  keyId: string | null
  onClose: () => void
}

function RevokeApiKeyDialog({ keyId, onClose }: RevokeApiKeyDialogProps) {
  const revokeApiKey = useRevokeApiKey()
  const [reason, setReason] = useState('')

  const handleRevoke = async () => {
    if (!keyId) return
    try {
      await revokeApiKey.mutateAsync({ keyId, reason: reason || undefined })
      onClose()
      setReason('')
    } catch (error) {
      console.error('Failed to revoke API key:', error)
    }
  }

  return (
    <Dialog open={!!keyId} onClose={onClose}>
      <DialogHeader onClose={onClose}>Revoke API Key</DialogHeader>
      <DialogBody className="space-y-4">
        <p className="text-sm text-gray-600">
          Are you sure you want to revoke this API key? This action cannot be undone.
        </p>
        <Input
          label="Reason (optional)"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          placeholder="Enter a reason for revoking"
        />
      </DialogBody>
      <DialogFooter>
        <Button variant="secondary" onClick={onClose}>
          Cancel
        </Button>
        <Button variant="danger" onClick={handleRevoke} loading={revokeApiKey.isPending}>
          Revoke Key
        </Button>
      </DialogFooter>
    </Dialog>
  )
}
