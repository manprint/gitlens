import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { expectNoA11yViolations } from '@/test/a11y'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { base, complete, oldAgent } from '@/test/fixtures/getCoverage'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'

import { CoverageBanner } from './CoverageBanner'

type Coverage = Schemas['CoverageResponse']
type CoverageCheck = Coverage['checks'][number]

/**
 * Eighteen checks two minutes after the agent started: fourteen reported, the
 * other four pending, the last of them expected 9.5 minutes after the response.
 */
function eighteenChecks(): Coverage {
  const checks: CoverageCheck[] = Array.from({ length: 18 }, (_, index) => {
    const name = `check_${String(index + 1).padStart(2, '0')}`
    return index < 14
      ? {
          check: name,
          interval_seconds: 30,
          state: 'reported',
          first_result_at: '2026-08-27T01:58:30Z',
          last_result_at: '2026-08-27T01:59:30Z',
          expected_by: null,
        }
      : {
          check: name,
          interval_seconds: 3600,
          state: 'pending',
          first_result_at: null,
          last_result_at: null,
          expected_by: index === 17 ? '2026-08-27T02:09:30Z' : '2026-08-27T02:01:00Z',
        }
  })
  return {
    instance_id: INSTANCE_ID,
    agent_started_at: '2026-08-27T01:58:00Z',
    generated_at: '2026-08-27T02:00:00Z',
    checks,
    reported: 14,
    total: 18,
  }
}

