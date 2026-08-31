import type { Schemas } from '@/api/types'

/**
 * The inventory endpoint currently returns databases in its canonical order.
 * The optional activity value lets callers prefer a measured activity sample
 * when one is available without changing the API contract for the inventory.
 */
export type DatabaseEntry = Schemas['Database'] & {
  activity?: number | null
}

export interface DatabasePartition {
  monitored: DatabaseEntry[]
  skipped: Map<string, string[]>
}

const UNKNOWN_SKIP_REASON = 'unknown'

export function partitionDatabases(list: readonly DatabaseEntry[]): DatabasePartition {
  const monitored: DatabaseEntry[] = []
  const skipped = new Map<string, string[]>()

  for (const database of list) {
    if (database.monitored) {
      monitored.push(database)
      continue
    }

    const reason = database.skip_reason?.trim()
    const normalizedReason = reason === undefined || reason === '' ? UNKNOWN_SKIP_REASON : reason
    const names = skipped.get(normalizedReason) ?? []
    names.push(database.datname)
    skipped.set(normalizedReason, names)
  }

  return { monitored, skipped }
}

/**
 * Pick the most active monitored database. API responses without activity
 * samples are already canonical, so their first monitored entry is the
 * honest fallback rather than pretending that an unmeasured value is zero.
 */
export function defaultDatabase(list: readonly DatabaseEntry[]): DatabaseEntry | null {
  const { monitored } = partitionDatabases(list)
  let selected = monitored[0] ?? null

  for (const candidate of monitored.slice(1)) {
    if (candidate.activity == null) continue
    if (selected?.activity == null || candidate.activity > selected.activity) {
      selected = candidate
    }
  }

  return selected
}

export function selectedDatabase(
  list: readonly DatabaseEntry[],
  requestedDatabase: string | null,
): DatabaseEntry | null {
  const { monitored } = partitionDatabases(list)
  return monitored.find((database) => database.datname === requestedDatabase) ?? defaultDatabase(monitored)
}
