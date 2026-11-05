import { act, fireEvent, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { dataRows, filterTable, searchTable, seq, sortTable } from '@/test/table'

import { SettingsSection } from './SettingsSection'

const SETTING_TIME = '2026-08-28T12:00:00Z'

function setting(overrides: Partial<Schemas['Setting']> = {}): Schemas['Setting'] {
  return {
    name: 'fsync',
    value: 'on',
    unit: '',
    source: 'postgresql.conf',
    context: 'postmaster',
    pending_restart: 'false',
    first_seen: SETTING_TIME,
    last_seen: SETTING_TIME,
    changed_at: SETTING_TIME,
    ...overrides,
  }
}

async function settle() {
  await act(async () => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      await vi.advanceTimersByTimeAsync(0)
      await Promise.resolve()
    }
  })
}

function renderSettings(settings: Schemas['Setting'][]) {
  server.use(
    ok('getInstanceSettings', {
      instance_id: INSTANCE_ID,
      settings,
    }),
  )
  return renderWithProviders(<SettingsSection instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}?range=1h`,
  })
}

describe('SettingsSection', () => {
  it('UI-INST-044 explains archive command redaction', async () => {
    renderSettings([
      setting({
        name: 'archive_command',
        value: 'wal-g wal-push %p [redacted]',
      }),
    ])
    await settle()

    const value = screen.getByText('wal-g wal-push %p [redacted]')
    expect(value).toHaveAttribute(
      'title',
      'Arguments are withheld because they may contain credentials.',
    )
  })

  it('UI-INST-045 changed_since narrows the table and updates the URL', async () => {
    const oldSetting = setting({ name: 'old_setting', value: 'old' })
    const recentSetting = setting({
      name: 'recent_setting',
      value: 'new',
      changed_at: '2026-08-28T12:30:00Z',
    })
    const view = renderSettings([oldSetting, recentSetting])
    await settle()
    expect(screen.getByText('old_setting')).toBeInTheDocument()

    server.use(
      http.get('/api/v1/instances/:id/settings', ({ request }) => {
        const changedSince = new URL(request.url).searchParams.get('changed_since')
        return HttpResponse.json({
          instance_id: INSTANCE_ID,
          settings:
            changedSince === '2026-08-28T12:30:00.000Z'
              ? [recentSetting]
              : [oldSetting, recentSetting],
        })
      }),
    )

    fireEvent.change(screen.getByLabelText('Changed since'), {
      target: { value: '2026-08-28T12:30' },
    })
    await settle()

    expect(view.router.state.location.search).toBe(
      '?range=1h&changed_since=2026-08-28T12%3A30%3A00.000Z',
    )
    expect(screen.queryByText('old_setting')).not.toBeInTheDocument()
    expect(screen.getByText('recent_setting')).toBeInTheDocument()
  })

  it('UI-INST-046 makes an empty changed-since result explicit', async () => {
    renderSettings([])
    await settle()

    expect(screen.getByRole('heading', { name: 'No setting changes' })).toBeInTheDocument()
    expect(
      screen.getByText('No setting changed since the date in Changed since.'),
    ).toBeInTheDocument()
  })

  it('UI-INST-047 keeps the durability posture when nothing changed in the range', async () => {
    const fsyncOff = setting({ name: 'fsync', value: 'off', changed_at: '2026-08-07T12:00:00Z' })
    server.use(
      http.get('/api/v1/instances/:id/settings', ({ request }) => {
        // Like the server, drop the settings changed before changed_since.
        const changedSince = new URL(request.url).searchParams.get('changed_since')
        const settings = [fsyncOff].filter(
          (candidate) =>
            !changedSince || Date.parse(candidate.changed_at) >= Date.parse(changedSince),
        )
        return HttpResponse.json({ instance_id: INSTANCE_ID, settings })
      }),
    )
    renderWithProviders(<SettingsSection instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}?range=1h`,
    })
    await settle()

    expect(screen.getByRole('heading', { name: 'No setting changes' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Durability summary' })).toBeInTheDocument()
    expect(screen.getByText('fsync')).toBeInTheDocument()
    expect(screen.getByLabelText('Durability severity: critical')).toBeInTheDocument()
  })
})

describe('SettingsSection table controls', () => {
  it('TBL-UI-107 pg-settings pages, searches, filters and sorts the changed settings', async () => {
    renderSettings(
      seq(25).map((n, index) =>
        setting({
          name: `setting_${n}`,
          source: index < 4 ? 'override' : 'postgresql.conf',
          pending_restart: index < 2 ? 'true' : 'false',
        }),
      ),
    )
    await settle()
    const name = 'Observed PostgreSQL settings'
    const names = () => dataRows(name).map((row) => within(row).getByRole('rowheader').textContent)

    expect(dataRows(name)).toHaveLength(20)
    searchTable(name, 'setting_2', 'Search settings')
    expect(dataRows(name)).toHaveLength(6)
    searchTable(name, '', 'Search settings')
    filterTable(name, 'Source', 'override')
    expect(dataRows(name)).toHaveLength(4)
    filterTable(name, 'Source', '')
    filterTable(name, 'State', 'pending')
    expect(names()).toEqual(['setting_01', 'setting_02'])
    filterTable(name, 'State', '')
    sortTable(name, 'Name', 'descending')
    expect(names()[0]).toBe('setting_25')
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    expect(dataRows(name)).toHaveLength(5)
  })
})
