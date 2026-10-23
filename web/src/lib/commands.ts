import type { ApiFailure } from '@/api/client'
import type { components } from '@/api/generated'

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
  return `Command queue full: ${failure.detail}`
}

export function describeCommandState(state: CommandState, error?: string | null): string {
  switch (state) {
    case 'pending':
      return 'Command is queued and waiting for an agent.'
    case 'claimed':
      return 'Command was claimed by an agent and is running.'
    case 'done':
    case 'succeeded':
      return 'Command succeeded and its result is ready.'
    case 'failed':
      return error ? `Command failed: ${error}` : 'Command failed without an agent result.'
    case 'expired':
      return error
        ? `Command expired: ${error}`
        : 'Command expired: its TTL ended before a result was recorded.'
    case 'rejected':
      return error ? `Command rejected by the agent: ${error}` : 'Command rejected by the agent.'
    default:
      return assertNever(state)
  }
}
