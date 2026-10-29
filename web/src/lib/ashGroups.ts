import type { AshGroupBy } from './ash'

export type AshWaitGroupId = 'cpu' | 'io' | 'locks' | 'client' | 'other'

export interface AshWaitGroup {
  id: AshWaitGroupId
  label: string
  /** Wait event types the group plots; `other` also takes every unknown type. */
  types: readonly string[]
}

/** The wait groups in display order; each gets its own chart and y axis. */
export const ASH_WAIT_GROUPS: readonly AshWaitGroup[] = [
  { id: 'cpu', label: 'CPU', types: ['CPU'] },
  { id: 'io', label: 'I/O', types: ['IO'] },
  { id: 'locks', label: 'Locks and contention', types: ['Lock', 'LWLock', 'BufferPin', 'Buffer'] },
  { id: 'client', label: 'Client and inter-process', types: ['Client', 'IPC'] },
  { id: 'other', label: 'Other waits', types: ['Timeout', 'Activity', 'Extension', 'other'] },
]

/** One plain sentence per wait event type, for the legend. */
export const ASH_WAIT_DESCRIPTIONS: Readonly<Record<string, string>> = {
  CPU: 'Backend running on CPU (no wait event reported)',
  IO: 'Waiting for a read or write to storage',
  Lock: 'Waiting for a heavyweight lock held by another transaction',
  LWLock: 'Waiting for an internal lightweight lock (buffer, WAL, catalog contention)',
  BufferPin: 'Waiting for exclusive access to a data buffer',
  Buffer: 'Waiting for exclusive access to a data buffer',
  Client: 'Waiting for the client application to send or receive data',
  IPC: 'Waiting for another server process (parallel workers, replication)',
  Timeout: 'Sleeping until a timer fires',
  Activity: 'Background process idle in its main loop',
  Extension: 'Waiting inside an extension',
  other: 'Wait events not classified above',
}

const FOLDED = /^other( \(folded\))?$/u

/** The legend sentence for a wait event type; unknown types name themselves. */
export function waitDescription(type: string): string {
  return ASH_WAIT_DESCRIPTIONS[type] ?? `Wait event type ${type}`
}

/** The group of a wait event type; an unknown type is never dropped, it is "other". */
export function groupOfType(type: string | undefined): AshWaitGroupId {
  if (type === undefined) return 'other'
  return ASH_WAIT_GROUPS.find((group) => group.types.includes(type))?.id ?? 'other'
}

interface GroupableSeries {
  name: string
  points: readonly { value: number | null }[]
}

/** Average over the sampled buckets; unsampled (null) buckets are left out. */
export function seriesAverage(series: GroupableSeries): number {
  let total = 0
  let count = 0
  for (const point of series.points) {
    if (point.value === null || !Number.isFinite(point.value)) continue
    total += point.value
    count += 1
  }
  return count === 0 ? 0 : total / count
}

export interface AshGroupedSeries<S extends GroupableSeries> {
  group: AshWaitGroup
  series: S[]
  /** Average active backends of the group over the window. */
  total: number
  /** The group's part of all waits in the window; 0 when nothing was sampled. */
  share: number
}

/**
 * Split ASH series into the wait groups, in fixed order, leaving out empty
 * groups. By wait event type a series is named by its type; by wait event its
 * type comes from `eventTypes` (the server names it on every bucket). A folded
 * "other" series, and any series of an unknown type, lands in "other".
 */
export function groupSeries<S extends GroupableSeries>(
  series: readonly S[],
  groupBy: Extract<AshGroupBy, 'wait_event_type' | 'wait_event'>,
  eventTypes: ReadonlyMap<string, string> = new Map(),
): AshGroupedSeries<S>[] {
  const byGroup = new Map<AshWaitGroupId, S[]>()
  for (const entry of series) {
    const type = FOLDED.test(entry.name)
      ? 'other'
      : groupBy === 'wait_event_type'
        ? entry.name
        : eventTypes.get(entry.name)
    const id = groupOfType(type)
    byGroup.set(id, [...(byGroup.get(id) ?? []), entry])
  }

  const groups = ASH_WAIT_GROUPS.flatMap((group) => {
    const members = byGroup.get(group.id) ?? []
    if (members.length === 0) return []
    const total = members.reduce((sum, entry) => sum + seriesAverage(entry), 0)
    return [{ group, series: members, total }]
  })
  const grandTotal = groups.reduce((sum, entry) => sum + entry.total, 0)
  return groups.map((entry) => ({
    ...entry,
    share: grandTotal > 0 ? entry.total / grandTotal : 0,
  }))
}

/** A small average as `<0.01` rather than a misleading `0.00`. */
export function formatAverage(value: number): string {
  return value > 0 && value < 0.005 ? '<0.01' : value.toFixed(2)
}

/**
 * One letter per entry, unique within the legend: the first letter of the
 * name not taken yet (Lock → L, LWLock → W), else the entry's position.
 */
export function swatchLetters(names: readonly string[]): string[] {
  const taken = new Set<string>()
  return names.map((name, index) => {
    const letter =
      [...name.toLocaleUpperCase('en-US')].find(
        (char) => /[A-Z0-9]/u.test(char) && !taken.has(char),
      ) ?? String(index + 1)
    taken.add(letter)
    return letter
  })
}
