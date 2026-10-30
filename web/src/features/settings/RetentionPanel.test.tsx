import { fireEvent, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it, vi } from 'vitest'

import { expectNoA11yViolations } from '@/test/a11y'
import { assertMatchesContract } from '@/test/contract'
import { base as families, custom as customFamilies } from '@/test/fixtures/getRetention'
import { base as shortening, lengthening } from '@/test/fixtures/previewRetention'
import { makeSetRetention } from '@/test/fixtures/setRetention'
import { ok, sequence, status } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'

import { RetentionPanel } from './RetentionPanel'

const TABLE = 'Data retention by metrics family'

async function renderPanel() {
  const view = renderWithProviders(<RetentionPanel />, { route: '/settings?tab=retention' })
  await flushNetwork()
  return view
}

function row(family: string) {
  const table = screen.getByRole('table', { name: TABLE })
  const name = `New retention of ${family} in days`
  const tr = within(table)
    .getAllByRole('row')
    .find((candidate) => within(candidate).queryByRole('textbox', { name }) !== null)
  if (tr === undefined) throw new Error(`no row for ${family}`)
  const input = within(tr).getByRole('textbox', { name })
  return {
    tr,
    input,
    preview: within(tr).getByRole('button', { name: `Preview ${family}` }),
    apply: within(tr).getByRole('button', { name: `Apply ${family}` }),
  }
}

/** Captures the bodies of setRetention and answers each with `answer`. */
function captureSet(answer: (body: { days: number }) => Response) {
  const bodies: unknown[] = []
  server.use(
    http.put('*/api/v1/settings/retention/:family', async ({ params, request }) => {
      const body = (await request.json()) as { days: number }
      bodies.push({ family: params.family, body })
      return answer(body)
    }),
  )
  return bodies
}

