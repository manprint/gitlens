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
import { useT, type Translate } from '@/i18n/react'
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

function actionButton(t: Translate, kind: SignalKind, onClick: () => void, disabled = false) {
  return (
    <Button
      size="sm"
      variant="outline"
      type="button"

      aria-label={t(
        kind === 'cancel' ? 'locks.signal.cancelQuery' : 'locks.signal.terminateBackend',
      )}
      disabled={disabled}
      onClick={onClick}
    >
      {t(kind === 'cancel' ? 'locks.signal.cancelQuery' : 'locks.signal.terminateBackend')}
    </Button>
  )
}

function commandStatus(t: Translate, handle: CommandHandle) {
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
          <Link className="text-primary underline underline-offset-2" to="/settings?tab=audit">
            {t('locks.signal.viewAudit')}
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
  const t = useT()
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
  // A sample that shows the PID as another kind of backend ends the
  // confirmation, so a later sample with a client backend at that PID cannot
  // reopen it for the session the operator saw before.
  if (unavailable && pending !== null) {
    setPending(null)
  }

  if (unavailable) {
    return (
      <p role="status" className="mt-3 text-sm">
        {t('locks.signal.unavailable', { pid: pidLabel })}
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
    <div
      className="mt-3 flex flex-wrap gap-2"
      aria-label={t('locks.signal.groupLabel', { pid: pidLabel })}
    >
      {actionButton(
        t,
        'cancel',
        () => setPending({ kind: 'cancel', target: node }),
        belowT2 || pid === null || sampleStale,
      )}
      {actionButton(
        t,
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
          {actionButton(t, 'cancel', () => setPending({ kind: 'cancel', target: node }), true)}
        </NotPermitted>
      ) : (
        <>
          {buttons}
          {/* The locks response does not carry the target's allow_signal setting, so
              the agent's refusal arrives as the command result, not as a disabled button. */}
          <p className="text-muted-foreground mt-1 text-xs">{t('locks.signal.allowSignal')}</p>
          {sampleStale ? (
            <p className="text-muted-foreground mt-1 text-xs">{t('locks.signal.staleDisabled')}</p>
          ) : null}
        </>
      )}
      {belowT2 ? (
        <>
          <p className="text-muted-foreground mt-1 text-xs">
            {t('locks.signal.tierNeeded')} <code>monitoring_user.sql -v tier1=1 -v tier2=1</code>
            {t('locks.signal.tierCommandEnd')}
          </p>
          {actionButton(
            t,
            'terminate',
            () => setPending({ kind: 'terminate', target: node }),
            true,
          )}
        </>
      ) : null}
      {commandStatus(t, cancelCommand)}
      {commandStatus(t, terminateCommand)}
      {cancelCommand.error ? (
        <CommandSubmitError
          endpoint={t('locks.signal.endpointCancel')}
          failure={cancelCommand.error.failure}
          onDismiss={() => cancelCommand.reset()}
        />
      ) : null}
      {terminateCommand.error ? (
        <CommandSubmitError
          endpoint={t('locks.signal.endpointTerminate')}
          failure={terminateCommand.error.failure}
          onDismiss={() => terminateCommand.reset()}
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
              {openKind === 'terminate'
                ? t('locks.signal.terminateTitle')
                : t('locks.signal.cancelTitle')}
            </DialogTitle>
            <DialogDescription>
              {t('locks.signal.summary', {
                pid: confirmed.pidLabel,
                database: confirmed.database,
                user: confirmed.user,
                application: confirmed.application,
                query: confirmed.query,
              })}{' '}
              {openKind === 'terminate'
                ? t('locks.signal.terminateConsequence')
                : t('locks.signal.cancelConsequence')}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              size="sm"
              variant="outline"
              ref={cancelRef}
              type="button"

              onClick={() => setPending(null)}
            >
              {t('locks.signal.keepRunning')}
            </Button>
            <Button
              type="button"
              variant={openKind === 'terminate' ? 'destructive' : 'default'}
              disabled={sampleStale}
              onClick={submit}
            >
              {openKind === 'terminate'
                ? t('locks.signal.terminateBackend')
                : t('locks.signal.cancelQuery')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {selectedHandle?.isPending ? (
        <p role="status">
          {t('locks.signal.submitting', {
            kind: t(
              openKind === 'terminate' ? 'locks.signal.kindTerminate' : 'locks.signal.kindCancel',
            ),
          })}
        </p>
      ) : null}
    </div>
  )
}
