import type { Cluster } from '@/api/types'

const LAST_INSTANCE_KEY = 'pglens.lastInstance'

/** The instance the operator opened last, remembered across pages. */
export function readLastInstance(): string | null {
  try {
    return window.localStorage.getItem(LAST_INSTANCE_KEY)
  } catch {
    return null
  }
}

export function writeLastInstance(instanceId: string): void {
  try {
    window.localStorage.setItem(LAST_INSTANCE_KEY, instanceId)
  } catch {
    // Remembering the instance is a convenience; a blocked store only forgets it.
  }
}

/**
 * Picks the instance a server-scoped page shows when none is named: the
 * requested one when it exists, then the remembered one, then the first
 * instance that is up, then any instance. The sidebar and the pages use the
 * same rule, so they always agree on the current server.
 */
export function chooseInstance(
  clusters: readonly Cluster[],
  requested: string | null,
  remembered: string | null,
): string | null {
  const instances = clusters.flatMap((cluster) => cluster.instances)
  const known = (id: string | null) =>
    id !== null && instances.some((instance) => instance.instance_id === id)
  if (known(requested)) return requested
  if (known(remembered)) return remembered
  return (instances.find((instance) => instance.up) ?? instances[0])?.instance_id ?? null
}