describe('RetentionPanel', () => {
  it('RET-UI-001 lists the families; Apply waits for a preview of the typed value, then applies a lengthening without confirm', async () => {
    const updated = makeSetRetention({ days: 60, source: 'custom' })
    assertMatchesContract('setRetention', 200, updated)
    server.use(
      sequence(
        'getRetention',
        families,
        families.map((f) => (f.family === 'metrics_ash' ? updated : f)),
      ),
      ok('previewRetention', lengthening),
    )
    const bodies = captureSet(() => HttpResponse.json(updated))
    await renderPanel()

    const table = screen.getByRole('table', { name: TABLE })
    expect(within(table).getAllByRole('textbox')).toHaveLength(7)
    // One line per family: the label, with the hypertable in the title.
    expect(within(table).getByText('Bloat estimates')).toHaveAttribute('title', 'metrics_bloat')
    expect(within(table).getByText('90 days')).toBeInTheDocument()
    expect(within(table).getByText('8–365 days')).toBeInTheDocument()

    let ash = row('metrics_ash')
    expect(ash.input).toHaveValue('30')
    expect(ash.apply).toBeDisabled()
    // Every control states its effect (I-16).
    for (const control of [ash.input, ash.preview, ash.apply]) {
      expect(control).toHaveAccessibleDescription()
    }

    fireEvent.change(ash.input, { target: { value: '60' } })
    expect(row('metrics_ash').apply).toBeDisabled()
    fireEvent.click(row('metrics_ash').preview)
    await flushNetwork()

    ash = row('metrics_ash')
    expect(within(ash.tr).getByText('Keeps more history: nothing is deleted.')).toBeInTheDocument()
    expect(ash.apply).toBeEnabled()

    // A value other than the previewed one needs its own preview.
    fireEvent.change(ash.input, { target: { value: '61' } })
    expect(row('metrics_ash').apply).toBeDisabled()
    fireEvent.change(row('metrics_ash').input, { target: { value: '60' } })
    fireEvent.click(row('metrics_ash').preview)
    await flushNetwork()

    fireEvent.click(row('metrics_ash').apply)
    await flushNetwork()

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(bodies).toEqual([{ family: 'metrics_ash', body: { days: 60 } }])
    ash = row('metrics_ash')
    expect(within(ash.tr).getByText('60 days')).toBeInTheDocument()
    expect(within(ash.tr).getByRole('status')).toHaveTextContent('Applied: 60 days.')
    expect(ash.input).toHaveValue('60')
    expect(ash.apply).toBeDisabled()
  })

  it('RET-UI-001 a family without a policy shows it and an empty value', async () => {
    server.use(ok('getRetention', customFamilies))
    await renderPanel()

    const tables = row('metrics_tables')
    expect(within(tables.tr).getByText('No policy: keeps all history')).toBeInTheDocument()
    expect(tables.input).toHaveValue('')
    expect(tables.preview).toBeDisabled()
    expect(within(row('metrics_ash').tr).getByText('14 days')).toBeInTheDocument()
  })

  it('RET-UI-001 a policy without drop_after is shown as set outside pglens and cannot be changed', async () => {
    server.use(
      ok(
        'getRetention',
        families.map((f) =>
          f.family === 'metrics_ash'
            ? {
                ...f,
                days: null,
                source: 'custom' as const,
                unsupported_policy: 'drop_created_before 30 days',
              }
            : f,
        ),
      ),
    )
    await renderPanel()

    const ash = row('metrics_ash')
    expect(within(ash.tr).getByText('Set outside pglens')).toBeInTheDocument()
    expect(
      within(ash.tr).getByText('drop_created_before 30 days: change it with TimescaleDB.'),
    ).toBeInTheDocument()
    expect(ash.input).toBeDisabled()
    expect(ash.preview).toBeDisabled()
    expect(ash.apply).toBeDisabled()
  })

  it('RET-UI-002 a shortening needs the destructive dialog and its checkbox; only then is confirm sent', async () => {
    const updated = makeSetRetention({ days: 14, source: 'custom' })
    server.use(ok('getRetention', families), ok('previewRetention', shortening))
    const bodies = captureSet(() => HttpResponse.json(updated))
    await renderPanel()

    fireEvent.change(row('metrics_ash').input, { target: { value: '14' } })
    fireEvent.click(row('metrics_ash').preview)
    await flushNetwork()
    expect(
      within(row('metrics_ash').tr).getByText(
        'Deletes 64 chunks (~40 MiB) at the next policy run.',
      ),
    ).toBeInTheDocument()

    fireEvent.click(row('metrics_ash').apply)
    await flushNetwork(2)
    let dialog = screen.getByRole('dialog')
    expect(dialog).toHaveTextContent(
      'This will permanently delete 64 chunks (~40 MiB) of metrics_ash data older than 14 days at the next policy run. Take a backup first.',
    )
    const confirm = within(dialog).getByRole('button', { name: 'Delete history and apply' })
    expect(confirm).toBeDisabled()

    // Cancel sends nothing.
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await flushNetwork(2)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(bodies).toEqual([])

    fireEvent.click(row('metrics_ash').apply)
    await flushNetwork(2)
    dialog = screen.getByRole('dialog')
    const checkbox = within(dialog).getByRole('checkbox', {
      name: 'I understand that this history is deleted permanently at the next policy run.',
    })
    expect(checkbox).not.toBeChecked()
    fireEvent.click(checkbox)
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete history and apply' }))
    await flushNetwork()

    expect(bodies).toEqual([{ family: 'metrics_ash', body: { days: 14, confirm: true } }])
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(within(row('metrics_ash').tr).getByRole('status')).toHaveTextContent('Applied: 14 days.')
  })

  it('RET-UI-003 a failed apply or preview shows inline and keeps the previous value', async () => {
    server.use(ok('getRetention', families), ok('previewRetention', lengthening))
    const bodies = captureSet(() =>
      HttpResponse.json(
        { error: 'retention update failed', detail: 'add_retention_policy: deadlock detected' },
        { status: 500 },
      ),
    )
    await renderPanel()

    fireEvent.change(row('metrics_ash').input, { target: { value: '60' } })
    fireEvent.click(row('metrics_ash').preview)
    await flushNetwork()
    fireEvent.click(row('metrics_ash').apply)
    await flushNetwork()

    const ash = row('metrics_ash')
    expect(bodies).toHaveLength(1)
    expect(within(ash.tr).getByRole('alert')).toHaveTextContent(
      'retention update failed: add_retention_policy: deadlock detected',
    )
    expect(within(ash.tr).getByText('30 days')).toBeInTheDocument()
    expect(ash.input).toHaveValue('60')

    server.use(
      status('previewRetention', 400, {
        error: 'invalid days',
        detail: 'days of metrics_bloat must be between 8 and 365',
      }),
    )
    fireEvent.change(row('metrics_bloat').input, { target: { value: '30' } })
    fireEvent.click(row('metrics_bloat').preview)
    await flushNetwork()
    const bloat = row('metrics_bloat')
    expect(within(bloat.tr).getByRole('alert')).toHaveTextContent(
      'invalid days: days of metrics_bloat must be between 8 and 365',
    )
    expect(within(bloat.tr).getByText('90 days')).toBeInTheDocument()
    expect(bloat.apply).toBeDisabled()

    // An out-of-range value is refused before any request.
    fireEvent.change(row('metrics').input, { target: { value: '2' } })
    expect(row('metrics').input).toHaveAttribute('aria-invalid', 'true')
    expect(row('metrics').preview).toBeDisabled()
    expect(within(row('metrics').tr).getByText('Enter between 3 and 365 days.')).toBeInTheDocument()
  })

  it('RET-UI-006 a refused apply (409) reads the listing again and asks for a new preview', async () => {
    const moved = families.map((f) =>
      f.family === 'metrics_ash' ? { ...f, days: 20, source: 'custom' as const } : f,
    )
    server.use(sequence('getRetention', families, moved), ok('previewRetention', lengthening))
    const bodies = captureSet(() =>
      HttpResponse.json(
        {
          error: 'confirmation required',
          detail: 'a retention of 60 days for metrics_ash deletes history at the next policy run',
          preview: { ...lengthening, chunks_to_drop: 3, bytes_to_free: 1024 },
        },
        { status: 409 },
      ),
    )
    await renderPanel()

    fireEvent.change(row('metrics_ash').input, { target: { value: '60' } })
    fireEvent.click(row('metrics_ash').preview)
    await flushNetwork()
    fireEvent.click(row('metrics_ash').apply)
    await flushNetwork()

    const ash = row('metrics_ash')
    expect(bodies).toHaveLength(1)
    expect(within(ash.tr).getByRole('alert')).toHaveTextContent(/Preview again/)
    // The listing was read again: the live value moved to 20 days.
    expect(within(ash.tr).getByText('20 days')).toBeInTheDocument()
    expect(screen.queryByTestId('retention-preview-metrics_ash')).not.toBeInTheDocument()
    expect(ash.input).toHaveValue('60')
    expect(ash.apply).toBeDisabled()
  })

  it('RET-UI-007 a late preview of a value no longer typed is dropped, and a preview of an older live value does not enable Apply', async () => {
    let release: () => void = () => undefined
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    const moved = families.map((f) =>
      f.family === 'metrics_ash' ? { ...f, days: 45, source: 'custom' as const } : f,
    )
    server.use(
      sequence('getRetention', families, moved),
      http.get('*/api/v1/settings/retention/preview', async () => {
        await gate
        return HttpResponse.json(lengthening)
      }),
    )
    await renderPanel()

    // The answer for 60 arrives after the value became 61: dropped.
    fireEvent.change(row('metrics_ash').input, { target: { value: '60' } })
    fireEvent.click(row('metrics_ash').preview)
    await flushNetwork(2)
    fireEvent.change(row('metrics_ash').input, { target: { value: '61' } })
    release()
    await flushNetwork()
    expect(screen.queryByTestId('retention-preview-metrics_ash')).not.toBeInTheDocument()
    expect(row('metrics_ash').apply).toBeDisabled()
    expect(row('metrics_ash').preview).toBeEnabled()

    // A preview of 60 made against the live 30 days...
    fireEvent.change(row('metrics_ash').input, { target: { value: '60' } })
    fireEvent.click(row('metrics_ash').preview)
    await flushNetwork()
    expect(screen.getByTestId('retention-preview-metrics_ash')).toBeInTheDocument()
    expect(row('metrics_ash').apply).toBeEnabled()

    // ...no longer enables Apply once the listing reads 45 days (a failed
    // apply elsewhere reads it again).
    captureSet(() =>
      HttpResponse.json({ error: 'retention update failed', detail: 'boom' }, { status: 500 }),
    )
    fireEvent.change(row('metrics').input, { target: { value: '60' } })
    fireEvent.click(row('metrics').preview)
    await flushNetwork()
    fireEvent.click(row('metrics').apply)
    await flushNetwork()
    expect(within(row('metrics_ash').tr).getByText('45 days')).toBeInTheDocument()
    expect(row('metrics_ash').input).toHaveValue('60')
    expect(row('metrics_ash').apply).toBeDisabled()
  })

  it('RET-UI-003 a failed load offers a retry', async () => {
    server.use(status('getRetention', 503, { error: 'store unavailable', detail: '' }))
    await renderPanel()

    expect(screen.getByRole('alert')).toHaveTextContent('Could not load data retention.')
    expect(screen.queryByRole('table', { name: TABLE })).not.toBeInTheDocument()
  })

  it('RET-UI-004 the panel has no serious accessibility violations', async () => {
    server.use(ok('getRetention', families))
    const view = await renderPanel()
    expect(screen.getByRole('table', { name: TABLE })).toBeInTheDocument()

    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
  }, 15000)

  it('RET-UI-004 the confirmation dialog has no serious accessibility violations', async () => {
    server.use(ok('getRetention', families), ok('previewRetention', shortening))
    await renderPanel()
    fireEvent.change(row('metrics_ash').input, { target: { value: '14' } })
    fireEvent.click(row('metrics_ash').preview)
    await flushNetwork()
    fireEvent.click(row('metrics_ash').apply)
    await flushNetwork(2)
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByRole('checkbox')).not.toBeChecked()

    vi.useRealTimers()
    await expectNoA11yViolations(dialog)
  }, 15000)
})
