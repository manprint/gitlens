import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { resetPreferences } from '@/lib/preferences'
import { expectNoA11yViolations } from '@/test/a11y'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'
import { dataRows, sortTable } from '@/test/table'

import { AutovacuumActivity } from './AutovacuumRuns'

const INSTANCE_ID = '11111111-1111-4111-8111-111111111111'
const SUMMARY = 'Autovacuum activity'

function table(relname: string, total: number, runs: number, wraparound = 0) {
  return {
    datname: 'app',
    schemaname: 'public',
    relname,
    runs,
    total_elapsed_seconds: total,
    max_elapsed_seconds: total / runs,
    tuples_removed: runs * 100,
    last_ts: '2026-11-20T10:00:00Z',
    wraparound_runs: wraparound,
  }
}

function run(ts: string, wraparound: boolean, aggressive: boolean) {
  return {
    ts,
    datname: 'app',
    schemaname: 'public',
    relname: 'orders',
    kind: 'vacuum',
    aggressive,
    wraparound,
    index_scans: 1,
    pages_removed: 0,
    pages_remain: 120,
    pages_scanned: 40,
    tuples_removed: 900,
    tuples_remain: 5000,
    tuples_dead_not_removable: 3,
    buffer_hits: 300,
    buffer_reads: 12,
    buffer_dirtied: 8,
    wal_records: 410,
    wal_fpi: 6,
    wal_bytes: 2048,
    cpu_user_seconds: 0.01,
    cpu_system_seconds: 0,
    elapsed_seconds: 1.5,
  }
}

function serve(tables: ReturnType<typeof table>[], runs: ReturnType<typeof run>[] = []) {
  const runRequests: URLSearchParams[] = []
  server.use(
    http.get('*/api/v1/autovacuum/summary', () => HttpResponse.json({ tables })),
    http.get('*/api/v1/autovacuum/runs', ({ request }) => {
      runRequests.push(new URL(request.url).searchParams)
      return HttpResponse.json({ runs })
    }),
  )
  return runRequests
}

function renderActivity() {
  return renderWithProviders(<AutovacuumActivity instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}`,
  })
}

describe('AutovacuumActivity', () => {
  afterEach(async () => {
    resetPreferences()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('UI-AVH-001 lists tables by total time and sorts by another column', async () => {
    vi.useRealTimers()
    serve([table('small', 2, 8), table('big', 30, 3), table('mid', 10, 5)])
    const { container } = renderActivity()
    await waitFor(() => expect(dataRows(SUMMARY)).toHaveLength(3))
    const names = () =>
      dataRows(SUMMARY).map((row) => within(row).getByRole('rowheader').textContent)
    expect(names()).toEqual(['app · public.big', 'app · public.mid', 'app · public.small'])
    expect(dataRows(SUMMARY)[0]).toHaveTextContent('30s')
    sortTable(SUMMARY, 'Runs', 'descending')
    expect(names()[0]).toBe('app · public.small')
    await expectNoA11yViolations(container)
  })

  it('UI-AVH-002 a row opens the runs of its table with the wraparound badge', async () => {
    vi.useRealTimers()
    const requests = serve(
      [table('orders', 3, 2, 1)],
      [run('2026-11-20T10:00:00Z', true, true), run('2026-11-19T10:00:00Z', false, false)],
    )
    renderActivity()
    fireEvent.click(
      await screen.findByRole('button', { name: 'Show the runs of app · public.orders' }),
    )
    const runsTable = 'Runs of app · public.orders'
    await waitFor(() => expect(dataRows(runsTable)).toHaveLength(2))
    expect(requests[0]?.get('database')).toBe('app')
    expect(requests[0]?.get('schema')).toBe('public')
    expect(requests[0]?.get('relation')).toBe('orders')
    const [first, second] = dataRows(runsTable)
    expect(first).toHaveTextContent('to prevent wraparound')
    expect(first).toHaveTextContent('aggressive')
    expect(first).toHaveTextContent('2.0 KiB')
    expect(second).not.toHaveTextContent('to prevent wraparound')
  })

  it('UI-AVH-003 an empty history names both settings', async () => {
    vi.useRealTimers()
    serve([])
    renderActivity()
    const setup = await screen.findByTestId('autovacuum-setup')
    expect(setup).toHaveTextContent('checks.logs.enabled')
    expect(setup).toHaveTextContent('log_autovacuum_min_duration')
  })

  it('UI-AVH-004 reads in Italian with the setting names in English', async () => {
    vi.useRealTimers()
    await setServerLanguage('it')
    await refreshLocale()
    serve([table('orders', 1.5, 1, 1)], [run('2026-11-20T10:00:00Z', true, false)])
    renderActivity()
    fireEvent.click(
      await screen.findByRole('button', { name: 'Mostra le esecuzioni di app · public.orders' }),
    )
    const runsTable = 'Esecuzioni di app · public.orders'
    await waitFor(() => expect(dataRows(runsTable)).toHaveLength(1))
    expect(dataRows(runsTable)[0]).toHaveTextContent('per prevenire il wraparound')
    expect(dataRows(runsTable)[0]).toHaveTextContent('1,5s')
    expect(screen.getByRole('table', { name: 'Attività di autovacuum' })).toBeInTheDocument()
  })
})
