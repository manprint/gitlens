import { useId, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useIsFetching } from '@tanstack/react-query'

import { useInstanceSearch } from '@/api/queries'
import type { Schemas } from '@/api/types'
import {
  FULL_SEARCH_HINT,
  HighlightedText,
  MARK_CLASS,
  SearchBox,
} from '@/components/layout/SearchBox'
import { Section } from '@/components/layout/Section'
import { ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useShownTerm } from '@/hooks/useSearch'
import { useTimeRange } from '@/hooks/useTimeRange'
import { formatCount } from '@/lib/format'
import { searchTermOf } from '@/lib/highlight'
import { queryWindowKey, queryWindowOf, resolveQueryWindow } from '@/lib/timerange'

type SearchGroup = Schemas['SearchGroup']
type SearchHit = Schemas['SearchHit']

/** The groups in the order the panel shows them, with their names (D20). */
const SEARCH_GROUPS: readonly { kind: SearchGroup['kind']; label: string }[] = [
  { kind: 'indexes', label: 'Indexes' },
  { kind: 'tables', label: 'Tables' },
  { kind: 'columns', label: 'Columns' },
  { kind: 'statements', label: 'Queries' },
]

/** The URL keys of the page's time range (useTimeRange). */
const RANGE_KEYS = ['range', 'from', 'to'] as const

/**
 * The link of a hit in the page's own time range: the server names none for
 * an index or table and resolved bounds for a statement, so the range keys of
 * the current URL replace them (a relative range stays relative).
 */
function linkInRange(link: string, current: URLSearchParams): string {
  const [path = link, query = ''] = link.split('?')
  const params = new URLSearchParams(query)
  for (const key of RANGE_KEYS) params.delete(key)
  for (const key of RANGE_KEYS) {
    const value = current.get(key)
    if (value !== null) params.set(key, value)
  }
  const search = params.toString()
  return search === '' ? path : `${path}?${search}`
}

/** The page's `?q=` and how to change it (replace navigation, D12). */
function useSearchParam() {
  const [searchParams, setSearchParams] = useSearchParams()
  const q = searchParams.get('q') ?? ''
  function setSearch(value: string) {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current)
        if (value.trim() === '') next.delete('q')
        else next.set('q', value)
        return next
      },
      { replace: true },
    )
  }
  return { q, term: searchTermOf(q), setSearch, searchParams }
}

interface SearchEverythingProps {
  instanceId: string
  /** The database picked on the page; empty for all of them. */
  database: string
}

/**
 * The "Search everything" box of Index analysis (D20). It sits on the page
 * title line with the tabs; the results show in `SearchEverythingResults`.
 */
export function SearchEverythingBox({ instanceId }: Pick<SearchEverythingProps, 'instanceId'>) {
  const { q, term, setSearch } = useSearchParam()
  const fetching = useIsFetching({ queryKey: ['searchInstance', instanceId] }) > 0
  return (
    <SearchBox
      busy={fetching && term !== ''}
      boxClassName="min-[1600px]:w-48 min-[1800px]:w-56"
      hint={FULL_SEARCH_HINT}
      id="index-search"
      label="Search everything"
      onChange={setSearch}
      placeholder="Indexes, tables, columns, queries"
      value={q}
    />
  )
}

/**
 * The bounds of the searched window. A relative range is resolved when the
 * term or the range changes and then kept, so the request is not repeated at
 * every render with a moving clock (adjusting state while rendering).
 */
function useSearchBounds(term: string): { from: string; to: string } {
  const { range } = useTimeRange()
  const window = queryWindowOf(range)
  const id = `${term}|${queryWindowKey(window)}`
  const [snapshot, setSnapshot] = useState(() => ({ id, bounds: resolveQueryWindow(window) }))
  if (snapshot.id !== id) {
    const next = { id, bounds: resolveQueryWindow(window) }
    setSnapshot(next)
    return next.bounds
  }
  return snapshot.bounds
}

function Snippet({ parts }: { parts: SearchHit['snippet'] }) {
  return (
    <>
      {parts.map((part, index) =>
        part.match ? (
          <mark key={index} className={MARK_CLASS}>
            {part.text}
          </mark>
        ) : (
          part.text
        ),
      )}
    </>
  )
}

