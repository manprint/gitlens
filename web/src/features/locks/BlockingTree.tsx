import { useMemo, useRef, useState, type KeyboardEvent } from 'react'
import { useParams } from 'react-router-dom'

import { useInstance, useInstanceActivity, useLocks } from '@/api/queries'
import type { PermTier } from '@/api/types'
import { PageHeader } from '@/components/layout/PageHeader'
import { Section } from '@/components/layout/Section'
import { Degraded, EmptyState, ErrorState, Stale, Truncated } from '@/components/state'
import { ActivitySection, type ActivityResponseLike } from './ActivitySection'
import { SignalActions } from './SignalActions'
import { formatDuration, formatRelative, truncateQuery } from '@/lib/format'
import {
  buildBlockingTree,
  rankRoots,
  waitDuration,
  type BlockingTree,
  type BlockingTreeNode,
  type LockNode,
  type LockPID,
} from '@/lib/locks'

const QUERY_BYTE_BUDGET = 2048
const SAMPLE_INTERVAL_SECONDS = 10
const STALE_THRESHOLD_SECONDS = 20

export interface LocksResponseLike {
  sampled_at: string | null
  stale: boolean
  nodes: readonly unknown[]
}

interface TreeEntry {
  key: string
  parentKey: string | undefined
  level: number
  tree: BlockingTreeNode
}

interface TreeItemProps {
  entry: TreeEntry
  currentTier: PermTier
  instanceId: string | undefined
  sampledAt: string | null
  expanded: boolean
  onKeyDown: (event: KeyboardEvent<HTMLLIElement>, entry: TreeEntry) => void
  onToggle: (key: string) => void
  registerRef: (key: string, element: HTMLLIElement | null) => void
  expandedKeys: ReadonlySet<string>
}

function pidFrom(value: unknown): LockPID | null {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim() !== '') return value
  return null
}

function normalizeNode(value: unknown): LockNode | null {
  if (typeof value !== 'object' || value === null) return null
  const record = value as Record<string, unknown>
  const pid = pidFrom(record.pid)
  if (pid === null) return null

  const blockedBy = Array.isArray(record.blocked_by)
    ? record.blocked_by.map(pidFrom).filter((candidate): candidate is LockPID => candidate !== null)
    : []

  return { ...record, pid, blocked_by: blockedBy }
}

function nodeText(value: unknown): string {
  if (typeof value === 'string' && value.trim() !== '') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return '—'
}

function queryBytes(query: string): number {
  return new TextEncoder().encode(query).byteLength
}

function sampleAge(sampledAt: string | null, now: Date): number | null {
  if (sampledAt === null) return null
  const timestamp = new Date(sampledAt).getTime()
  if (!Number.isFinite(timestamp)) return null
  return Math.max(0, (now.getTime() - timestamp) / 1000)
}

function itemKey(parentKey: string | undefined, tree: BlockingTreeNode, index: number): string {
  return `${parentKey ?? 'root'}/${String(tree.node.pid)}-${index}`
}

function expandableKeys(forest: BlockingTree): Set<string> {
  const keys = new Set<string>()
  const visit = (tree: BlockingTreeNode, parentKey: string | undefined, index: number) => {
    const key = itemKey(parentKey, tree, index)
    if (!tree.cycle && tree.children.length > 0) {
      keys.add(key)
      tree.children.forEach((child, childIndex) => visit(child, key, childIndex))
    }
  }
  forest.forEach((tree, index) => visit(tree, undefined, index))
  return keys
}

function visibleEntries(forest: BlockingTree, expandedKeys: ReadonlySet<string>): TreeEntry[] {
  const entries: TreeEntry[] = []
  const visit = (
    tree: BlockingTreeNode,
    parentKey: string | undefined,
    level: number,
    index: number,
  ) => {
    const key = itemKey(parentKey, tree, index)
    entries.push({ key, parentKey, level, tree })
    if (!tree.cycle && expandedKeys.has(key)) {
      tree.children.forEach((child, childIndex) => visit(child, key, level + 1, childIndex))
    }
  }
  forest.forEach((tree, index) => visit(tree, undefined, 1, index))
  return entries
}

