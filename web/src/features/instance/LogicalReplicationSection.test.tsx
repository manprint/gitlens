import { act, fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { INSTANCE_ID, NOW } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { columnTexts, dataRows, filterTable, searchTable, seq, sortTable } from '@/test/table'

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

/** Opens one list: the section shows one list at a time (15.3). */
async function openView(name: string) {
  const tab = screen.getByRole('tab', { name })
  fireEvent.mouseDown(tab)
  fireEvent.click(tab)
  await settle()
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

    await openView('Logical slots')
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

describe('LogicalReplicationSection tables: pages, search, filters, sort', () => {
  it('TBL-UI-025 subscriptions', async () => {
    renderSection({
      subscriptions: seq(25).map((n, index) =>
        subscription({
          name: `sub_${n}`,
          slot_name: `slot_${n}`,
          database: index < 6 ? 'crm' : 'app',
          enabled: index >= 2,
          last_message_at: index === 2 ? minutesBefore(30) : minutesBefore(1),
        }),
      ),
      slots: seq(25).map((n, index) => ({
        slot_name: `slot_${n}`,
        bytes_per_second: (index + 1) * 1_024,
        txns_per_second: 1,
        spill_bytes_per_second: 0,
        spill_txns_per_second: 0,
        stream_bytes_per_second: 0,
        last_seen: NOW,
      })),
    })
    await settle()

    const subs = 'Subscriptions'
    expect(dataRows(subs)).toHaveLength(20)
    searchTable(subs, 'slot_1')
    expect(dataRows(subs)).toHaveLength(10)
    searchTable(subs, '')
    filterTable(subs, 'State', 'disabled')
    expect(columnTexts(subs)).toEqual(['sub_01', 'sub_02'])
    filterTable(subs, 'State', 'silent')
    expect(columnTexts(subs)).toEqual(['sub_03'])
    filterTable(subs, 'State', '')
    filterTable(subs, 'Database', 'crm')
    expect(dataRows(subs)).toHaveLength(6)
    filterTable(subs, 'Database', '')
    sortTable(subs, 'Subscription', 'descending')
    expect(columnTexts(subs)[0]).toBe('sub_25')
  })

  it('TBL-UI-026 logical-slots', async () => {
    renderSection({
      subscriptions: seq(25).map((n, index) =>
        subscription({
          name: `sub_${n}`,
          slot_name: `slot_${n}`,
          database: index < 6 ? 'crm' : 'app',
          enabled: index >= 2,
          last_message_at: index === 2 ? minutesBefore(30) : minutesBefore(1),
        }),
      ),
      slots: seq(25).map((n, index) => ({
        slot_name: `slot_${n}`,
        bytes_per_second: (index + 1) * 1_024,
        txns_per_second: 1,
        spill_bytes_per_second: 0,
        spill_txns_per_second: 0,
        stream_bytes_per_second: 0,
        last_seen: NOW,
      })),
    })
    await settle()
    await openView('Logical slots')
    const slots = 'Logical slots'
    expect(dataRows(slots)).toHaveLength(20)
    searchTable(slots, 'slot_2')
    expect(dataRows(slots)).toHaveLength(6)
    searchTable(slots, '')
    sortTable(slots, 'Decoded', 'descending')
    expect(columnTexts(slots)[0]).toBe('slot_25')
  })
})