interface GroupProps {
  group: SearchGroup
  label: string
  term: string
  /** The current URL, whose time range the links keep. */
  current: URLSearchParams
}

function Group({ group, label, term, current }: GroupProps) {
  const titleId = `${useId().replaceAll(':', '')}-group`
  return (
    <section aria-labelledby={titleId} className="min-w-0 space-y-1.5">
      <h3 className="flex items-center gap-2 text-sm font-semibold" id={titleId}>
        {label}
        <Badge variant="secondary">
          {formatCount(group.total)}
          <span className="sr-only">{group.total === 1 ? ' match' : ' matches'}</span>
        </Badge>
      </h3>
      {group.truncated ? (
        <p className="text-muted-foreground text-xs">
          Showing {formatCount(group.hits.length)} of {formatCount(group.total)}
        </p>
      ) : null}
      {group.hits.length === 0 ? (
        <p className="text-muted-foreground text-xs">No match.</p>
      ) : (
        // From 1600 px the lists stay short, so the tab's table header stays on a Full HD screen.
        <ul
          aria-label={`${label} found`}
          className="max-h-64 space-y-0.5 overflow-auto pr-1 min-[1600px]:max-h-40"
        >
          {group.hits.map((hit) => (
            <li key={hit.key}>
              <Link
                className="hover:bg-muted focus-visible:ring-ring/50 block min-w-0 rounded-md px-2 py-1 outline-none focus-visible:ring-[3px]"
                to={linkInRange(hit.link, current)}
              >
                <span className="text-primary block truncate text-sm font-medium">
                  <HighlightedText q={term} text={hit.title} />
                </span>
                <span className="text-muted-foreground block truncate text-xs">{hit.subtitle}</span>
                <span
                  className="text-muted-foreground block truncate font-mono text-xs"
                  title={hit.snippet.map((part) => part.text).join('')}
                >
                  <Snippet parts={hit.snippet} />
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

/**
 * The grouped results of "Search everything" (D20): indexes, tables, columns
 * and queries, each with its full match count and a link that opens the hit
 * on its tab (`?tab=&focus=`) or the Slowest queries page. Shown above the
 * tabs' content while a term of 2 characters or more is searched.
 */
export function SearchEverythingResults({ instanceId, database }: SearchEverythingProps) {
  const { term, setSearch, searchParams } = useSearchParam()
  const bounds = useSearchBounds(term)
  const searchQuery = useInstanceSearch(instanceId, {
    q: term,
    ...bounds,
    ...(database === '' ? {} : { database }),
  })
  // The term the shown groups answer: the previous one while a new search loads (I-9).
  const shownTerm = useShownTerm(term, searchQuery.isPlaceholderData, searchQuery.isSuccess)
  if (term === '') return null

  const data = searchQuery.data
  const groups = SEARCH_GROUPS.flatMap(({ kind, label }) => {
    const group = data?.groups.find((candidate) => candidate.kind === kind)
    return group ? [{ group, label }] : []
  })
  const nothing = data !== undefined && groups.every(({ group }) => group.total === 0)

  return (
    <Section captionId="indexes.search" title={`Search results for “${shownTerm}”`}>
      {data === undefined ? (
        searchQuery.error ? (
          <ErrorState
            endpoint="search"
            failure={searchQuery.error}
            onRetry={() => void searchQuery.refetch()}
          />
        ) : (
          <div aria-busy="true" role="status">
            Searching…
          </div>
        )
      ) : nothing ? (
        <div className="flex flex-wrap items-center gap-3 text-sm" role="status">
          <span>
            Nothing matches “{shownTerm}” in this range: no index, table, column or query collected
            for this server.
          </span>
          <Button
            onClick={() => {
              setSearch('')
              // The button goes with the results: the search box keeps the keyboard.
              document.getElementById('index-search')?.focus()
            }}
            size="sm"
            type="button"
            variant="outline"
          >
            Clear the search
          </Button>
        </div>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {groups.map(({ group, label }) => (
            <Group
              key={group.kind}
              current={searchParams}
              group={group}
              label={label}
              term={shownTerm}
            />
          ))}
        </div>
      )}
    </Section>
  )
}
