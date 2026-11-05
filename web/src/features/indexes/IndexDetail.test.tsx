import { screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { buildIndexRows } from '@/lib/indexAnalysis'
import { expectNoA11yViolations } from '@/test/a11y'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { base as history, empty as emptyHistory } from '@/test/fixtures/getIndexHistory'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'

import { IndexDetail } from './IndexDetail'

vi.mock('echarts-for-react/esm/core', () => ({ default: () => null }))

function row(extra: Record<string, unknown> = {}) {
  return buildIndexRows(
    [
      {
        datname: 'odoo',
        schemaname: 'public',
        relname: 'res_partner',
        indexrelname: 'res_partner_name_idx',
        index_bytes: 1_400_000,
        is_primary: false,
        is_unique: false,
        is_valid: true,
        last_scan_status: 'scanned',
        last_scan_at: '2026-08-27T01:30:00Z',
        definition: 'CREATE INDEX res_partner_name_idx ON public.res_partner USING btree (name)',
        ...extra,
      },
    ],
    [],
  )[0]!
}

function render(index = row(), response: unknown = history) {
  server.use(ok('getIndexHistory', response))
  return renderWithProviders(<IndexDetail instanceId={INSTANCE_ID} row={index} />, {
    route: '/indexes?range=6h',
  })
}

describe('IndexDetail', () => {
  it('INDEXDETAIL-001 draws the scan and size charts from the history and shows the badges', async () => {
    render()
    await flushNetwork(30)

    expect(
      screen.getByRole('img', { name: 'Index scans of res_partner_name_idx' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('img', { name: 'Index size of res_partner_name_idx' }),
    ).toBeInTheDocument()
    const detail = screen.getByTestId('index-detail')
    expect(within(detail).getByText('Valid')).toBeInTheDocument()
    expect(within(detail).getByText(/Last scan/)).toBeInTheDocument()
    expect(within(detail).queryByText('Unique')).not.toBeInTheDocument()
  })

  it('INDEXDETAIL-002 says there is no history yet when the series is empty', async () => {
    render(row(), emptyHistory)
    await flushNetwork(30)

    expect(screen.getByText('No history yet (collected every 5 min).')).toBeInTheDocument()
    expect(screen.queryByRole('img', { name: /Index scans of/ })).not.toBeInTheDocument()
  })

  it('INDEXDETAIL-004 flags an invalid unique index and a missing last-scan time', async () => {
    render(row({ is_valid: false, is_unique: true, last_scan_status: undefined, last_scan_at: '' }))
    await flushNetwork(30)

    const detail = screen.getByTestId('index-detail')
    expect(within(detail).getByText('Invalid')).toBeInTheDocument()
    expect(within(detail).getByText('Unique')).toBeInTheDocument()
    expect(within(detail).getByText(/last scan time is not available/)).toBeInTheDocument()
  })

  it('INDEXDETAIL-003 has no accessibility violations', async () => {
    vi.useRealTimers()
    const { container } = render()
    await screen.findByRole('img', { name: 'Index size of res_partner_name_idx' })

    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })
})
