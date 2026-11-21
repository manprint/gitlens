import { fireEvent, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { renderWithProviders } from '@/test/render'

const mocks = vi.hoisted(() => ({ logs: vi.fn(), classes: vi.fn() }))

vi.mock('@/api/queries', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/queries')>()),
  useLogs: mocks.logs,
  useLogClasses: mocks.classes,
}))

import { LogsPage } from './LogsPage'

const INSTANCE = '22222222-2222-2222-2222-222222222222'

function result(data: unknown) {
  return { data, isPending: false, isError: false, error: null, refetch: vi.fn() }
}

const deadlock = {
  ts: '2026-11-21T10:00:00Z',
  class: 'lock.deadlock',
  severity: 'ERROR',
  sqlstate: '40P01',
  datname: 'app',
  usename: 'web',
  application_name: 'api',
  pid: 4242,
  queryid: '-7',
  message: 'deadlock detected',
  extra: { detail: 'Process 4242 waits for ShareLock', statement: 'UPDATE t SET v = 1' },
  trace_id: null,
}

function render(route = `/instances/${INSTANCE}/logs`) {
  return renderWithProviders(<LogsPage instanceId={INSTANCE} />, { route })
}

beforeEach(() => {
  vi.useRealTimers()
  mocks.logs.mockReset()
  mocks.classes.mockReset()
})

afterEach(async () => {
  await setServerLanguage(undefined)
  await refreshLocale()
})

describe('LogsPage', () => {
  it('UI-PGL-001 renders the rows and the class summary', async () => {
    mocks.logs.mockReturnValue(result([deadlock]))
    mocks.classes.mockReturnValue(
      result([{ class: 'lock.deadlock', count: 3, last_ts: '2026-11-21T10:00:00Z' }]),
    )
    render()
    expect(await screen.findByRole('heading', { name: 'PostgreSQL logs' })).toBeInTheDocument()
    const summary = screen.getByRole('list', { name: 'Log classes' })
    expect(
      within(summary).getByRole('button', { name: 'lock.deadlock: 3 lines' }),
    ).toBeInTheDocument()
    const row = screen.getByTestId('log-row')
    expect(within(row).getByText('deadlock detected')).toBeInTheDocument()
    expect(within(row).getByText('ERROR')).toBeInTheDocument()
    expect(within(row).getByRole('link', { name: 'Open the query' })).toHaveAttribute(
      'href',
      `/instances/${INSTANCE}/queries/-7`,
    )
    fireEvent.click(within(row).getByRole('button', { name: 'Show the detail of this line' }))
    const detail = screen.getByTestId('log-row-detail')
    expect(within(detail).getByText('Process 4242 waits for ShareLock')).toBeInTheDocument()
    expect(within(detail).getByText('UPDATE t SET v = 1')).toBeInTheDocument()
    expect(within(detail).getByText('40P01')).toBeInTheDocument()
  })

  it('UI-PGL-002 a class filter sends class=', async () => {
    mocks.logs.mockReturnValue(result([deadlock]))
    mocks.classes.mockReturnValue(
      result([{ class: 'lock.deadlock', count: 3, last_ts: '2026-11-21T10:00:00Z' }]),
    )
    render()
    expect(mocks.logs).toHaveBeenLastCalledWith(
      { instance_id: INSTANCE, limit: 100 },
      expect.anything(),
    )
    fireEvent.click(await screen.findByRole('button', { name: 'lock.deadlock: 3 lines' }))
    expect(mocks.logs).toHaveBeenLastCalledWith(
      { instance_id: INSTANCE, limit: 100, class: 'lock.deadlock' },
      expect.anything(),
    )
    fireEvent.change(screen.getByRole('combobox', { name: 'Severity' }), {
      target: { value: 'ERROR' },
    })
    expect(mocks.logs).toHaveBeenLastCalledWith(
      { instance_id: INSTANCE, limit: 100, class: 'lock.deadlock', severity: 'ERROR' },
      expect.anything(),
    )
  })

  it('UI-PGL-003 the empty state names the settings', async () => {
    mocks.logs.mockReturnValue(result([]))
    mocks.classes.mockReturnValue(result([]))
    render()
    const text = (await screen.findByText(/checks\.logs\.enabled/)).textContent ?? ''
    expect(text).toContain('logging_collector = on')
    expect(text).toContain('log_destination = jsonlog')
  })

  it('UI-PGL-004 reads in Italian', async () => {
    await setServerLanguage('it')
    mocks.logs.mockReturnValue(result([deadlock]))
    mocks.classes.mockReturnValue(
      result([{ class: 'lock.deadlock', count: 1, last_ts: '2026-11-21T10:00:00Z' }]),
    )
    render()
    expect(await screen.findByRole('heading', { name: 'Log PostgreSQL' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'lock.deadlock: 1 riga' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Righe del log' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: 'Messaggio' })).toBeInTheDocument()
  })
})
