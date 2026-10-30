/**
 * Names given to clusters and instances (D14, D15) and the labels the console
 * shows for them. The server keeps the rules (`validateName` in
 * internal/server/rename.go); `validateDisplayName` mirrors them so the
 * rename dialog can refuse a name before it is sent.
 */

import type { ApiFailure } from '@/api/client'

/** The longest name the server accepts, counted in Unicode code points. */
export const NAME_MAX_LENGTH = 64

/** Control, invisible format and line or paragraph separator characters (Go `unicode.IsControl`, Cf, Zl, Zp). */
const REFUSED_CHARACTERS = /[\p{Cc}\p{Cf}\p{Zl}\p{Zp}]/u

/**
 * The white space Go's `strings.TrimSpace` removes (`unicode.IsSpace`). It is
 * not JavaScript's `trim()`: that also removes U+FEFF, which the server
 * refuses, and keeps U+0085, which the server trims.
 */
const SURROUNDING_SPACE =
  /^[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$/g

/** Trims a name as the server does. */
export function trimName(text: string): string {
  return text.replace(SURROUNDING_SPACE, '')
}

interface NamedCluster {
  cluster_id: string
  name?: string | null
}

interface NamedInstance {
  addr: string
  port: number
  display_name?: string | null
}

/** A stored name, or null when there is none; `undefined` (field left out) counts as none. */
function storedName(name: string | null | undefined): string | null {
  return name === undefined || name === null || trimName(name) === '' ? null : name
}

/**
 * The label of a cluster: its name, else the page's own fallback
 * (`Cluster <id>` unless the caller passes another one).
 */
export function clusterLabel(
  cluster: NamedCluster,
  fallback = `Cluster ${cluster.cluster_id}`,
): string {
  return storedName(cluster.name) ?? fallback
}

/** The address and port of an instance, as the console shows them. */
export function instanceAddress(instance: Pick<NamedInstance, 'addr' | 'port'>): string {
  return `${instance.addr}:${instance.port}`
}

/** The label of an instance: its display name, else its address and port. */
export function instanceLabel(instance: NamedInstance): string {
  return storedName(instance.display_name) ?? instanceAddress(instance)
}

/** True when the instance has a display name, so its address goes on a secondary line. */
export function hasDisplayName(instance: Pick<NamedInstance, 'display_name'>): boolean {
  return storedName(instance.display_name) !== null
}

export type NameValidation = { ok: true; value: string | null } | { ok: false; error: string }

/**
 * Checks a name typed in the rename dialog against the server rules: the text
 * is trimmed; an empty or blank text clears the name (`null`); at most 64
 * characters; no control, invisible format or separator character.
 */
export function validateDisplayName(text: string): NameValidation {
  const trimmed = trimName(text)
  if (trimmed === '') return { ok: true, value: null }
  if ([...trimmed].length > NAME_MAX_LENGTH) {
    return { ok: false, error: `Use at most ${NAME_MAX_LENGTH} characters.` }
  }
  if (REFUSED_CHARACTERS.test(trimmed)) {
    return { ok: false, error: 'Remove tabs, line breaks and other invisible characters.' }
  }
  return { ok: true, value: trimmed }
}

/** What a rename dialog renames. */
export type RenameKind = 'cluster' | 'instance'

function isApiFailure(error: unknown): error is ApiFailure {
  return typeof error === 'object' && error !== null && 'kind' in error
}

/** The server's answer in words: 409 is a duplicate, other statuses carry the server message. */
export function renameErrorMessage(error: unknown, kind: RenameKind): string {
  if (!isApiFailure(error)) return 'The name could not be saved. Try again.'
  switch (error.kind) {
    case 'server':
      if (error.status === 409) return 'That name is already used. Choose another one.'
      return error.detail ? `${error.error}: ${error.detail}` : error.error
    case 'unprocessable':
      return error.detail ? `${error.error}: ${error.detail}` : error.error
    case 'not_found':
      return `The ${kind} no longer exists. Reload the page.`
    case 'unauthorized':
      return 'Your session has expired. Sign in again to rename.'
    case 'forbidden':
      return 'The server refused the change.'
    case 'network':
    case 'malformed':
    case 'client':
      return error.message
  }
}

interface LookupCluster extends NamedCluster {
  instances: readonly (NamedInstance & { instance_id: string })[]
}

/** A cluster and an instance found by id in the cluster list. */
export interface NameLookup {
  cluster: (id: string | null | undefined) => NamedCluster | undefined
  instance: (id: string | null | undefined) => (NamedInstance & { instance_id: string }) | undefined
}

/**
 * Finds clusters and instances by id in the cluster list (D14, D15), so a page
 * that only holds ids can show their names. A miss returns undefined and the
 * caller keeps the id.
 */
export function nameLookup(clusters: readonly LookupCluster[] | undefined): NameLookup {
  const byCluster = new Map<string, NamedCluster>()
  const byInstance = new Map<string, NamedInstance & { instance_id: string }>()
  for (const cluster of clusters ?? []) {
    byCluster.set(cluster.cluster_id, cluster)
    for (const instance of cluster.instances) byInstance.set(instance.instance_id, instance)
  }
  return {
    cluster: (id) => (id ? byCluster.get(id) : undefined),
    instance: (id) => (id ? byInstance.get(id) : undefined),
  }
}

/** A cluster id as text: its name when the list knows one, else the id. */
export function clusterNameOrId(lookup: NameLookup, id: string): string {
  const cluster = lookup.cluster(id)
  return cluster ? clusterLabel(cluster, id) : id
}

/** An instance id as text: its display name or address when the list knows it, else the id. */
export function instanceNameOrId(lookup: NameLookup, id: string): string {
  const instance = lookup.instance(id)
  return instance ? instanceLabel(instance) : id
}
