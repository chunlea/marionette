import { useState, useCallback } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import {
  useRunnerTokens,
  useCreateRunnerToken,
  useRevokeRunnerToken,
  useRotateRunnerToken,
} from '@/api/hooks'
import { Button } from '@/components/Button'
import { Input } from '@/components/Input'
import {
  Dialog,
  DialogHeader,
  DialogBody,
  DialogFooter,
} from '@/components/Dialog'
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
  TableEmpty,
  TableLoading,
} from '@/components/Table'
import { Card } from '@/components/Card'
import { Badge } from '@/components/Badge'
import { formatRelativeTime, copyToClipboard } from '@/lib/utils'
import { Plus, Copy, Check, Trash2, RefreshCw } from 'lucide-react'
import type { CreateRunnerTokenRequest, RunnerTokenStatus } from '@/types/api'

export const Route = createFileRoute('/admin/_layout/runner-tokens')({
  component: RunnerTokensPage,
})

function getStatusBadgeVariant(
  status: RunnerTokenStatus
): 'success' | 'warning' | 'danger' | 'default' {
  switch (status) {
    case 'active':
      return 'success'
    case 'rotating':
      return 'warning'
    case 'revoked':
      return 'danger'
    case 'expired':
      return 'default'
    default:
      return 'default'
  }
}

