import { act, fireEvent, screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

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
      screen.getByText('No observed setting changes match the selected time range and search.'),
    ).toBeInTheDocument()
  })
})
