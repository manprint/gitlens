import { useRef, useState } from 'react'
import { Link } from 'react-router-dom'

import { useCreateCommand, type CommandArgs } from '@/api/commands'
import type { PermTier } from '@/api/types'
import { CommandSubmitError, NotPermitted } from '@/components/state'
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

interface PendingSignal {
  kind: SignalKind
  // The session as the operator saw it when the confirmation opened.
  target: LockNode
}

interface SignalActionsProps {
  node: LockNode
  instanceId?: string
  currentTier: PermTier
  // Set when the lock sample is stale. The session may have ended and its PID
  // been reused since the sample, and the command carries only the PID.
  sampleStale?: boolean
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

// The start time goes back exactly as the API reported it. A Date would keep
// milliseconds only, while the agent compares microseconds.
function commandArgs(node: LockNode, pid: number): CommandArgs {
  const args: CommandArgs = { pid }
  if (typeof node.backend_start === 'string' && node.backend_start !== '') {
    args.backend_start = node.backend_start
  }
  return args
}

function sessionSummary(node: LockNode) {
  return {
    pidLabel: textValue(node.pid),
    database: textValue(node.datname ?? node.database),
    user: textValue(node.usename ?? node.username ?? node.user),
    application: textValue(node.application_name ?? node.application),
    query: queryFirstLine(node.query),
  }
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

function commandStatus(handle: CommandHandle) {
  const command = handle.command.data
  if (!command) return null
  const terminal = isTerminalCommandState(command.state)

  return (
    <p role="status" className="mt-2 text-sm">
      {describeCommandState(command.state, command.error)}
      {terminal ? (
        <>
          {' '}
          {/* The audit is a section of Settings; its API endpoint returns raw JSON. */}
          <Link className="text-primary underline underline-offset-2" to="/settings">
            View command audit
          </Link>
        </>
      ) : null}
    </p>
  )
}

export function SignalActions({
  node,
  instanceId,
  currentTier,
  sampleStale = false,
}: SignalActionsProps) {
  const cancelCommand = useCreateCommand(instanceId)
  const terminateCommand = useCreateCommand(instanceId)
  // The confirmation keeps the session it was opened for. A lock sample can
  // refresh while the dialog is open, and the PID may belong to another session
  // by then, so the dialog and its command use the node as it was opened.
  const [pending, setPending] = useState<PendingSignal | null>(null)
  const openKind = pending?.kind ?? null
  const cancelRef = useRef<HTMLButtonElement>(null)
  const pid = commandPID(node)
  const pidLabel = textValue(node.pid)
  const confirmed = sessionSummary(pending?.target ?? node)
  const handles: Record<SignalKind, CommandHandle> = {
    cancel: cancelCommand,
    terminate: terminateCommand,
  }
  const selectedHandle = openKind ? handles[openKind] : null
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
    if (!pending || sampleStale) return
    const targetPID = commandPID(pending.target)
    if (targetPID === null) return
    handles[pending.kind].mutate({
      args: commandArgs(pending.target, targetPID),
      kind: pending.kind,
    })
    setPending(null)
  }

  const buttons = (
    <div className="mt-3 flex flex-wrap gap-2" aria-label={`Signal actions for PID ${pidLabel}`}>
      {actionButton(
        'cancel',
        () => setPending({ kind: 'cancel', target: node }),
        belowT2 || pid === null || sampleStale,
      )}
      {actionButton(
        'terminate',
        () => setPending({ kind: 'terminate', target: node }),
        belowT2 || pid === null || sampleStale,
      )}
    </div>
  )

  return (
    <div>
      {belowT2 ? (
        <NotPermitted required="T2" current={currentTier}>
          {actionButton('cancel', () => setPending({ kind: 'cancel', target: node }), true)}
        </NotPermitted>
      ) : (
        <>
          {buttons}
          {/* The locks response does not carry the target's allow_signal setting, so
              the agent's refusal arrives as the command result, not as a disabled button. */}
          <p className="text-muted-foreground mt-1 text-xs">
            Signals remain subject to allow_signal on the target.
          </p>
          {sampleStale ? (
            <p className="text-muted-foreground mt-1 text-xs">
              Signal actions are disabled until a fresh lock sample arrives.
            </p>
          ) : null}
        </>
      )}
      {belowT2 ? (
        <>
          <p className="text-muted-foreground mt-1 text-xs">
            Grant T2 with <code>monitoring_user.sql -v tier1=1 -v tier2=1</code>.
          </p>
          {actionButton('terminate', () => setPending({ kind: 'terminate', target: node }), true)}
        </>
      ) : null}
      {commandStatus(cancelCommand)}
      {commandStatus(terminateCommand)}
      {cancelCommand.error ? (
        <CommandSubmitError
          endpoint="cancel command"
          failure={cancelCommand.error.failure}
          onRetry={() => cancelCommand.reset()}
        />
      ) : null}
      {terminateCommand.error ? (
        <CommandSubmitError
          endpoint="terminate command"
          failure={terminateCommand.error.failure}
          onRetry={() => terminateCommand.reset()}
        />
      ) : null}

      <Dialog open={openKind !== null} onOpenChange={(open) => !open && setPending(null)}>
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
              PID {confirmed.pidLabel}; database {confirmed.database}; user {confirmed.user};
              application {confirmed.application}; query: {confirmed.query}.
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
              onClick={() => setPending(null)}
            >
              Keep running
            </button>
            <Button
              type="button"
              variant={openKind === 'terminate' ? 'destructive' : 'default'}
              disabled={sampleStale}
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
