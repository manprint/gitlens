import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { expectNoA11yViolations } from '@/test/a11y'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { base as fullReport, partial as partialReport } from '@/test/fixtures/getReadiness'
import { ok, status } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'

import { ReadinessBadge } from './ReadinessBadge'
import { ReadinessBanner } from './ReadinessBanner'
import { ReadinessSection } from './ReadinessSection'

type Report = Schemas['ReadinessReport']

async function renderBanner(report: Report, compact = false) {
  server.use(ok('getReadiness', report))
  const view = renderWithProviders(<ReadinessBanner compact={compact} instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}`,
  })
  await flushNetwork()
  return view
}

async function renderSection(report: Report) {
  server.use(ok('getReadiness', report))
  const view = renderWithProviders(<ReadinessSection instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}?section=readiness`,
  })
  await flushNetwork()
  return view
}

describe('readiness UI', () => {
  it('RDY-UI-001 the banner is hidden when full and shows the warning count when partial', async () => {
    const hidden = await renderBanner(fullReport)
    expect(screen.queryByTestId('readiness-banner')).toBeNull()
    hidden.unmount()

    await renderBanner(partialReport)
    const banner = screen.getByTestId('readiness-banner')
    expect(banner).toHaveTextContent('Partial · 1 warning')
    const link = within(banner).getByRole('link')
    expect(link).toHaveAttribute('href', `/instances/${INSTANCE_ID}?section=readiness`)
    expect(banner).toHaveAttribute('data-caption-id', 'instance.readiness')
  })

  it('RDY-UI-002 the section lists warnings before notes, with impact and evidence', async () => {
    await renderSection(partialReport)
    const items = screen.getAllByTestId('readiness-item')
    expect(items.map((item) => item.getAttribute('data-severity'))).toEqual(['warning', 'info'])
    expect(within(items[0]!).getByText('What you lose')).toBeInTheDocument()
    expect(within(items[0]!).getByText('I/O time charts')).toBeInTheDocument()
    expect(items[0]).toHaveTextContent('Evidence: track_io_timing = off')
  })

  it('RDY-UI-003 an SQL fix shows its steps in order and a copy button for the snippet', async () => {
    await renderSection(partialReport)
    const fix = within(screen.getAllByTestId('readiness-item')[0]!).getByTestId('readiness-fix')
    const steps = within(fix).getAllByRole('listitem')
    expect(steps[0]).toHaveTextContent('Run the statements below as a superuser.')
    expect(steps[1]).toHaveTextContent('No restart is needed.')
    expect(fix).toHaveTextContent('ALTER SYSTEM SET track_io_timing = on;')
    expect(within(fix).getByRole('button', { name: /^Copy/ })).toBeInTheDocument()
    expect(within(fix).queryByText('Requires restart')).toBeNull()
  })

  it('RDY-UI-004 a console fix links to its route and a restart fix says so', async () => {
    const report: Report = {
      ...partialReport,
      items: [
        {
          ...partialReport.items[0]!,
          fixes: [
            {
              channel: 'console',
              title: 'Turn it on in Settings',
              steps: ['Open Settings.'],
              requires_restart: false,
              route: '/settings/notifications',
            },
            {
              channel: 'postgres_config',
              title: 'Edit postgresql.conf',
              steps: ['Set the value.'],
              requires_restart: true,
            },
          ],
        },
      ],
    }
    await renderSection(report)
    expect(screen.getByRole('link', { name: 'Open in the console' })).toHaveAttribute(
      'href',
      '/settings/notifications',
    )
    expect(screen.getByText('Requires restart')).toBeInTheDocument()
    const fixes = screen.getByTestId('readiness-fixes')
    const channels = within(fixes)
      .getAllByRole('heading', { level: 4 })
      .map((heading) => heading.textContent)
    expect(channels).toEqual(['In the console', 'PostgreSQL configuration'])
  })

  it('RDY-UI-005 notes are collapsed by default and open on demand', async () => {
    await renderSection(partialReport)
    const note = screen.getAllByTestId('readiness-item')[1]!
    expect(within(note).queryByTestId('readiness-fixes')).toBeNull()
    const toggle = within(note).getByRole('button', { name: 'Show details' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(toggle)
    expect(within(note).getByRole('button', { name: 'Hide details' })).toHaveAttribute(
      'aria-expanded',
      'true',
    )
  })

  it('RDY-UI-012 with several warnings every item starts collapsed, so the tab fits Full HD', async () => {
    const warning = partialReport.items[0]!
    await renderSection({
      ...partialReport,
      counts: { warning: 3, info: 0 },
      items: [
        { ...warning, id: 'RDY-A-01' },
        { ...warning, id: 'RDY-A-02' },
        { ...warning, id: 'RDY-A-03' },
      ],
    })
    expect(screen.getAllByRole('button', { name: 'Show details' })).toHaveLength(3)
    expect(screen.queryByTestId('readiness-fixes')).toBeNull()
  })

  it('RDY-UI-012 with several warnings every item starts collapsed, so the tab fits Full HD', async () => {
    const warning = partialReport.items[0]!
    await renderSection({
      ...partialReport,
      counts: { warning: 3, info: 0 },
      items: [
        { ...warning, id: 'RDY-A-01' },
        { ...warning, id: 'RDY-A-02' },
        { ...warning, id: 'RDY-A-03' },
      ],
    })
    expect(screen.getAllByRole('button', { name: 'Show details' })).toHaveLength(3)
    expect(screen.queryByTestId('readiness-fixes')).toBeNull()
  })

  it('RDY-UI-006 the badge text and accessible label', () => {
    const full = renderWithProviders(<ReadinessBadge level="full" warnings={0} />)
    expect(screen.getByTestId('readiness-badge')).toHaveTextContent('Full')
    expect(screen.getByLabelText('Monitoring: full')).toBeInTheDocument()
    full.unmount()
    renderWithProviders(<ReadinessBadge infos={2} level="partial" warnings={3} />)
    expect(screen.getByTestId('readiness-badge')).toHaveTextContent('Partial · 3 warnings')
    expect(screen.getByLabelText('Monitoring: partial, 3 warnings, 2 notes')).toBeInTheDocument()
  })

  it('RDY-UI-007 loading, error and empty states', async () => {
    server.use(ok('getReadiness', fullReport))
    const view = renderWithProviders(<ReadinessSection instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}`,
    })
    expect(screen.getByRole('status', { name: 'Loading readiness' })).toBeInTheDocument()
    await flushNetwork()
    expect(screen.getByText('Monitored in full')).toBeInTheDocument()
    view.unmount()

    server.resetHandlers()
    server.use(status('getReadiness', 500, { error: 'internal_error', detail: 'unavailable' }))
    renderWithProviders(<ReadinessSection instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}`,
    })
    await flushNetwork()
    expect(screen.getByRole('alert')).toHaveTextContent('Could not load instance readiness')
  })

  it('RDY-UI-008 API text is rendered as text, never as markup', async () => {
    const report: Report = {
      ...partialReport,
      items: [
        {
          ...partialReport.items[0]!,
          title: '<script>window.__pwned = 1</script>',
          summary: '<img src=x onerror="window.__pwned = 1">',
        },
      ],
    }
    await renderSection(report)
    expect(screen.getByText('<script>window.__pwned = 1</script>')).toBeInTheDocument()
    expect(screen.queryByRole('img')).toBeNull()
    expect((window as unknown as { __pwned?: number }).__pwned).toBeUndefined()
  })

  it('RDY-UI-009 the banner has no accessibility violations', async () => {
    const view = await renderBanner(partialReport)
    expect(screen.getByTestId('readiness-banner')).toBeInTheDocument()
    // axe schedules its work on real timers.
    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
  })

  it('RDY-UI-009 the section has no accessibility violations', async () => {
    const view = await renderSection(partialReport)
    expect(screen.getAllByTestId('readiness-item')).toHaveLength(2)
    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
  })
})
