import {
  getPreferencesSnapshot,
  subscribePreferences,
  type TimeZonePreference,
} from './preferences'

/*
 * The zone the console shows times in: the operator's choice in Settings ›
 * Preferences, else the server default (PGLENS_TZ, handed over by
 * GET /api/v1/session). Every formatter reads it, so a page never mixes zones.
 * URLs, exports and API requests stay in UTC (ISO 8601).
 */

let serverZone = 'UTC'
const listeners = new Set<() => void>()

export function isTimeZone(zone: string): boolean {
  if (zone === '') return false
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: zone })
    return true
  } catch {
    return false
  }
}

export function browserTimeZone(): string {
  try {
    const zone = Intl.DateTimeFormat().resolvedOptions().timeZone
    return zone && isTimeZone(zone) ? zone : 'UTC'
  } catch {
    return 'UTC'
  }
}

export function serverTimeZone(): string {
  return serverZone
}

/** Record the server default; a missing or unknown zone keeps UTC. */
export function setServerTimeZone(zone: string | undefined): void {
  const next = zone !== undefined && isTimeZone(zone) ? zone : 'UTC'
  if (next === serverZone) return
  serverZone = next
  for (const listener of listeners) listener()
}

export function resolveTimeZone(
  choice: TimeZonePreference,
  server: string = serverZone,
  browser: string = browserTimeZone(),
): string {
  if (choice === 'browser') return browser
  if (choice === 'UTC') return 'UTC'
  return server
}

/** The zone every console time is written in right now. */
export function displayTimeZone(): string {
  return resolveTimeZone(getPreferencesSnapshot().timeZone)
}

export function subscribeTimeZone(listener: () => void): () => void {
  listeners.add(listener)
  const unsubscribe = subscribePreferences(listener)
  return () => {
    listeners.delete(listener)
    unsubscribe()
  }
}

export interface WallClock {
  year: number
  month: number
  day: number
  hour: number
  minute: number
  second: number
}

const clockFormats = new Map<string, Intl.DateTimeFormat>()
const offsetFormats = new Map<string, Intl.DateTimeFormat>()

function cachedFormat(
  cache: Map<string, Intl.DateTimeFormat>,
  zone: string,
  options: Intl.DateTimeFormatOptions,
): Intl.DateTimeFormat {
  let format = cache.get(zone)
  if (!format) {
    format = new Intl.DateTimeFormat('en-US', { ...options, timeZone: zone })
    cache.set(zone, format)
  }
  return format
}

/** The wall clock in `zone` at `ms` (UTC milliseconds). */
export function wallClock(ms: number, zone: string): WallClock {
  const date = new Date(ms)
  if (zone === 'UTC') {
    return {
      year: date.getUTCFullYear(),
      month: date.getUTCMonth() + 1,
      day: date.getUTCDate(),
      hour: date.getUTCHours(),
      minute: date.getUTCMinutes(),
      second: date.getUTCSeconds(),
    }
  }
  const parts: Record<string, number> = {}
  const format = cachedFormat(clockFormats, zone, {
    numberingSystem: 'latn',
    year: 'numeric',
    month: 'numeric',
    day: 'numeric',
    hour: 'numeric',
    minute: 'numeric',
    second: 'numeric',
    hourCycle: 'h23',
  })
  for (const part of format.formatToParts(date)) {
    if (part.type !== 'literal') parts[part.type] = Number(part.value)
  }
  return {
    year: parts.year ?? 0,
    month: parts.month ?? 0,
    day: parts.day ?? 0,
    hour: (parts.hour ?? 0) % 24,
    minute: parts.minute ?? 0,
    second: parts.second ?? 0,
  }
}

/** `UTC`, `UTC+1`, `UTC+5:30`: the zone's offset at `ms`. */
export function zoneLabel(zone: string, ms: number = Date.now()): string {
  if (zone === 'UTC') return 'UTC'
  const name =
    cachedFormat(offsetFormats, zone, { timeZoneName: 'shortOffset' })
      .formatToParts(new Date(ms))
      .find((part) => part.type === 'timeZoneName')?.value ?? 'GMT'
  return name === 'GMT' || name === 'GMT+0' || name === 'GMT-0' ? 'UTC' : name.replace('GMT', 'UTC')
}

function offsetMs(ms: number, zone: string): number {
  const w = wallClock(ms, zone)
  const asUTC = Date.UTC(w.year, w.month - 1, w.day, w.hour, w.minute, w.second)
  return asUTC - Math.floor(ms / 1_000) * 1_000
}

const pad = (value: number) => String(value).padStart(2, '0')

/** A `datetime-local` value (`YYYY-MM-DDTHH:mm`) holding the wall clock in `zone`. */
export function toZonedInput(ms: number, zone: string): string {
  const w = wallClock(ms, zone)
  return `${w.year}-${pad(w.month)}-${pad(w.day)}T${pad(w.hour)}:${pad(w.minute)}`
}

/** The instant a `datetime-local` value names when read as wall clock in `zone`. */
export function fromZonedInput(value: string, zone: string): Date | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(value)
  if (!match) return null
  const [year, month, day, hour, minute] = match.slice(1).map(Number) as [
    number,
    number,
    number,
    number,
    number,
  ]
  const asUTC = Date.UTC(year, month - 1, day, hour, minute)
  if (!Number.isFinite(asUTC)) return null
  const first = offsetMs(asUTC, zone)
  let ms = asUTC - first
  // Across a DST change the offset at the guess differs from the one at the result.
  const second = offsetMs(ms, zone)
  if (second !== first) ms = asUTC - second
  return new Date(ms)
}
