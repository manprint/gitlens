import { fireEvent, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it, vi } from 'vitest'

vi.mock('echarts-for-react/esm/core', () => ({ default: () => null }))

import { buildIndexRows } from '@/lib/indexAnalysis'
import { captionOf, type CaptionId } from '@/lib/captions'
import { expectCaptionedCards, expectClearCard } from '@/test/clearCard'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { base as clusters } from '@/test/fixtures/getClusters'
import { base as indexHistory } from '@/test/fixtures/getIndexHistory'
import { base as suggestions } from '@/test/fixtures/getIndexSuggestions'
import { base as tableHistory } from '@/test/fixtures/getInstanceTableHistory'
import { base as search } from '@/test/fixtures/searchInstance'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'

import IndexAnalysisPage from './IndexAnalysisPage'
import { IndexDetail } from './IndexDetail'

function index(name: string, extra: Record<string, unknown> = {}) {
  return {
    datname: 'app',
    schemaname: 'public',
    relname: 'orders',
    indexrelname: name,
    index_bytes: 1_000_000,
    idx_scan_rate: 0.5,
    is_primary: false,
    is_unique: false,
    is_valid: true,
    last_scan_status: 'scanned',
    last_scan_at: '2026-08-27T01:00:00Z',
    definition: `CREATE INDEX ${name} ON public.orders USING btree (a)`,
    ts: '2026-08-27T02:00:00Z',
    ...extra,
  }
}

const ITEMS = [
  index('orders_pkey', { is_primary: true, is_unique: true }),
  index('orders_old_idx', { last_scan_status: 'never_since_reset' }),
  index('orders_bad_idx', { is_valid: false }),
]

const TABLES = [
  {
    datname: 'app',
    schemaname: 'public',
    relname: 'orders',
    n_live_tup: 30_000,
    n_dead_tup: 12_000,
    total_bytes: 100_000_000,
    table_bytes: 80_000_000,
    n_tup_ins_rate: 2,
    n_tup_upd_rate: 10,
    n_tup_hot_upd_rate: 8,
    n_tup_del_rate: 1,
    ts: '2026-08-27T02:00:00Z',
  },
]

const FINDINGS = [
  {
    finding_id: 'f1',
    rule_id: 'index.invalid',
    severity: 'critical',
    state: 'open',
    scope: 'relation',
    datname: 'app',
    object_name: 'public.orders_bad_idx',
    title: 'Invalid index',
  },
  {
    finding_id: 'f2',
    rule_id: 'index.fk_unindexed',
    severity: 'warning',
    state: 'open',
    scope: 'relation',
    datname: 'app',
    object_name: 'public.orders',
    title: 'Foreign key without an index',
    evidence: { table: 'public.orders', constraint: 'fk', columns: 'a', referenced: 'x' },
  },
]

function renderRoute(route: string) {
  server.use(
    ok('getClusters', clusters),
    ok('getInstanceIndexes', {
      instance_id: INSTANCE_ID,
      items: ITEMS,
      truncated: false,
      relations_not_reported: null,
    }),
    ok('getInstanceTables', {
      instance_id: INSTANCE_ID,
      items: TABLES,
      truncated: false,
      relations_not_reported: null,
    }),
    ok('getInstanceSettings', { instance_id: INSTANCE_ID, settings: [] }),
    ok('getInstanceTableHistory', tableHistory),
    ok('getFindings', FINDINGS),
    ok('getIndexSuggestions', suggestions),
    http.get('*/api/v1/instances/:id/search', () => HttpResponse.json(search)),
  )
  return renderWithProviders(<IndexAnalysisPage />, { route })
}

async function pageIds(route: string) {
  const { container } = renderRoute(route)
  await flushNetwork(30)
  return { container, ids: expectCaptionedCards(container) }
}

function hint(label: string) {
  return screen.getByRole('button', { name: `About ${label}` })
}

function openHint(label: string, id: CaptionId) {
  fireEvent.click(hint(label))
  expect(screen.getAllByText(captionOf(id).what).length).toBeGreaterThan(0)
}

