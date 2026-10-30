import { Link } from 'react-router-dom'

import { clusterNameOrId, instanceAddress, instanceNameOrId, type NameLookup } from '@/lib/names'
import { cn } from '@/lib/utils'

/**
 * Cluster and instance cells of the alert and event tables (D14, D15): the
 * name from the cluster list the shell polls, the id when the list does not
 * know it. The cell is cut to one line from 1600 px (D26); its `title` holds
 * the cluster id or the instance address.
 */

function objectLink(path: string, id: string, label: string) {
  return (
    <Link
      // An id keeps the monospace face; a name reads as text.
      className={cn('text-primary text-xs hover:underline', label === id && 'font-mono')}
      to={`${path}/${encodeURIComponent(id)}`}
    >
      {label}
    </Link>
  )
}

/** The tooltip of a cluster cell: the cluster id. */
export function clusterTitle(id: string | null | undefined, missing: string): string {
  return id ?? missing
}

/** A cluster cell; `missing` when the row has no cluster. */
export function clusterCell(names: NameLookup, id: string | null | undefined, missing: string) {
  if (id === null || id === undefined) return missing
  return objectLink('/clusters', id, clusterNameOrId(names, id))
}

/** The tooltip of an instance cell: its address and port when known, else the id. */
export function instanceTitle(
  names: NameLookup,
  id: string | null | undefined,
  missing: string,
): string {
  if (id === null || id === undefined) return missing
  const instance = names.instance(id)
  return instance ? instanceAddress(instance) : id
}

/** An instance cell; `missing` when the row has no instance. */
export function instanceCell(names: NameLookup, id: string | null | undefined, missing: string) {
  if (id === null || id === undefined) return missing
  return objectLink('/instances', id, instanceNameOrId(names, id))
}