function LockDetails({ node, sampledAt }: { node: LockNode; sampledAt: string | null }) {
  const query = typeof node.query === 'string' ? node.query : ''
  const bytes = queryBytes(query)
  const shownQuery = truncateQuery(query, QUERY_BYTE_BUDGET)
  const backendMarkedTruncated = node.query_truncated === true || node.truncated === true
  const queryWasTruncated = backendMarkedTruncated || bytes >= QUERY_BYTE_BUDGET

  return (
    <div className="mt-2 grid gap-x-6 gap-y-1 text-sm sm:grid-cols-2 lg:grid-cols-4">
      <div>
        <span className="text-muted-foreground">User</span>
        <span className="block">{nodeText(node.usename ?? node.username)}</span>
      </div>
      <div>
        <span className="text-muted-foreground">Application</span>
        <span className="block">{nodeText(node.application_name)}</span>
      </div>
      <div>
        <span className="text-muted-foreground">Database</span>
        <span className="block">{nodeText(node.datname ?? node.database)}</span>
      </div>
      <div>
        <span className="text-muted-foreground">State</span>
        <span className="block">{nodeText(node.state)}</span>
      </div>
      <div>
        <span className="text-muted-foreground">Wait event</span>
        <span className="block">
          {nodeText(node.wait_event_type)} / {nodeText(node.wait_event)}
        </span>
      </div>
      <div>
        <span className="text-muted-foreground">Lock mode</span>
        <span className="block">{nodeText(node.lock_mode ?? node.mode)}</span>
      </div>
      <div>
        <span className="text-muted-foreground">Wait duration</span>
        <span className="block">{formatDuration(waitDuration(node, sampledAt))}</span>
      </div>
      <div className="sm:col-span-2 lg:col-span-4">
        <span className="text-muted-foreground">Query</span>
        <span className="block">
          {query ? <code className="break-words">{shownQuery}</code> : <span>—</span>}
          {queryWasTruncated ? (
            <Truncated
              shown={Math.min(bytes, QUERY_BYTE_BUDGET)}
              budget={QUERY_BYTE_BUDGET}
              scope="query bytes"
            />
          ) : null}
        </span>
      </div>
    </div>
  )
}

