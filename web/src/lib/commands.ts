import type { components } from '@/api/generated'

export type ServerCommandState = components['schemas']['CommandResponse']['state']
export type CommandState = ServerCommandState | 'succeeded' | 'rejected'

export const COMMAND_TTL_MS = 5 * 60_000
export const COMMAND_GRACE_MS = 5_000
export const COMMAND_CLIENT_TIMEOUT_MS = COMMAND_TTL_MS + COMMAND_GRACE_MS
export const COMMAND_TTL_LABEL = '5 minutes'

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
      return `Command expired: no agent claimed it within the ${COMMAND_TTL_LABEL} TTL.`
    case 'rejected':
      return error ? `Command rejected by the agent: ${error}` : 'Command rejected by the agent.'
    default:
      return assertNever(state)
  }
}
