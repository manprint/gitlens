import { fireEvent, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { expectNoA11yViolations } from '@/test/a11y'
import { assertMatchesContract } from '@/test/contract'
import { catalogue } from '@/test/fixtures/getAdvisorRules'
import { base as windows, custom as customWindows } from '@/test/fixtures/getAdvisorWindows'
import { makeSetAdvisorRuleOverride } from '@/test/fixtures/setAdvisorRuleOverride'
import { ok, sequence, status } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'

import { AdvisorPanel } from './AdvisorPanel'

type AdvisorRule = Schemas['AdvisorRule']

const TABLE = 'Advisor rules'

async function renderPanel(route = '/settings?tab=advisor') {
  const view = renderWithProviders(<AdvisorPanel />, { route })
  await flushNetwork()
  return view
}

function ruleSwitch(id: string) {
  return within(screen.getByRole('table', { name: TABLE })).getByRole('switch', {
    name: `Run ${id}`,
  })
}

function bodyRows() {
  return within(screen.getByRole('table', { name: TABLE }))
    .getAllByRole('row')
    .slice(1)
}

function firstCells() {
  return bodyRows().map((row) => within(row).getAllByRole('cell')[0]?.textContent)
}

/** Captures the bodies of setAdvisorRuleOverride and answers each with `answer`. */
function captureOverride(answer: (id: string, body: { enabled: boolean }) => Response) {
  const bodies: unknown[] = []
  server.use(
    http.put('*/api/v1/advisor/rules/:id/override', async ({ params, request }) => {
      const body = (await request.json()) as { enabled: boolean }
      bodies.push({ id: params.id, body })
      return answer(String(params.id), body)
    }),
  )
  return bodies
}

/** Captures the bodies of setAdvisorWindows and answers each with `answer`. */
function captureWindows(answer: (body: Record<string, number | null>) => Response) {
  const bodies: unknown[] = []
  server.use(
    http.put('*/api/v1/settings/advisor-windows', async ({ request }) => {
      const body = (await request.json()) as Record<string, number | null>
      bodies.push(body)
      return answer(body)
    }),
  )
  return bodies
}

function windowsView() {
  return '/settings?tab=advisor&view=windows'
}

describe('AdvisorPanel', () => {
  it('ADVO-UI-001 a switch writes the override and shows the stored value; enabling again deletes it', async () => {
    const disabled = makeSetAdvisorRuleOverride({ id: 'index.unused' })
    const enabledAgain: AdvisorRule = { ...disabled, enabled: true, overridden: false }
    assertMatchesContract('setAdvisorRuleOverride', 200, disabled)
    assertMatchesContract('setAdvisorRuleOverride', 200, enabledAgain)
    const after = catalogue.map((rule) => (rule.id === 'index.unused' ? disabled : rule))
    server.use(sequence('getAdvisorRules', catalogue, after, catalogue))
    const bodies = captureOverride((_id, body) =>
      HttpResponse.json(body.enabled ? enabledAgain : disabled),
    )
    await renderPanel()

    const table = screen.getByRole('table', { name: TABLE })
    expect(within(table).getAllByRole('switch')).toHaveLength(4)
    // The rule disabled from the console says so, and the summary counts it.
    expect(ruleSwitch('config.checkpoints_too_frequent')).toHaveAttribute('aria-checked', 'false')
    expect(screen.getByTestId('advisor-rules-disabled-count')).toHaveTextContent('1 of 4 disabled')
    // Every switch states its effect (I-16).
    expect(ruleSwitch('index.unused')).toHaveAccessibleDescription(
      /resolved at the next evaluation/,
    )

    expect(ruleSwitch('index.unused')).toHaveAttribute('aria-checked', 'true')
    fireEvent.click(ruleSwitch('index.unused'))
    await flushNetwork()
    expect(bodies).toEqual([{ id: 'index.unused', body: { enabled: false } }])
    await flushNetwork()
    expect(ruleSwitch('index.unused')).toHaveAttribute('aria-checked', 'false')
    expect(screen.getByTestId('advisor-rules-disabled-count')).toHaveTextContent('2 of 4 disabled')

    fireEvent.click(ruleSwitch('index.unused'))
    await flushNetwork()
    expect(bodies[1]).toEqual({ id: 'index.unused', body: { enabled: true } })
    await flushNetwork()
    expect(ruleSwitch('index.unused')).toHaveAttribute('aria-checked', 'true')
  })

  it('ADVO-UI-002 a failed write keeps the previous value and says why on the row', async () => {
    server.use(ok('getAdvisorRules', catalogue))
    captureOverride(() =>
      HttpResponse.json({ error: 'store unavailable', detail: '' }, { status: 503 }),
    )
    await renderPanel()

    fireEvent.click(ruleSwitch('txn.long_running'))
    await flushNetwork()

    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent('txn.long_running was not changed: the store is unavailable.')
    expect(ruleSwitch('txn.long_running')).toHaveAttribute('aria-checked', 'true')
    expect(ruleSwitch('txn.long_running')).toBeEnabled()
    expect(ruleSwitch('txn.long_running').getAttribute('aria-describedby')).toContain(alert.id)
    // The other rules are untouched.
    expect(ruleSwitch('index.unused')).toHaveAttribute('aria-checked', 'true')

    // A 404 names the rule as unknown to the server.
    captureOverride(() => HttpResponse.json({ error: 'unknown rule' }, { status: 404 }))
    fireEvent.click(ruleSwitch('index.unused'))
    await flushNetwork()
    expect(screen.getAllByRole('alert').map((node) => node.textContent)).toContain(
      'index.unused was not changed: the server does not know it. Reload the page.',
    )
    expect(ruleSwitch('index.unused')).toHaveAttribute('aria-checked', 'true')
  })

  it('ADVO-UI-003 the rules table searches, filters, sorts and pages', async () => {
    const many: AdvisorRule[] = Array.from({ length: 25 }, (_, index) => ({
      id: `${index % 2 === 0 ? 'index' : 'query'}.rule_${String(index).padStart(2, '0')}`,
      severity: index % 5 === 0 ? 'critical' : 'warning',
      scope: 'instance',
      needs: [],
      min_tier: 'T0',
      enabled: index !== 3,
      overridden: index === 3,
    }))
    server.use(ok('getAdvisorRules', many))
    const { router } = await renderPanel()

    // 20 rows per page by default (the tablePageSize preference).
    expect(bodyRows()).toHaveLength(20)
    expect(screen.getByText('Showing 1–20 of 25 rules.')).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    expect(bodyRows()).toHaveLength(5)
    expect(router.state.location.search).toContain('advisor-rules.page=2')

    // The category filter offers the categories of the catalogue.
    fireEvent.change(screen.getByRole('combobox', { name: 'Filter Advisor rules by Category' }), {
      target: { value: 'query' },
    })
    expect(bodyRows()).toHaveLength(12)
    expect(firstCells().every((text) => text?.startsWith('query.'))).toBe(true)

    fireEvent.change(screen.getByRole('combobox', { name: 'Filter Advisor rules by Runs' }), {
      target: { value: 'disabled' },
    })
    expect(firstCells()).toEqual(['query.rule_03'])
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))

    fireEvent.change(screen.getByRole('combobox', { name: 'Filter Advisor rules by Severity' }), {
      target: { value: 'critical' },
    })
    expect(bodyRows()).toHaveLength(5)
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))

    fireEvent.change(screen.getByRole('searchbox', { name: 'Search Advisor rules' }), {
      target: { value: 'rule_1' },
    })
    expect(bodyRows()).toHaveLength(10)

    fireEvent.change(screen.getByRole('searchbox', { name: 'Search Advisor rules' }), {
      target: { value: '' },
    })
    const idHeader = screen.getByRole('columnheader', { name: /Rule ID/ })
    fireEvent.click(within(idHeader).getByRole('button', { name: 'Sort Rule ID descending' }))
    expect(idHeader).toHaveAttribute('aria-sort', 'descending')
    expect(firstCells()[0]).toBe('query.rule_23')
  })

  it('ADVW-UI-001 two windows with units, bounds messages and the false-positive warning; Save sends the changed key, Reset sends null', async () => {
    server.use(sequence('getAdvisorWindows', windows, customWindows, windows))
    assertMatchesContract('setAdvisorWindows', 200, customWindows)
    const bodies = captureWindows((body) =>
      HttpResponse.json(body['window.unused_index'] === null ? windows : customWindows),
    )
    await renderPanel(windowsView())

    expect(screen.getByText('Short windows can raise false positives.')).toBeVisible()
    // The advisor applies the stored windows (13.2); the note says how (13.3).
    expect(screen.getByRole('note')).toHaveTextContent(
      'Saved windows apply at the next advisor evaluation. On PostgreSQL 16 and later the statistics age counts only for an index never scanned since the reset; a base backup stays recent for 7 days (30 days for a backup strategy) whatever the window.',
    )
    const unused = screen.getByRole('textbox', { name: 'Unused-index history' })
    const backup = screen.getByRole('textbox', { name: 'Backup history' })
    expect(unused).toHaveValue('7')
    expect(backup).toHaveValue('7')
    const unit = screen.getByRole('combobox', { name: 'Unused-index history unit' })
    expect(unit).toHaveValue('days')
    const save = screen.getByRole('button', { name: 'Save windows' })
    expect(save).toBeDisabled()
    // Every control states its effect (I-16).
    for (const control of [unused, backup, unit, save]) {
      expect(control.getAttribute('aria-describedby')).toBeTruthy()
    }

    fireEvent.change(unused, { target: { value: '91' } })
    expect(screen.getByText('Enter a window between 1 hour and 90 days.')).toBeVisible()
    expect(unused).toHaveAttribute('aria-invalid', 'true')
    expect(save).toBeDisabled()
    fireEvent.change(unused, { target: { value: '1.5' } })
    expect(screen.getByText('Enter a whole number of days.')).toBeVisible()
    fireEvent.change(unused, { target: { value: '' } })
    expect(screen.getByText('Enter a window between 1 hour and 90 days.')).toBeVisible()

    fireEvent.change(unit, { target: { value: 'hours' } })
    fireEvent.change(unused, { target: { value: '1' } })
    expect(unused).not.toHaveAttribute('aria-invalid')
    expect(screen.getByText(/Shorter than the 7-day default/)).toBeVisible()
    expect(save).toBeEnabled()
    fireEvent.click(save)
    await flushNetwork()
    expect(bodies).toEqual([{ 'window.unused_index': 3600 }])
    expect(screen.getByText(/Saved: Unused-index history 1 hour/)).toBeVisible()
    await flushNetwork()
    expect(screen.getByRole('textbox', { name: 'Unused-index history' })).toHaveValue('1')

    const reset = screen.getByRole('button', { name: 'Reset Unused-index history to default' })
    expect(screen.getByRole('button', { name: 'Reset Backup history to default' })).toBeDisabled()
    fireEvent.click(reset)
    await flushNetwork()
    expect(bodies[1]).toEqual({ 'window.unused_index': null })
    await flushNetwork()
    expect(screen.getByRole('textbox', { name: 'Unused-index history' })).toHaveValue('7')
  })

  it('ADVW-UI-001 a stored window that is not a whole number of hours is shown, never rewritten', async () => {
    const stored = [
      { ...customWindows[0]!, seconds: 5400, source: 'custom' as const },
      customWindows[1]!,
    ]
    server.use(ok('getAdvisorWindows', stored))
    const bodies = captureWindows(() => HttpResponse.json(stored))
    await renderPanel(windowsView())

    const unused = screen.getByRole('textbox', { name: 'Unused-index history' })
    expect(unused).toHaveValue('1.5')
    expect(unused).not.toHaveAttribute('aria-invalid')
    expect(screen.getByText('5400 seconds')).toBeVisible()
    const save = screen.getByRole('button', { name: 'Save windows' })
    expect(save).toBeDisabled()

    fireEvent.change(screen.getByRole('textbox', { name: 'Backup history' }), {
      target: { value: '14' },
    })
    expect(save).toBeEnabled()
    fireEvent.click(save)
    await flushNetwork()
    expect(bodies).toEqual([{ 'window.backup_history': 1209600 }])
  })

  it('ADVW-UI-001 a refused save keeps the typed value and the stored one (I-11)', async () => {
    server.use(ok('getAdvisorWindows', windows))
    captureWindows(() =>
      HttpResponse.json(
        { error: 'invalid seconds', detail: 'window.backup_history' },
        { status: 400 },
      ),
    )
    await renderPanel(windowsView())

    const backup = screen.getByRole('textbox', { name: 'Backup history' })
    fireEvent.change(backup, { target: { value: '30' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save windows' }))
    await flushNetwork()

    expect(screen.getByRole('alert')).toHaveTextContent(
      /The observation windows was not changed: invalid seconds/,
    )
    expect(backup).toHaveValue('30')
    expect(screen.getAllByText('7 days').length).toBeGreaterThan(0)
  })

  it('ADVW-UI-002 the rules view has no serious accessibility violations', async () => {
    server.use(ok('getAdvisorRules', catalogue))
    const view = await renderPanel()
    expect(screen.getByRole('table', { name: TABLE })).toBeInTheDocument()

    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
  }, 15000)

  it('ADVW-UI-002 the windows view, reached from its view tab, has no serious accessibility violations', async () => {
    server.use(ok('getAdvisorRules', catalogue), ok('getAdvisorWindows', customWindows))
    const view = await renderPanel()
    fireEvent.mouseDown(
      within(screen.getByRole('tablist', { name: 'Advisor views' })).getByRole('tab', {
        name: 'Observation windows',
      }),
    )
    await flushNetwork()
    expect(view.router.state.location.search).toContain('view=windows')
    expect(screen.getByRole('textbox', { name: 'Unused-index history' })).toHaveValue('1')
    expect(screen.getByRole('combobox', { name: 'Unused-index history unit' })).toHaveValue('hours')

    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
  }, 15000)

  it('ADVW-UI-002 a failed load offers a retry and has no serious accessibility violations', async () => {
    server.use(status('getAdvisorWindows', 503, { error: 'store unavailable', detail: '' }))
    const view = await renderPanel(windowsView())
    expect(screen.getByRole('alert')).toHaveTextContent('Could not load advisor windows.')
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()

    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
  }, 15000)
})
