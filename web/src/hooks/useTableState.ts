import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { DEFAULT_PAGE_SIZE, PAGE_SIZES, type SortDirection } from '@/lib/tableControls'

/** Search, sort, page and filters of one table (D12, D23). */
export interface TableState {
  q: string
  /** Sorted column id, or null for the data's own order. */
  sort: string | null
  dir: SortDirection
  /** 1-based; tables clamp it to their page count. */
  page: number
  size: number
  /** Filter id → value (`multi` values are comma-separated). */
  filters: Readonly<Record<string, string>>
}

export interface TableDefaults {
  sort?: string | null
  dir?: SortDirection
  size?: number
}

export type TableStatePatch = Partial<TableState>

export interface TableStateControls {
  state: TableState
  /** Apply a change; any change other than `page` returns to page 1. */
  set: (patch: TableStatePatch) => void
  /** Clear the search and every filter; sort and page size stay. */
  clear: () => void
  /** Active filters, the search counting as one. */
  activeFilterCount: number
}

/** A cleared sort when the table has a default one: the data's own order. */
const NO_SORT = 'none'

function resolved(defaults: TableDefaults): Required<TableDefaults> {
  return {
    sort: defaults.sort ?? null,
    dir: defaults.dir ?? 'asc',
    size: defaults.size ?? DEFAULT_PAGE_SIZE,
  }
}

export function initialTableState(defaults: TableDefaults = {}): TableState {
  const { sort, dir, size } = resolved(defaults)
  return { q: '', sort, dir, page: 1, size, filters: {} }
}

const key = (prefix: string, name: string) => `${prefix}.${name}`

/** The table state stored under `<prefix>.*`; absent or invalid values fall back to the defaults. */
export function readTableState(
  params: URLSearchParams,
  prefix: string,
  defaults: TableDefaults = {},
): TableState {
  const fallback = resolved(defaults)
  const rawSort = params.get(key(prefix, 'sort'))
  const sort =
    rawSort === null || rawSort === '' ? fallback.sort : rawSort === NO_SORT ? null : rawSort
  const rawDir = params.get(key(prefix, 'dir'))
  const dir: SortDirection = rawDir === 'asc' || rawDir === 'desc' ? rawDir : fallback.dir
  const rawSize = Number(params.get(key(prefix, 'size')))
  const size = (PAGE_SIZES as readonly number[]).includes(rawSize) ? rawSize : fallback.size
  const rawPage = Number(params.get(key(prefix, 'page')))
  const page = Number.isInteger(rawPage) && rawPage >= 1 ? rawPage : 1

  const filters: Record<string, string> = {}
  const filterPrefix = key(prefix, 'f.')
  for (const [name, value] of params) {
    if (name.startsWith(filterPrefix) && value !== '') {
      filters[name.slice(filterPrefix.length)] = value
    }
  }
  return { q: params.get(key(prefix, 'q')) ?? '', sort, dir, page, size, filters }
}

function sameFilters(a: TableState['filters'], b: TableState['filters']): boolean {
  const keys = new Set([...Object.keys(a), ...Object.keys(b)])
  return [...keys].every((name) => (a[name] ?? '') === (b[name] ?? ''))
}

/** `current` with `patch` applied; a change to anything but `page` resets the page to 1. */
export function nextTableState(current: TableState, patch: TableStatePatch): TableState {
  const next: TableState = { ...current, ...patch, filters: patch.filters ?? current.filters }
  const changed =
    next.q !== current.q ||
    next.sort !== current.sort ||
    next.dir !== current.dir ||
    next.size !== current.size ||
    !sameFilters(next.filters, current.filters)
  return changed ? { ...next, page: 1 } : next
}

/**
 * `params` with the `<prefix>.*` keys rewritten for `state`. Default values
 * are never written (an absent key means the default), so other tables'
 * keys and unrelated parameters stay as they are.
 */
export function writeTableState(
  params: URLSearchParams,
  prefix: string,
  state: TableState,
  defaults: TableDefaults = {},
): URLSearchParams {
  const fallback = resolved(defaults)
  const next = new URLSearchParams(params)
  for (const name of [...next.keys()]) {
    if (name.startsWith(`${prefix}.`)) next.delete(name)
  }

  const q = state.q.trim() === '' ? '' : state.q
  if (q !== '') next.set(key(prefix, 'q'), q)
  const defaultOrder =
    state.sort === fallback.sort && (state.sort === null || state.dir === fallback.dir)
  if (!defaultOrder) {
    next.set(key(prefix, 'sort'), state.sort ?? NO_SORT)
    if (state.sort !== null) next.set(key(prefix, 'dir'), state.dir)
  }
  if (state.page > 1) next.set(key(prefix, 'page'), String(state.page))
  if (state.size !== fallback.size) next.set(key(prefix, 'size'), String(state.size))
  for (const [id, value] of Object.entries(state.filters)) {
    if (value !== '') next.set(key(prefix, `f.${id}`), value)
  }
  return next
}

export function countActiveFilters(state: TableState): number {
  const filters = Object.values(state.filters).filter((value) => value !== '').length
  return filters + (state.q.trim() === '' ? 0 : 1)
}

/** Table state kept in the URL under `<prefix>.*`, written with `replace` navigation. */
export function useTableState(prefix: string, defaults: TableDefaults = {}): TableStateControls {
  const [params, setParams] = useSearchParams()
  const state = readTableState(params, prefix, defaults)

  function set(patch: TableStatePatch) {
    setParams(
      (current) =>
        writeTableState(
          current,
          prefix,
          nextTableState(readTableState(current, prefix, defaults), patch),
          defaults,
        ),
      { replace: true },
    )
  }

  return {
    state,
    set,
    clear: () => set({ q: '', filters: {} }),
    activeFilterCount: countActiveFilters(state),
  }
}

/** The same controls kept in component state, for tables without a URL id (no Router needed). */
export function useLocalTableState(defaults: TableDefaults = {}): TableStateControls {
  const [state, setState] = useState(() => initialTableState(defaults))

  function set(patch: TableStatePatch) {
    setState((current) => nextTableState(current, patch))
  }

  return {
    state,
    set,
    clear: () => set({ q: '', filters: {} }),
    activeFilterCount: countActiveFilters(state),
  }
}
