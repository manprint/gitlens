import type { MessageKey } from '@/i18n/messages/en'
import { activeIntl, translate } from '@/i18n/store'

import type { AshDimension, AshGroupBy } from './ash'

export type AshChartGroup =
  Extract<AshGroupBy, 'wait_event_type' | 'wait_event' | 'queryid'> | AshDimension

/** The console label of each group, also the dimension's name on a filter chip. */
export const ASH_GROUP_LABELS: Record<AshChartGroup, MessageKey> = {
  queryid: 'ash.group.queryid',
  wait_event: 'ash.group.waitEvent',
  wait_event_type: 'ash.group.waitEventType',
  usename: 'ash.group.usename',
  client_addr: 'ash.group.clientAddr',
  application_name: 'ash.group.applicationName',
}

export type AshWaitGroupId = 'cpu' | 'io' | 'locks' | 'client' | 'other'

export interface AshWaitGroup {
  id: AshWaitGroupId
  /** The chart title, in the console language through `labelKey`. */
  labelKey: MessageKey
  /** Wait event types the group plots; `other` also takes every unknown type. */
  types: readonly string[]
}

/** The wait groups in display order; each gets its own chart and y axis. */
export const ASH_WAIT_GROUPS: readonly AshWaitGroup[] = [
  { id: 'cpu', labelKey: 'ash.waitGroup.cpu', types: ['CPU'] },
  { id: 'io', labelKey: 'ash.waitGroup.io', types: ['IO'] },
  {
    id: 'locks',
    labelKey: 'ash.waitGroup.locks',
    types: ['Lock', 'LWLock', 'BufferPin', 'Buffer'],
  },
  { id: 'client', labelKey: 'ash.waitGroup.client', types: ['Client', 'IPC'] },
  {
    id: 'other',
    labelKey: 'ash.waitGroup.other',
    types: ['Timeout', 'Activity', 'Extension', 'other'],
  },
]

/** The catalog key of the legend sentence of each wait event type. */
export const ASH_WAIT_DESCRIPTIONS: Readonly<Record<string, MessageKey>> = {
  CPU: 'ash.waitType.CPU',
  IO: 'ash.waitType.IO',
  Lock: 'ash.waitType.Lock',
  LWLock: 'ash.waitType.LWLock',
  BufferPin: 'ash.waitType.BufferPin',
  Buffer: 'ash.waitType.Buffer',
  Client: 'ash.waitType.Client',
  IPC: 'ash.waitType.IPC',
  Timeout: 'ash.waitType.Timeout',
  Activity: 'ash.waitType.Activity',
  Extension: 'ash.waitType.Extension',
  other: 'ash.waitType.other',
}

const FOLDED = /^other( \(folded\))?$/u

/** The legend sentence for a wait event type; unknown types name themselves. */
export function waitDescription(type: string): string {
  const key = ASH_WAIT_DESCRIPTIONS[type]
  return key ? translate(key) : translate('ash.waitType.unknown', { type })
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
  const two = new Intl.NumberFormat(activeIntl(), {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
    useGrouping: false,
  })
  return value > 0 && value < 0.005 ? `<${two.format(0.01)}` : two.format(value)
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
