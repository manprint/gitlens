import { act, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { INSTANCE_ID, NOW } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { InternalsSection } from './InternalsSection'

async function settle() {
  await act(async () => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      await vi.advanceTimersByTimeAsync(0)
      await Promise.resolve()
    }
  })
}

function cache(name: string, overrides: Partial<Schemas['SLRUCache']> = {}): Schemas['SLRUCache'] {
  return {
    name,
    hit_ratio: 0.9,
    hits_per_second: 90,
    reads_per_second: 10,
    writes_per_second: 0.5,
    zeroed_per_second: 0,
    flushes_per_second: 0.02,
    truncates_per_second: 0,
    exists_per_second: 0,
    last_seen: NOW,
    ...overrides,
  }
}

function renderSection(data: Partial<Schemas['InternalsResponse']>) {
  server.use(
    ok('getInstanceInternals', {
      instance_id: INSTANCE_ID,
      window_seconds: 3600,
      wal_directory: null,
      databases: [],
      slru: [],
      temp_files: [],
      standby: null,
      ...data,
    }),
  )
  return renderWithProviders(<InternalsSection instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}`,
  })
}

describe('InternalsSection', () => {
  // UI-INT-001: a primary shows the size of pg_wal and of each database with
  // its growth (n/a without history), each SLRU cache's hit
  // ratio and misses, and only the databases that wrote temporary files; it
  // has no replay section.
  it('UI-INT-001 shows pg_wal, the SLRU caches and temporary files of a primary', async () => {
    renderSection({
      wal_directory: { bytes: 2 * 1024 ** 3, files: 128, observed_at: NOW },
      databases: [
        {
          database: 'app',
          size_bytes: 5 * 1024 ** 3,
          observed_at: NOW,
          growth_24h_bytes: 1024 ** 3,
          growth_7d_bytes: -512 * 1024 ** 2,
        },
        {
          database: 'crm',
          size_bytes: 8 * 1024 ** 2,
          observed_at: NOW,
          growth_24h_bytes: null,
          growth_7d_bytes: null,
        },
      ],
      slru: [
        cache('subtransaction'),
        cache('notify', { hit_ratio: null, hits_per_second: 0, reads_per_second: 0 }),
      ],
      temp_files: [
        { database: 'app', files_per_second: 0.05, bytes_per_second: 1024 * 1024 },
        { database: 'postgres', files_per_second: 0, bytes_per_second: 0 },
      ],
    })
    await settle()

    const dir = screen.getByRole('article', { name: 'WAL directory' })
    expect(within(dir).getByText('2.0 GiB')).toBeInTheDocument()
    expect(within(dir).getByText('128 files')).toBeInTheDocument()
    expect(screen.queryByRole('article', { name: 'WAL replay' })).not.toBeInTheDocument()

    const sizes = within(screen.getByRole('table', { name: 'Database sizes' }))
      .getAllByRole('row')
      .slice(1)
    expect(within(sizes[0]!).getByText('5.0 GiB')).toBeInTheDocument()
    expect(within(sizes[0]!).getByText('+1.0 GiB')).toBeInTheDocument()
    expect(within(sizes[0]!).getByText('-512.0 MiB')).toBeInTheDocument()
    expect(within(sizes[1]!).getAllByText('n/a')).toHaveLength(2)

    const caches = within(screen.getByRole('table', { name: 'SLRU caches' }))
      .getAllByRole('row')
      .slice(1)
    expect(within(caches[0]!).getByText('90.0%')).toBeInTheDocument()
    expect(within(caches[0]!).getByText('10/s')).toBeInTheDocument()
    expect(within(caches[1]!).getByText('not read')).toBeInTheDocument()

    const temp = within(screen.getByRole('table', { name: 'Temporary files' }))
      .getAllByRole('row')
      .slice(1)
    expect(temp).toHaveLength(1)
    expect(within(temp[0]!).getByText('app')).toBeInTheDocument()
    expect(within(temp[0]!).getByText('3/min')).toBeInTheDocument()
    expect(within(temp[0]!).getByText('1.0 MiB/s')).toBeInTheDocument()
  })

  // UI-INT-002: a standby shows whether replay is paused and the queries
  // recovery conflicts cancelled; without the pg_wal listing it says why.
  it('UI-INT-002 shows a paused standby and its recovery conflicts', async () => {
    renderSection({
      standby: {
        replay_paused: true,
        observed_at: NOW,
        conflicts: [{ database: 'app', reason: 'snapshot', per_second: 0.1 }],
        prefetch_blocks_per_second: { prefetch: 4, hit: 200 },
      },
    })
    await settle()

    const replay = screen.getByRole('article', { name: 'WAL replay' })
    expect(within(replay).getByText('paused')).toBeInTheDocument()
    expect(screen.getByText(/pg_wal is not listed/)).toBeInTheDocument()
    const conflicts = within(screen.getByRole('table', { name: 'Recovery conflicts' }))
      .getAllByRole('row')
      .slice(1)
    expect(within(conflicts[0]!).getByText('snapshot')).toBeInTheDocument()
    expect(within(conflicts[0]!).getByText('6/min')).toBeInTheDocument()
    expect(
      screen.getByText('WAL prefetch: 4/s blocks read ahead, 200/s already in shared buffers.'),
    ).toBeInTheDocument()
    expect(screen.getByText('No query outgrew work_mem in the last hour.')).toBeInTheDocument()
  })

  it('UI-INT-003 says when nothing was collected', async () => {
    renderSection({})
    await settle()
    expect(screen.getByText('No internals collected')).toBeInTheDocument()
  })
})