function RunnerTokensPage() {
  const { data, isLoading, refetch } = useRunnerTokens({ include_revoked: true })
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [showRevokeDialog, setShowRevokeDialog] = useState<string | null>(null)
  const [showRotateDialog, setShowRotateDialog] = useState<string | null>(null)

  const handleCreateDialogClose = () => {
    setShowCreateDialog(false)
    refetch()
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Runner Tokens</h1>
          <p className="mt-1 text-sm text-gray-600">
            Manage authentication tokens for runner agents
          </p>
        </div>
        <Button onClick={() => setShowCreateDialog(true)}>
          <Plus className="mr-2 h-4 w-4" />
          Create Token
        </Button>
      </div>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>ID</TableHead>
              <TableHead>Pool</TableHead>
              <TableHead>Token Prefix</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Last Used</TableHead>
              <TableHead>Created</TableHead>
              <TableHead className="w-32">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableLoading colSpan={7} />
            ) : !data?.items?.length ? (
              <TableEmpty colSpan={7} message="No runner tokens found" />
            ) : (
              data.items.map((token) => (
                <TableRow key={token.id}>
                  <TableCell className="font-mono text-xs">{token.id}</TableCell>
                  <TableCell>{token.pool_name}</TableCell>
                  <TableCell>
                    <code className="rounded bg-gray-100 px-2 py-1 text-xs">
                      {token.token_prefix}...
                    </code>
                  </TableCell>
                  <TableCell>
                    <Badge variant={getStatusBadgeVariant(token.status)}>
                      {token.status}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-gray-500">
                    {formatRelativeTime(token.last_used_at)}
                  </TableCell>
                  <TableCell className="text-gray-500">
                    {formatRelativeTime(token.created_at)}
                  </TableCell>
                  <TableCell>
                    <div className="flex gap-1">
                      {token.status === 'active' && (
                        <>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => setShowRotateDialog(token.id)}
                            title="Rotate token"
                          >
                            <RefreshCw className="h-4 w-4 text-blue-500" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => setShowRevokeDialog(token.id)}
                            title="Revoke token"
                          >
                            <Trash2 className="h-4 w-4 text-red-500" />
                          </Button>
                        </>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </Card>

      {/* Create Dialog */}
      <CreateRunnerTokenDialog
        open={showCreateDialog}
        onClose={handleCreateDialogClose}
      />

      {/* Revoke Dialog */}
      <RevokeRunnerTokenDialog
        tokenId={showRevokeDialog}
        onClose={() => setShowRevokeDialog(null)}
      />

      {/* Rotate Dialog */}
      <RotateRunnerTokenDialog
        tokenId={showRotateDialog}
        onClose={() => setShowRotateDialog(null)}
      />
    </div>
  )
}

interface CreateRunnerTokenDialogProps {
  open: boolean
  onClose: () => void
}

function CreateRunnerTokenDialog({ open, onClose }: CreateRunnerTokenDialogProps) {
  const createRunnerToken = useCreateRunnerToken()
  const [poolName, setPoolName] = useState('')
  const [createdToken, setCreatedToken] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    const request: CreateRunnerTokenRequest = {
      pool_name: poolName,
    }

    try {
      const result = await createRunnerToken.mutateAsync(request)
      setCreatedToken(result.raw_token)
    } catch (error) {
      console.error('Failed to create runner token:', error)
    }
  }

  const handleCopy = async () => {
    if (createdToken) {
      const success = await copyToClipboard(createdToken)
      if (success) {
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
      }
    }
  }

  const handleClose = () => {
    setPoolName('')
    setCreatedToken(null)
    setCopied(false)
    onClose()
  }

  return (
    <Dialog open={open} onClose={handleClose}>
      <DialogHeader onClose={handleClose}>
        {createdToken ? 'Runner Token Created' : 'Create Runner Token'}
      </DialogHeader>

      {createdToken ? (
        <>
          <DialogBody>
            <div className="space-y-4">
              <div className="rounded-lg bg-yellow-50 p-4">
                <p className="text-sm text-yellow-800">
                  Make sure to copy your token now. You won't be able to see it again!
                </p>
              </div>
              <div className="flex items-center gap-2">
                <code className="flex-1 rounded-lg bg-gray-100 p-3 font-mono text-sm break-all">
                  {createdToken}
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
              label="Pool Name"
              value={poolName}
              onChange={(e) => setPoolName(e.target.value)}
              placeholder="default"
              required
              helperText="The pool this token will belong to"
            />
          </DialogBody>
          <DialogFooter>
            <Button variant="secondary" type="button" onClick={handleClose}>
              Cancel
            </Button>
            <Button type="submit" loading={createRunnerToken.isPending}>
              Create Token
            </Button>
          </DialogFooter>
        </form>
      )}
    </Dialog>
  )
}

interface RevokeRunnerTokenDialogProps {
  tokenId: string | null
  onClose: () => void
}

function RevokeRunnerTokenDialog({ tokenId, onClose }: RevokeRunnerTokenDialogProps) {
  const revokeRunnerToken = useRevokeRunnerToken()
  const [reason, setReason] = useState('')

  const handleRevoke = async () => {
    if (!tokenId) return
    try {
      await revokeRunnerToken.mutateAsync({ tokenId, reason: reason || undefined })
      onClose()
      setReason('')
    } catch (error) {
      console.error('Failed to revoke runner token:', error)
    }
  }

  return (
    <Dialog open={!!tokenId} onClose={onClose}>
      <DialogHeader onClose={onClose}>Revoke Runner Token</DialogHeader>
      <DialogBody className="space-y-4">
        <p className="text-sm text-gray-600">
          Are you sure you want to revoke this runner token? Any runners using this
          token will lose access immediately. This action cannot be undone.
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
        <Button
          variant="danger"
          onClick={handleRevoke}
          loading={revokeRunnerToken.isPending}
        >
          Revoke Token
        </Button>
      </DialogFooter>
    </Dialog>
  )
}

interface RotateRunnerTokenDialogProps {
  tokenId: string | null
  onClose: () => void
}

function RotateRunnerTokenDialog({ tokenId, onClose }: RotateRunnerTokenDialogProps) {
  const rotateRunnerToken = useRotateRunnerToken()
  const [newToken, setNewToken] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const handleRotate = async () => {
    if (!tokenId) return
    try {
      const result = await rotateRunnerToken.mutateAsync(tokenId)
      setNewToken(result.raw_token)
    } catch (error) {
      console.error('Failed to rotate runner token:', error)
    }
  }

  const handleCopy = useCallback(async () => {
    if (newToken) {
      const success = await copyToClipboard(newToken)
      if (success) {
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
      }
    }
  }, [newToken])

  const handleClose = () => {
    setNewToken(null)
    setCopied(false)
    onClose()
  }

  return (
    <Dialog open={!!tokenId} onClose={handleClose}>
      <DialogHeader onClose={handleClose}>
        {newToken ? 'Token Rotated' : 'Rotate Runner Token'}
      </DialogHeader>

      {newToken ? (
        <>
          <DialogBody>
            <div className="space-y-4">
              <div className="rounded-lg bg-green-50 p-4">
                <p className="text-sm text-green-800">
                  Token rotated successfully. The old token will remain valid for 1 hour.
                  Make sure to update your runner with the new token.
                </p>
              </div>
              <div className="flex items-center gap-2">
                <code className="flex-1 rounded-lg bg-gray-100 p-3 font-mono text-sm break-all">
                  {newToken}
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
        <>
          <DialogBody className="space-y-4">
            <p className="text-sm text-gray-600">
              Rotating a token will generate a new token. The old token will remain
              valid for 1 hour to allow for a smooth transition.
            </p>
            <p className="text-sm text-gray-600">
              After rotation, you'll need to update your runner configuration with
              the new token.
            </p>
          </DialogBody>
          <DialogFooter>
            <Button variant="secondary" onClick={handleClose}>
              Cancel
            </Button>
            <Button onClick={handleRotate} loading={rotateRunnerToken.isPending}>
              Rotate Token
            </Button>
          </DialogFooter>
        </>
      )}
    </Dialog>
  )
}
