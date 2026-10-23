import { act, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { INSTANCE_ID, NOW } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { LogicalReplicationSection } from './LogicalReplicationSection'

async function settle() {
  await act(async () => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      await vi.advanceTimersByTimeAsync(0)
      await Promise.resolve()
    }
  })
}

function minutesBefore(minutes: number): string {
  return new Date(new Date(NOW).getTime() - minutes * 60 * 1000).toISOString()
}

function subscription(overrides: Partial<Schemas['Subscription']> = {}): Schemas['Subscription'] {
  return {
    name: 'orders_sub',
    database: 'app',
    owner: 'postgres',
    enabled: true,
    slot_name: 'orders_sub',
    publications: ['orders'],
    apply_worker: true,
    sync_workers: 0,
    last_message_at: minutesBefore(1),
    apply_errors: 0,
    sync_errors: 0,
    conflicts: null,
    first_seen: NOW,
    last_seen: NOW,
    ...overrides,
  }
}

function renderSection(data: Partial<Schemas['LogicalReplicationResponse']>) {
  server.use(
    ok('getInstanceLogicalReplication', {
      instance_id: INSTANCE_ID,
      window_seconds: 3600,
      subscriptions: [],
      slots: [],
      ...data,
    }),
  )
  return renderWithProviders(<LogicalReplicationSection instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}`,
  })
}

describe('LogicalReplicationSection', () => {
  // UI-LREP-001: each subscription shows whether it applies changes, as the
  // advisor judges it, and each logical slot its decoding rates, a high
  // spill rate marked.
  it('UI-LREP-001 shows subscription states and slot decoding', async () => {
    renderSection({
      subscriptions: [
        subscription(),
        subscription({ name: 'stuck', last_message_at: minutesBefore(12) }),
        subscription({
          name: 'crashed',
          apply_worker: false,
          last_message_at: null,
          apply_errors: 7,
        }),
        subscription({
          name: 'parked',
          enabled: false,
          apply_worker: false,
          slot_name: null,
          conflicts: 3,
        }),
      ],
      slots: [
        {
          slot_name: 'orders_sub',
          bytes_per_second: 20480,
          txns_per_second: 12.5,
          spill_bytes_per_second: 2 * 1024 * 1024,
          spill_txns_per_second: 0.5,
          stream_bytes_per_second: 0,
          last_seen: NOW,
        },
      ],
    })
    await settle()

    const subs = within(screen.getByRole('table', { name: 'Subscriptions' }))
      .getAllByRole('row')
      .slice(1)
    expect(within(subs[0]!).getByText('running')).toBeInTheDocument()
    expect(within(subs[1]!).getByText('silent')).toBeInTheDocument()
    expect(within(subs[2]!).getByText('not running')).toBeInTheDocument()
    expect(within(subs[2]!).getByText('7 / 0')).toBeInTheDocument()
    expect(within(subs[3]!).getByText('disabled')).toBeInTheDocument()
    expect(within(subs[3]!).getByText('none')).toBeInTheDocument()
    expect(within(subs[3]!).getByText('3')).toBeInTheDocument()
    expect(within(subs[0]!).getByText('n/a')).toBeInTheDocument()

    const slots = within(screen.getByRole('table', { name: 'Logical slots' }))
      .getAllByRole('row')
      .slice(1)
    expect(within(slots[0]!).getByText('2.0 MiB/s')).toBeInTheDocument()
    expect(within(slots[0]!).getByText('13/s')).toBeInTheDocument()
  })

  it('UI-LREP-002 says when the instance has no logical replication', async () => {
    renderSection({})
    await settle()
    expect(screen.getByText('No logical replication')).toBeInTheDocument()
  })
})
