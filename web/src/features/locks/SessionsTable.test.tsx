import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { withOrigins } from '@/test/fixtures/getLocks'
import { renderWithProviders } from '@/test/render'
import { columnTexts, dataRows, filterTable, searchTable, sortTable } from '@/test/table'

import { ActivitySection } from './ActivitySection'
import { SessionsTable } from './SessionsTable'
import { splitByOrigin, type Session } from '@/lib/sessions'
import { NOW } from '@/test/time'

const sessions = withOrigins.sessions as Session[]
const connections = withOrigins.connections

function renderTable(origin: 'application' | 'pglens', rows: Session[], all = rows) {
  return renderWithProviders(
    <SessionsTable
      currentTier="T2"
      instanceId="instance-1"
      origin={origin}
      rows={rows}
      sampleStale={false}
      sessions={all}
    />,
  )
}

function manySessions(count: number): Session[] {
  return Array.from({ length: count }, (_, index) => ({
    pid: 5000 + index,
    usename: index % 2 === 0 ? 'ann' : 'bob',
    datname: 'shop',
    application_name: 'api',
    client_addr: '10.0.0.9',
    state: index === 0 ? 'idle in transaction' : 'active',
    xact_age: index,
    query_age: index,
    origin: 'application',
    query_text: `SELECT ${index}`,
    query_text_source: 'normalised',
    blocked_by: index === 1 ? [5000] : [],
  }))
}

describe('SessionsTable', () => {
  it('SORIGIN-UI-001 splits the sessions into two tables by origin, never one in both', () => {
    renderWithProviders(
      <ActivitySection
        connections={connections}
        currentTier="T2"
        instanceId="instance-1"
        now={NOW}
        response={{ stale: false, metrics: {} }}
        sessions={sessions}
      />,
    )

    expect(columnTexts('Application sessions', 1)).toEqual(['4102', '4101'])
    expect(columnTexts('pglens monitoring sessions', 1)).toEqual(['4103'])
    expect(screen.queryByRole('list', { name: 'Active sessions' })).not.toBeInTheDocument()
    expect(splitByOrigin(sessions).application.map((s) => s.pid)).not.toContain(4103)
  })

  it('SORIGIN-UI-001 states an empty origin in words', () => {
    const { unmount } = renderTable('application', [])
    expect(screen.getByText('No application sessions at this moment')).toBeInTheDocument()
    unmount()
    renderTable('pglens', [])
    expect(screen.getByText('No pglens sessions')).toBeInTheDocument()
    expect(
      screen.getAllByText(
        'Connections opened by pglens itself to collect these metrics; they are not your workload.',
      ).length,
    ).toBeGreaterThan(0)
  })

  it('CAPTION-PAGE-045 each sessions table keeps its caption, hint and column hints', () => {
    for (const [origin, rows] of [
      ['application', sessions.filter((s) => s.origin === 'application')],
      ['pglens', [] as Session[]],
    ] as const) {
      const { unmount } = renderTable(origin, [...rows])

      expect(screen.getAllByRole('button', { name: /^About /i }).length, origin).toBeGreaterThan(0)
      unmount()
    }
    renderTable('application', manySessions(2))
    for (const column of ['State', 'Wait', 'Transaction age', 'Query age', 'PID']) {
      expect(screen.getByRole('button', { name: `About ${column}` }), column).toBeInTheDocument()
    }
  })

  it('SORIGIN-UI-003 searches, filters, sorts and pages with the shared controls', () => {
    renderTable('application', manySessions(25))
    const name = 'Application sessions'

    expect(dataRows(name)).toHaveLength(20)
    expect(screen.getByText(/Showing 1–20 of 25 sessions/)).toBeInTheDocument()
    expect(columnTexts(name, 1)[0]).toBe('5024')

    sortTable(name, 'PID', 'ascending')
    expect(columnTexts(name, 1)[0]).toBe('5000')

    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    expect(dataRows(name)).toHaveLength(5)

    searchTable(name, 'SELECT 7')
    expect(columnTexts(name, 1)).toEqual(['5007'])

    searchTable(name, '')
    filterTable(name, 'Waiting', 'only')
    expect(columnTexts(name, 1)).toEqual(['5001'])

    filterTable(name, 'Waiting', '')
    filterTable(name, 'Idle in transaction', 'only')
    expect(columnTexts(name, 1)).toEqual(['5000'])
  })

  it('SORIGIN-UI-004 expands a row to every field and a copy button', () => {
    renderTable('application', sessions.slice(0, 1))

    fireEvent.click(screen.getByRole('button', { name: 'Show details of session 4101' }))
    const detail = screen.getByTestId('session-detail')

    for (const label of [
      'PID',
      'Origin',
      'State',
      'Wait event type',
      'Wait event',
      'Transaction age',
      'Query age',
      'State age',
      'Backend start',
      'Client address',
      'Application',
      'User',
      'Database',
      'Backend XID age',
      'Backend xmin age',
      'Query ID',
      'Leader PID',
      'Blocked by',
      'Blocking',
    ]) {
      expect(within(detail).getByText(label)).toBeInTheDocument()
    }
    expect(within(detail).getByText('-4611686018427387904')).toBeInTheDocument()
    expect(within(detail).getByRole('button', { name: /Copy query text/ })).toBeInTheDocument()
    expect(within(detail).getByRole('button', { name: 'Cancel query' })).toBeInTheDocument()
    expect(within(detail).getByRole('button', { name: 'Terminate backend' })).toBeInTheDocument()
  })

  it('SORIGIN-UI-005 labels a raw statement and leaves a normalised one unlabelled', () => {
    renderTable('application', sessions.slice(0, 2))

    const raw = dataRows('Application sessions').find((row) => row.textContent?.includes('4102'))
    const normalised = dataRows('Application sessions').find((row) =>
      row.textContent?.includes('4101'),
    )
    expect(raw).toBeDefined()
    expect(within(raw!).getByText('raw')).toBeInTheDocument()
    expect(within(normalised!).queryByText('raw')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Show details of session 4102' }))
    expect(screen.getByText('Query text (raw)')).toBeInTheDocument()
  })
})