async function renderBanner(coverage: Coverage, compact = false) {
  server.use(ok('getCoverage', coverage))
  const view = renderWithProviders(<CoverageBanner compact={compact} instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}`,
  })
  await flushNetwork()
  return view
}

describe('CoverageBanner', () => {
  it('COVUI-001 shows how many checks have a current result and when the rest are expected', async () => {
    await renderBanner(eighteenChecks())

    const banner = screen.getByTestId('coverage-banner')
    expect(banner).toHaveTextContent(
      'Collecting data: 14 of 18 checks have a current result — the rest are expected within 10 min.',
    )
    expect(banner).toHaveAttribute('data-caption-id', 'instance.coverage')
    expect(
      within(banner).getByRole('button', { name: 'About Data collection' }),
    ).toBeInTheDocument()
  })

  it('COVUI-001 rounds the remaining time up to whole minutes', async () => {
    const coverage = eighteenChecks()
    // The last pending check is due 30 s after the response: within 1 min, not 0.
    await renderBanner({ ...coverage, generated_at: '2026-08-27T02:09:00Z' })

    expect(screen.getByTestId('coverage-banner')).toHaveTextContent(
      'Collecting data: 14 of 18 checks have a current result — the rest are expected within 1 min.',
    )
  })

  it('COVUI-001 says the rest are overdue once their expected time has passed', async () => {
    const late = eighteenChecks()
    await renderBanner({ ...late, generated_at: '2026-08-27T02:15:00Z' })

    expect(screen.getByTestId('coverage-banner')).toHaveTextContent(
      'Collecting data: 14 of 18 checks have a current result — the rest are overdue.',
    )
  })

  it('COVUI-001 leaves the expected time out when the agent does not report its start', async () => {
    const coverage = eighteenChecks()
    await renderBanner({
      ...coverage,
      agent_started_at: null,
      checks: coverage.checks.map((check) => ({ ...check, expected_by: null })),
    })

    expect(screen.getByTestId('coverage-banner')).toHaveTextContent(
      /^Collecting data: 14 of 18 checks have a current result\.$/,
    )
  })

  it('COVUI-002 is hidden once every check has reported', async () => {
    const coverage = eighteenChecks()
    await renderBanner({
      ...coverage,
      checks: coverage.checks.map((check) => ({
        ...check,
        state: 'reported',
        first_result_at: '2026-08-27T01:59:00Z',
        last_result_at: '2026-08-27T01:59:00Z',
        expected_by: null,
      })),
      reported: 18,
    })

    expect(screen.queryByTestId('coverage-banner')).not.toBeInTheDocument()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('COVUI-002 is hidden for the complete fixture and for an old agent with everything stored', async () => {
    const view = await renderBanner(complete)
    expect(screen.queryByTestId('coverage-banner')).not.toBeInTheDocument()
    view.unmount()

    await renderBanner(oldAgent)
    expect(screen.queryByTestId('coverage-banner')).not.toBeInTheDocument()
  })

  it('COVUI-003 names a check whose last run failed', async () => {
    await renderBanner(base)

    const banner = screen.getByTestId('coverage-banner')
    expect(banner).toHaveTextContent(
      'Collecting data: 2 of 4 checks have a current result — the pending ones are expected within 8 min.',
    )
    expect(within(banner).getByTestId('coverage-failed')).toHaveTextContent(
      'Failed on its last run: locks.',
    )
  })

  it('COVUI-003 stays while a failed check is the only one missing', async () => {
    await renderBanner({
      ...complete,
      checks: complete.checks.map((check) =>
        check.check === 'locks' || check.check === 'settings'
          ? { ...check, state: 'failed' }
          : check,
      ),
      reported: 2,
    })

    const banner = screen.getByTestId('coverage-banner')
    expect(banner).toHaveTextContent('Incomplete data: 2 of 4 checks have a current result.')
    expect(within(banner).getByTestId('coverage-failed')).toHaveTextContent(
      'Failed on their last run: locks, settings.',
    )
  })

  it('COVUI-004 is a polite live region', async () => {
    await renderBanner(base)

    const banner = screen.getByRole('status')
    expect(banner).toBe(screen.getByTestId('coverage-banner'))
    expect(banner).toHaveAttribute('aria-live', 'polite')
  })

  it('COVUI-007 in the header row keeps the count whole and truncates only the rest', async () => {
    await renderBanner(base, true)

    const banner = screen.getByRole('status')
    expect(banner).toHaveAttribute('aria-live', 'polite')
    // The title sits on the truncated line, not on the live region, so a
    // screen reader does not read the text twice.
    expect(banner).not.toHaveAttribute('title')
    const count = within(banner).getByTestId('coverage-count')
    expect(count).toHaveTextContent('2/4 checks')
    expect(count).toHaveClass('shrink-0')
    expect(count).not.toHaveClass('truncate')
    const line = within(banner).getByTestId('coverage-line')
    expect(line).toHaveClass('truncate')
    expect(line).toHaveTextContent(
      'Collecting data — the pending ones are expected within 8 min. Failed on its last run: locks.',
    )
    expect(line).toHaveAttribute(
      'title',
      'Collecting data: 2 of 4 checks have a current result — the pending ones are expected within 8 min. Failed on its last run: locks.',
    )
  })

  it('COVUI-007 gives the whole sentence and the failed checks to keyboard and touch users', async () => {
    await renderBanner(base, true)

    const hint = within(screen.getByRole('status')).getByRole('button', {
      name: 'About Data collection',
    })
    // Keyboard focus opens the tooltip, which starts with the live state and
    // is the button's description.
    fireEvent.focus(hint)
    const tooltip = screen.getByRole('tooltip')
    expect(hint).toHaveAccessibleDescription(
      expect.stringContaining(
        'Collecting data: 2 of 4 checks have a current result — the pending ones are expected within 8 min. Failed on its last run: locks.',
      ),
    )
    expect(within(tooltip).getByTestId('coverage-hint-sentence')).toHaveTextContent(
      'Collecting data: 2 of 4 checks have a current result — the pending ones are expected within 8 min.',
    )
    expect(within(tooltip).getByTestId('coverage-hint-failed')).toHaveTextContent(
      'Failed on its last run: locks.',
    )

    // A tap pins it open.
    fireEvent.blur(hint)
    fireEvent.click(hint)
    expect(hint).toHaveAttribute('aria-expanded', 'true')
    expect(tooltip).toBeVisible()
  })

  it('COVUI-006 has no serious accessibility violations in the header row', async () => {
    const view = await renderBanner(base, true)
    expect(screen.getByTestId('coverage-banner')).toBeInTheDocument()

    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
  }, 15000)

  it('COVUI-006 has no serious accessibility violations', async () => {
    const view = await renderBanner(base)
    expect(screen.getByTestId('coverage-banner')).toBeInTheDocument()

    // axe schedules its work on real timers.
    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
  }, 15000)
})
