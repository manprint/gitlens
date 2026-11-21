import type { ApiFailure } from '@/api/client'
import type { components } from '@/api/generated'
import { translate } from '@/i18n/store'

export type ServerCommandState = components['schemas']['CommandResponse']['state']
export type CommandState = ServerCommandState | 'succeeded' | 'rejected'

const terminalStates: ReadonlySet<CommandState> = new Set([
  'done',
  'succeeded',
  'failed',
  'expired',
  'rejected',
])

export function isTerminalCommandState(state: unknown): state is CommandState {
  return typeof state === 'string' && terminalStates.has(state as CommandState)
}

function assertNever(value: never): never {
  throw new Error(`Unhandled command state: ${String(value)}`)
}

/**
 * The server refuses a new command once the agent holds its pending limit: 429,
 * with a detail that names the count. Any other failure has no such message.
 */
export function commandQueueFullMessage(failure: ApiFailure): string | null {
  if (failure.kind !== 'server' || failure.status !== 429) return null
  return translate('shell.command.queueFull', { detail: failure.detail })
}

export function describeCommandState(state: CommandState, error?: string | null): string {
  switch (state) {
    case 'pending':
      return translate('shell.command.pending')
    case 'claimed':
      return translate('shell.command.claimed')
    case 'done':
    case 'succeeded':
      return translate('shell.command.succeeded')
    case 'failed':
      return error
        ? translate('shell.command.failed', { error })
        : translate('shell.command.failedNoResult')
    case 'expired':
      return error
        ? translate('shell.command.expired', { error })
        : translate('shell.command.expiredTtl')
    case 'rejected':
      return error
        ? translate('shell.command.rejected', { error })
        : translate('shell.command.rejectedNoReason')
    default:
      return assertNever(state)
  }
}
