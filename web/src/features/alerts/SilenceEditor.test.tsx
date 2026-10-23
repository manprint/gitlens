import { fireEvent, screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'

import { assertMatchesContract } from '@/test/contract'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'
import { NOW } from '@/test/time'

import { SilenceEditor } from './SilenceEditor'

// The suite pins TZ=UTC, where browser-local and UTC wall-clock text are the
// same, so an offset bug in the default window cannot show there. This file
// runs in Rome time: at the fixed clock the operator's local hour is 04:00,
// while the instant is 02:00Z.
const originalTimeZone = process.env.TZ

beforeAll(() => {
  process.env.TZ = 'Europe/Rome'
})

afterAll(() => {
  if (originalTimeZone === undefined) delete process.env.TZ
  else process.env.TZ = originalTimeZone
})

function renderEditor() {
  return renderWithProviders(
    <SilenceEditor firingAlerts={[]} onCancel={vi.fn()} onCreated={vi.fn()} />,
  )
}

describe('SilenceEditor default window', () => {
  it('UI-ALERT-038 shows the default window in the operator local time', () => {
    expect(new Date(NOW).getTimezoneOffset()).toBe(-120)

    renderEditor()

    expect(screen.getByLabelText('Start')).toHaveValue('2026-08-27T04:00')
    expect(screen.getByLabelText('End')).toHaveValue('2026-08-27T05:00')
  })

  it('UI-ALERT-039 submits the default window as the instant it shows', async () => {
    let requestBody: unknown
    server.use(
      http.post('*/api/v1/silences', async ({ request }) => {
        requestBody = await request.json()
        const response = {
          ends_at: '2026-08-27T03:00:00.000Z',
          matchers: [{ name: 'severity', value: 'warning' }],
          reason: 'planned maintenance',
          silence_id: '00000000-0000-4000-8000-000000000004',
          starts_at: NOW.toISOString(),
        }
        assertMatchesContract('createSilence', 201, response)
        return HttpResponse.json(response, { status: 201 })
      }),
    )
    renderEditor()

    fireEvent.change(screen.getByLabelText('Matcher 1 name'), { target: { value: 'severity' } })
    fireEvent.change(screen.getByLabelText('Matcher 1 value'), { target: { value: 'warning' } })
    fireEvent.change(screen.getByLabelText('Reason'), { target: { value: 'planned maintenance' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create silence' }))
    await flushNetwork()

    expect(requestBody).toEqual({
      ends_at: '2026-08-27T03:00:00.000Z',
      matchers: [{ name: 'severity', value: 'warning' }],
      reason: 'planned maintenance',
      starts_at: '2026-08-27T02:00:00.000Z',
    })
  })
})