function TreeItem({
  entry,
  currentTier,
  instanceId,
  sampledAt,
  expanded,
  onKeyDown,
  onToggle,
  registerRef,
  expandedKeys,
}: TreeItemProps) {
  const { tree } = entry
  const hasChildren = !tree.cycle && tree.children.length > 0
  const pid = String(tree.node.pid)

  return (
    <li
      ref={(element) => registerRef(entry.key, element)}
      role="treeitem"
      aria-level={entry.level}
      aria-expanded={hasChildren ? expanded : undefined}
      tabIndex={0}
      onKeyDown={(event) => onKeyDown(event, entry)}
      className="focus-visible:ring-ring rounded-md border p-3 focus-visible:ring-2 focus-visible:outline-none"
    >
      <div className="flex items-start gap-2">
        {hasChildren ? (
          <button
            type="button"
            className="mt-0.5 inline-flex size-6 shrink-0 items-center justify-center rounded border text-sm"
            aria-label={`${expanded ? 'Collapse' : 'Expand'} lock session ${pid}`}
            onClick={() => onToggle(entry.key)}
          >
            {expanded ? '−' : '+'}
          </button>
        ) : (
          <span
            aria-hidden="true"
            className="inline-flex size-6 shrink-0 items-center justify-center"
          >
            •
          </span>
        )}
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
            <strong>PID {pid}</strong>
            {tree.cycle ? <span className="text-destructive">Cycle detected</span> : null}
            <span className="text-muted-foreground">
              {tree.cycle ? 'blocking relationship repeats this session' : 'blocking session'}
            </span>
          </div>
          {tree.cycle ? (
            <p className="text-muted-foreground mt-1 text-sm">
              The blocking relationship forms a cycle; expansion stops at this marker.
            </p>
          ) : (
            <>
              <LockDetails node={tree.node} sampledAt={sampledAt} />
              {instanceId ? (
                <SignalActions node={tree.node} currentTier={currentTier} instanceId={instanceId} />
              ) : null}
            </>
          )}
        </div>
      </div>
      {hasChildren && expanded ? (
        <ul role="group" className="mt-3 space-y-2 border-l pl-4">
          {tree.children.map((child, index) => {
            const childEntry: TreeEntry = {
              key: itemKey(entry.key, child, index),
              parentKey: entry.key,
              level: entry.level + 1,
              tree: child,
            }
            return (
              <TreeItem
                key={childEntry.key}
                entry={childEntry}
                currentTier={currentTier}
                instanceId={instanceId}
                sampledAt={sampledAt}
                expanded={expandedKeys.has(childEntry.key)}
                onKeyDown={onKeyDown}
                onToggle={onToggle}
                registerRef={registerRef}
                expandedKeys={expandedKeys}
              />
            )
          })}
        </ul>
      ) : null}
    </li>
  )
}