describe('index analysis captions', () => {
  it('CAPTION-PAGE-001 the inventory view is captioned', async () => {
    const { ids } = await pageIds('/indexes')
    expect(ids).toContain('indexes.inventory')
    openHint('Index inventory', 'indexes.inventory')
  })

  it('CAPTION-PAGE-002 the summary view captions every tile and breakdown', async () => {
    const { ids } = await pageIds('/indexes?view=summary')
    expect(ids).toEqual(
      expect.arrayContaining([
        'indexes.summary',
        'indexes.summary-primary',
        'indexes.summary-unique',
        'indexes.summary-partial',
        'indexes.summary-expression',
        'indexes.summary-composite',
        'indexes.summary-invalid',
        'index.duplicate',
        'index.redundant-prefix',
        'indexes.foreign-keys',
        'indexes.breakdown-method',
        'indexes.breakdown-database',
        'indexes.breakdown-tables',
      ]),
    )
    openHint('Index summary', 'indexes.summary')
  })

  it('CAPTION-PAGE-003 the findings view is captioned', async () => {
    const { ids } = await pageIds('/indexes?view=findings')
    expect(ids).toContain('indexes.findings')
    openHint('Index findings', 'indexes.findings')
  })

  it('CAPTION-PAGE-004 the foreign keys view is captioned', async () => {
    const { ids } = await pageIds('/indexes?view=foreign-keys')
    expect(ids).toContain('indexes.foreign-keys')
    openHint('Unindexed foreign keys', 'indexes.foreign-keys')
  })

  it('CAPTION-PAGE-005 the cleanup tab is captioned', async () => {
    const { ids } = await pageIds('/indexes?tab=cleanup')
    expect(ids).toContain('indexes.cleanup')
    openHint('Indexes that look removable', 'indexes.cleanup')
  })

  it('CAPTION-PAGE-006 the dead rows tab is captioned', async () => {
    const { ids } = await pageIds('/indexes?tab=dead-rows')
    expect(ids).toContain('indexes.dead-rows')
    openHint('Tables by dead rows', 'indexes.dead-rows')
  })

  it('CAPTION-PAGE-007 the suggestions tab is captioned', async () => {
    const { ids } = await pageIds('/indexes?tab=suggestions')
    expect(ids).toContain('indexes.suggestions')
    openHint('Index suggestions', 'indexes.suggestions')
  })

  it('CAPTION-PAGE-008 the index tax tab is captioned', async () => {
    const { ids } = await pageIds('/indexes?tab=tax')
    expect(ids).toContain('indexes.tax')
    openHint('Index tax', 'indexes.tax')
  })

  it('CAPTION-PAGE-009 the search results are captioned and show their caption', async () => {
    const { ids } = await pageIds(`/indexes?instance=${INSTANCE_ID}&q=orders`)
    expect(ids).toContain('indexes.search')
    expect(screen.getAllByText(captionOf('indexes.search').what).length).toBeGreaterThan(0)
  })

  it('CAPTION-PAGE-010 the index detail charts are captioned', async () => {
    server.use(ok('getIndexHistory', indexHistory))
    const row = buildIndexRows([index('orders_idx')], [])[0]!
    const { container } = renderWithProviders(<IndexDetail instanceId={INSTANCE_ID} row={row} />, {
      route: '/indexes?range=6h',
    })
    await flushNetwork(30)

    expect(expectCaptionedCards(container)).toEqual(['index.history-scans', 'index.history-size'])
    expect(screen.getAllByText(captionOf('index.history-scans').what).length).toBeGreaterThan(0)
  })

  it('CAPTION-PAGE-011 every figure card of every tab names its unit', async () => {
    for (const route of [
      '/indexes',
      '/indexes?view=findings',
      '/indexes?view=foreign-keys',
      '/indexes?tab=cleanup',
      '/indexes?tab=dead-rows',
      '/indexes?tab=suggestions',
      '/indexes?tab=tax',
    ]) {
      const { container, unmount } = renderRoute(route)
      await flushNetwork(30)
      const articles = within(container).getAllByRole('article')
      expect(articles.length, route).toBeGreaterThan(0)
      for (const article of articles) expectClearCard(article)
      unmount()
    }
  })
})
