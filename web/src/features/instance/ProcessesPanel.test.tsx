import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { resetPreferences } from '@/lib/preferences'
import { expectNoA11yViolations } from '@/test/a11y'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'
import { columnTexts, dataRows, sortTable } from '@/test/table'

import { ProcessesPanel } from './ProcessesPanel'

const INSTANCE_ID = '11111111-1111-4111-8111-111111111111'
const TABLE = 'PostgreSQL processes'
const IO_UNKNOWN = 'The agent cannot read I/O counters of processes owned by another user'

function proc(
  pid: number,
  cpu: number,
  rss: number,
  read: number | null,
  queryid: string | null = null,
) {
  return {
    pid,
    backend_type: pid === 100 ? 'postmaster' : 'client backend',
    datname: pid === 100 ? '' : 'app',
    usename: pid === 100 ? '' : 'app',
    application_name: '',
    state: pid === 100 ? '' : 'active',
    queryid,
    cpu_ratio: cpu,
    rss_bytes: rss,
    read_bytes_per_s: read,
    write_bytes_per_s: read,
  }
}

function serve(snapshot: { ts: string | null; processes: ReturnType<typeof proc>[] }) {
  const topRequests: URLSearchParams[] = []
  server.use(
    http.get('*/api/v1/instances/:id/host/processes', () => HttpResponse.json(snapshot)),
    http.get('*/api/v1/instances/:id/host/processes/top', ({ request }) => {
      const params = new URL(request.url).searchParams
      topRequests.push(params)
      return HttpResponse.json({
        from: '2026-11-21T09:00:00Z',
        to: '2026-11-21T10:00:00Z',
        group_by: params.get('group_by'),
        sample_times: 2,
        groups: [
          {
            group: 'app',
            avg_cpu_ratio: 1.5,
            max_rss_bytes: 4096,
            avg_read_bytes_per_s: null,
            avg_write_bytes_per_s: 1024,
          },
        ],
      })
    }),
  )
  return topRequests
}

const SAMPLE = {
  ts: '2026-11-21T10:00:00Z',
  processes: [
    proc(100, 0.01, 1024 * 1024, null),
    proc(101, 0.2, 8 * 1024 * 1024, 2048, '-42'),
    proc(102, 1.5, 2 * 1024 * 1024, 0),
  ],
}

function renderPanel() {
  return renderWithProviders(<ProcessesPanel instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}`,
  })
}

describe('ProcessesPanel', () => {
  afterEach(async () => {
    resetPreferences()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('UI-PROC-001 renders the newest sample by CPU and sorts by memory', async () => {
    vi.useRealTimers()
    serve(SAMPLE)
    const { container } = renderPanel()
    await waitFor(() => expect(dataRows(TABLE)).toHaveLength(3))
    expect(columnTexts(TABLE, 0).length).toBe(3)
    const pids = () => dataRows(TABLE).map((row) => within(row).getByRole('rowheader').textContent)
    expect(pids()).toEqual(['102', '101', '100'])
    expect(dataRows(TABLE)[0]).toHaveTextContent('150.0%')
    const link = within(dataRows(TABLE)[1]!).getByRole('link', { name: '-42' })
    expect(link).toHaveAttribute('href', `/instances/${INSTANCE_ID}/queries/-42?db=app`)
    sortTable(TABLE, 'Memory', 'descending')
    expect(pids()).toEqual(['101', '102', '100'])
    await expectNoA11yViolations(container)
  })

  it('UI-PROC-002 the group selector reads the top endpoint with group_by', async () => {
    vi.useRealTimers()
    const requests = serve(SAMPLE)
    renderPanel()
    await waitFor(() => expect(dataRows(TABLE)).toHaveLength(3))
    fireEvent.change(screen.getByRole('combobox', { name: 'Group by' }), {
      target: { value: 'datname' },
    })
    const groups = 'PostgreSQL processes by group'
    await waitFor(() => expect(dataRows(groups)).toHaveLength(1))
    expect(requests.at(-1)?.get('group_by')).toBe('datname')
    expect(requests.at(-1)?.get('from')).toBeTruthy()
    expect(dataRows(groups)[0]).toHaveTextContent('150.0%')
    expect(dataRows(groups)[0]).toHaveTextContent('1.0 KiB/s')
  })

  it('UI-PROC-003 unknown I/O shows a dash with the reason', async () => {
    vi.useRealTimers()
    serve(SAMPLE)
    renderPanel()
    await waitFor(() => expect(dataRows(TABLE)).toHaveLength(3))
    const postmaster = dataRows(TABLE)[2]!
    const dashes = within(postmaster).getAllByTitle(IO_UNKNOWN)
    expect(dashes).toHaveLength(2)
    expect(dashes[0]).toHaveTextContent('—')
    expect(dataRows(TABLE)[1]).toHaveTextContent('2.0 KiB/s')
  })

  it('UI-PROC-004 the empty state names the setting and the agent placement', async () => {
    vi.useRealTimers()
    serve({ ts: null, processes: [] })
    renderPanel()
    const section = await screen.findByRole('region', { name: 'No per-process metrics' })
    expect(section).toHaveTextContent('host.processes.enabled')
    expect(section).toHaveTextContent('agent on the database host')
    expect(screen.queryByRole('combobox', { name: 'Group by' })).toBeNull()
  })

  it('UI-PROC-005 reads in Italian', async () => {
    vi.useRealTimers()
    await setServerLanguage('it')
    serve(SAMPLE)
    renderPanel()
    await waitFor(() => expect(dataRows('Processi PostgreSQL')).toHaveLength(3))
    expect(screen.getByRole('heading', { name: 'Processi' })).toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: 'Raggruppa per' })).toBeInTheDocument()
    expect(dataRows('Processi PostgreSQL')[0]).toHaveTextContent('150,0%')
    expect(
      within(dataRows('Processi PostgreSQL')[2]!).getAllByTitle(
        'L’agent non può leggere i contatori I/O dei processi di un altro utente',
      ),
    ).toHaveLength(2)
  })
})
