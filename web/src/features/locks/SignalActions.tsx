import { useRef, useState } from 'react'

import { useCreateCommand } from '@/api/commands'
import type { PermTier } from '@/api/types'
import { ErrorState, NotPermitted } from '@/components/state'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { describeCommandState, isTerminalCommandState } from '@/lib/commands'
import type { LockNode } from '@/lib/locks'

type SignalKind = 'cancel' | 'terminate'

interface SignalActionsProps {
  node: LockNode
  instanceId?: string
  currentTier: PermTier
}

interface CommandHandle {
  mutate: ReturnType<typeof useCreateCommand>['mutate']
  isPending: boolean
  error: ReturnType<typeof useCreateCommand>['error']
  command: ReturnType<typeof useCreateCommand>['command']
  reset: ReturnType<typeof useCreateCommand>['reset']
}

function textValue(value: unknown): string {
  if (typeof value === 'string' && value.trim() !== '') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return '—'
}

function queryFirstLine(value: unknown): string {
  if (typeof value !== 'string' || value.trim() === '') return '—'
  return value.split(/\r?\n/u, 1)[0]?.trim() ?? '—'
}

function commandPID(node: LockNode): number | null {
  return typeof node.pid === 'number' && Number.isSafeInteger(node.pid) ? node.pid : null
}

function isClientBackend(node: LockNode): boolean {
  const backendType = node.backend_type ?? node.backendType
  return typeof backendType !== 'string' || backendType === 'client backend'
}

function actionButton(kind: SignalKind, onClick: () => void, disabled = false) {
  return (
    <button
      type="button"
      className="rounded-md border px-3 py-2 text-sm font-medium"
      aria-label={kind === 'cancel' ? 'Cancel query' : 'Terminate backend'}
      disabled={disabled}
      onClick={onClick}
    >
      {kind === 'cancel' ? 'Cancel query' : 'Terminate backend'}
    </button>
  )
}

function commandStatus(handle: CommandHandle, instanceId?: string) {
  const command = handle.command.data
  if (!command) return null
  const terminal = isTerminalCommandState(command.state)

  return (
    <p role="status" className="mt-2 text-sm">
      {describeCommandState(command.state, command.error)}
      {terminal && instanceId ? (
        <>
          {' '}
          <a
            className="text-primary underline underline-offset-2"
            href={`/api/v1/instances/${encodeURIComponent(instanceId)}/command-audit`}
          >
            View command audit
          </a>
        </>
      ) : null}
    </p>
  )
}

export function SignalActions({ node, instanceId, currentTier }: SignalActionsProps) {
  const cancelCommand = useCreateCommand(instanceId)
  const terminateCommand = useCreateCommand(instanceId)
  const [openKind, setOpenKind] = useState<SignalKind | null>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)
  const pid = commandPID(node)
  const pidLabel = textValue(node.pid)
  const database = textValue(node.datname ?? node.database)
  const user = textValue(node.usename ?? node.username ?? node.user)
  const application = textValue(node.application_name ?? node.application)
  const query = queryFirstLine(node.query)
  const handles: Record<SignalKind, CommandHandle> = {
    cancel: cancelCommand,
    terminate: terminateCommand,
  }
  const selectedHandle = openKind ? handles[openKind] : null
  const policyClosed = node.allow_signal === false
  const belowT2 = currentTier !== 'T2'
  const unavailable = !isClientBackend(node)

  if (unavailable) {
    return (
      <p role="status" className="mt-3 text-sm">
        Signal actions unavailable: PID {pidLabel} is not a client backend.
      </p>
    )
  }

  const submit = () => {
    if (!openKind || pid === null) return
    handles[openKind].mutate({
      args: { pid },
      kind: openKind,
    })
    setOpenKind(null)
  }

  const buttons = (
    <div className="mt-3 flex flex-wrap gap-2" aria-label={`Signal actions for PID ${pidLabel}`}>
      {actionButton('cancel', () => setOpenKind('cancel'), belowT2 || policyClosed || pid === null)}
      {actionButton(
        'terminate',
        () => setOpenKind('terminate'),
        belowT2 || policyClosed || pid === null,
      )}
    </div>
  )

  return (
    <div>
      {belowT2 ? (
        <NotPermitted required="T2" current={currentTier}>
          {actionButton('cancel', () => setOpenKind('cancel'), true)}
        </NotPermitted>
      ) : policyClosed ? (
        <div role="status" className="border-muted bg-muted/30 mt-3 p-3 text-sm">
          <p>Signal actions are disabled because the target reports allow_signal=false.</p>
          {buttons}
        </div>
      ) : (
        buttons
      )}
      {belowT2 ? (
        <>
          <p className="text-muted-foreground mt-1 text-xs">
            Grant T2 with <code>monitoring_user.sql -v tier1=1 -v tier2=1</code>.
          </p>
          {actionButton('terminate', () => setOpenKind('terminate'), true)}
        </>
      ) : null}
      {commandStatus(cancelCommand, instanceId)}
      {commandStatus(terminateCommand, instanceId)}
      {cancelCommand.error ? (
        <ErrorState
          endpoint="cancel command"
          failure={cancelCommand.error.failure}
          onRetry={() => cancelCommand.reset()}
        />
      ) : null}
      {terminateCommand.error ? (
        <ErrorState
          endpoint="terminate command"
          failure={terminateCommand.error.failure}
          onRetry={() => terminateCommand.reset()}
        />
      ) : null}

      <Dialog open={openKind !== null} onOpenChange={(open) => !open && setOpenKind(null)}>
        <DialogContent
          onOpenAutoFocus={(event) => {
            event.preventDefault()
            cancelRef.current?.focus()
          }}
        >
          <DialogHeader>
            <DialogTitle>
              {openKind === 'terminate' ? 'Terminate backend?' : 'Cancel query?'}
            </DialogTitle>
            <DialogDescription>
              PID {pidLabel}; database {database}; user {user}; application {application}; query:{' '}
              {query}.
              {openKind === 'terminate'
                ? " The client's connection will be closed and its open transaction will be rolled back."
                : ' This asks PostgreSQL to cancel the current query.'}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <button
              ref={cancelRef}
              type="button"
              className="rounded-md border px-3 py-2 text-sm font-medium"
              onClick={() => setOpenKind(null)}
            >
              Keep running
            </button>
            <Button
              type="button"
              variant={openKind === 'terminate' ? 'destructive' : 'default'}
              onClick={submit}
            >
              {openKind === 'terminate' ? 'Terminate backend' : 'Cancel query'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {selectedHandle?.isPending ? <p role="status">Submitting {openKind} command…</p> : null}
    </div>
  )
}
