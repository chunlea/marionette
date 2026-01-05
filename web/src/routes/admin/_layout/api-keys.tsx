import { useState } from 'react'
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
  const [scopes, setScopes] = useState('')
  const [createdKey, setCreatedKey] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    const request: CreateAPIKeyRequest = {
      name,
      scopes: scopes ? scopes.split(',').map((s) => s.trim()) : undefined,
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
    setScopes('')
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
            <Input
              label="Scopes (optional)"
              value={scopes}
              onChange={(e) => setScopes(e.target.value)}
              placeholder="sessions:*, tasks:read"
              helperText="Comma-separated list of scopes. Leave empty for full access."
            />
          </DialogBody>
          <DialogFooter>
            <Button variant="secondary" type="button" onClick={handleClose}>
              Cancel
            </Button>
            <Button type="submit" loading={createApiKey.isPending}>
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