export function BlockingTree({
  response,
  now = new Date(),
  currentTier = 'T0',
  instanceId,
}: {
  response: LocksResponseLike
  now?: Date
  currentTier?: PermTier
  instanceId?: string
}) {
  const normalizedNodes = useMemo(
    () => response.nodes.map(normalizeNode).filter((node): node is LockNode => node !== null),
    [response.nodes],
  )
  const forest = useMemo(
    () => rankRoots(buildBlockingTree(normalizedNodes), response.sampled_at ?? undefined),
    [normalizedNodes, response.sampled_at],
  )
  const initialExpanded = useMemo(() => expandableKeys(forest), [forest])
  const [expandedKeys, setExpandedKeys] = useState<Set<string>>(initialExpanded)
  const itemRefs = useRef(new Map<string, HTMLLIElement>())

  if (response.sampled_at === null) {
    return (
      <Degraded
        reason={`No lock sample has been stored for this instance yet. Locks are sampled every ${SAMPLE_INTERVAL_SECONDS} seconds; shorter episodes can be missed.`}
      />
    )
  }

  const age = sampleAge(response.sampled_at, now)
  const entries = visibleEntries(forest, expandedKeys)

  if (forest.length === 0) {
    return (
      <Section
        title="Blocking tree"
        description={
          <>
            Latest sample: {formatRelative(response.sampled_at, now)}; sample age:{' '}
            {formatDuration(age)}; sampled every {SAMPLE_INTERVAL_SECONDS} seconds.
          </>
        }
      >
        <EmptyState
          title="No lock contention in the latest sample"
          description={`No blocked sessions were present in the most recent ${SAMPLE_INTERVAL_SECONDS}-second sample.`}
        />
      </Section>
    )
  }

  const handleToggle = (key: string) => {
    setExpandedKeys((current) => {
      const next = new Set(current)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  const focusEntry = (key: string | undefined) => {
    if (key) itemRefs.current.get(key)?.focus()
  }

  const handleKeyDown = (event: KeyboardEvent<HTMLLIElement>, entry: TreeEntry) => {
    const currentIndex = entries.findIndex((candidate) => candidate.key === entry.key)
    const isExpanded = expandedKeys.has(entry.key)
    const hasChildren = !entry.tree.cycle && entry.tree.children.length > 0

    if (event.key === 'ArrowRight' && hasChildren) {
      event.preventDefault()
      if (!isExpanded) handleToggle(entry.key)
      else focusEntry(entries[currentIndex + 1]?.key)
    } else if (event.key === 'ArrowLeft') {
      event.preventDefault()
      if (hasChildren && isExpanded) handleToggle(entry.key)
      else focusEntry(entry.parentKey)
    } else if (event.key === 'ArrowDown') {
      event.preventDefault()
      focusEntry(entries[currentIndex + 1]?.key)
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      focusEntry(entries[currentIndex - 1]?.key)
    } else if (event.key === 'Home') {
      event.preventDefault()
      focusEntry(entries[0]?.key)
    } else if (event.key === 'End') {
      event.preventDefault()
      focusEntry(entries.at(-1)?.key)
    }
  }

  return (
    <Section
      title="Blocking tree"
      description={
        <>
          Latest sample: {formatRelative(response.sampled_at, now)}; sample age:{' '}
          {formatDuration(age)}. Samples are collected every {SAMPLE_INTERVAL_SECONDS} seconds;
          shorter episodes can be missed.
        </>
      }
    >
      {response.stale && age !== null ? (
        <Stale age={age} threshold={STALE_THRESHOLD_SECONDS}>
          <span>Lock data is older than the freshness threshold.</span>
        </Stale>
      ) : null}
      <ul role="tree" aria-label="Blocking lock tree" className="space-y-2">
        {forest.map((tree, index) => {
          const entry: TreeEntry = {
            key: itemKey(undefined, tree, index),
            parentKey: undefined,
            level: 1,
            tree,
          }
          return (
            <TreeItem
              key={entry.key}
              entry={entry}
              currentTier={currentTier}
              instanceId={instanceId}
              sampledAt={response.sampled_at}
              expanded={expandedKeys.has(entry.key)}
              onKeyDown={handleKeyDown}
              onToggle={handleToggle}
              registerRef={(key, element) => {
                if (element) itemRefs.current.set(key, element)
                else itemRefs.current.delete(key)
              }}
              expandedKeys={expandedKeys}
            />
          )
        })}
      </ul>
    </Section>
  )
}

export default LocksPage

export interface LocksPageProps {
  instanceId?: string
}

export function LocksPage({ instanceId: explicitInstanceId }: LocksPageProps = {}) {
  const { instanceId: routeInstanceId } = useParams<{ instanceId: string }>()
  const instanceId = explicitInstanceId ?? routeInstanceId
  const instanceQuery = useInstance(instanceId ?? '')
  const locksQuery = useLocks(instanceId ?? '')
  const activityQuery = useInstanceActivity(instanceId ?? '')
  const response = locksQuery.data as LocksResponseLike | undefined
  const activityResponse = activityQuery.data as ActivityResponseLike | undefined

  if (!instanceId) {
    return <p role="status">Select an instance to inspect locks.</p>
  }

  return (
    <div className="space-y-8">
      <PageHeader
        title="Locks and activity"
        subtitle="Blocking sessions from the latest stored lock sample."
      />
      {locksQuery.isPending && response === undefined ? (
        <p role="status">Loading lock sample…</p>
      ) : locksQuery.isError && response === undefined ? (
        <ErrorState
          failure={locksQuery.error}
          onRetry={() => void locksQuery.refetch()}
          endpoint="locks"
        />
      ) : response ? (
        <>
          <BlockingTree
            response={response}
            currentTier={instanceQuery.data?.perm_tier ?? 'T0'}
            instanceId={instanceId}
          />
          {activityQuery.isError && activityResponse === undefined ? (
            <ErrorState
              failure={activityQuery.error}
              onRetry={() => void activityQuery.refetch()}
              endpoint="activity"
            />
          ) : activityResponse ? (
            <ActivitySection response={activityResponse} />
          ) : (
            <p role="status">Loading activity sample…</p>
          )}
        </>
      ) : (
        <p role="status">Waiting for the first lock sample…</p>
      )}
    </div>
  )
}
