import { act, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { expectNoA11yViolations } from '@/test/a11y'
import { renderWithProviders } from '@/test/render'

import { ChartNav } from './ChartNav'
import { OVERVIEW_NAV_LINKS } from './overviewCharts'

type Callback = (entries: Pick<IntersectionObserverEntry, 'isIntersecting' | 'target'>[]) => void

const observers: FakeObserver[] = []

class FakeObserver {
  readonly observed: Element[] = []
  disconnected = false
  constructor(
    readonly callback: Callback,
    readonly options: IntersectionObserverInit,
  ) {
    observers.push(this)
  }
  observe(target: Element) {
    this.observed.push(target)
  }
  disconnect() {
    this.disconnected = true
  }
}

function Page() {
  return (
    <>
      <ChartNav links={OVERVIEW_NAV_LINKS} />
      {OVERVIEW_NAV_LINKS.map((link) => (
        <section aria-label={`${link.label} target`} id={link.id} key={link.id} />
      ))}
    </>
  )
}

function labelOf(id: string) {
  return OVERVIEW_NAV_LINKS.find((link) => link.id === id)?.label ?? id
}

function report(entries: [id: string, visible: boolean][]) {
  const observer = observers.at(-1)
  act(() => {
    observer?.callback(
      entries.map(([id, isIntersecting]) => ({
        isIntersecting,
        target: screen.getByRole('region', { name: `${labelOf(id)} target` }),
      })),
    )
  })
}

function current() {
  return screen
    .getAllByRole('link')
    .filter((link) => link.getAttribute('aria-current') === 'location')
    .map((link) => link.textContent)
}

afterEach(() => {
  observers.length = 0
  vi.unstubAllGlobals()
})

describe('ChartNav', () => {
  it('OVC-005 lists six links to the groups and the Host section', () => {
    vi.stubGlobal('IntersectionObserver', FakeObserver)
    renderWithProviders(<Page />, { route: '/instances/i-1?db=app' })

    const nav = screen.getByRole('navigation', { name: 'Chart groups' })
    const links = screen.getAllByRole('link')
    expect(nav).toContainElement(links[0] ?? null)
    expect(links.map((link) => [link.textContent, link.getAttribute('href')])).toEqual([
      ['Workload', '#workload'],
      ['Memory & I/O', '#memory-io'],
      ['WAL & checkpoints', '#wal-checkpoints'],
      ['Queries', '#queries'],
      ['Tables & indexes', '#tables-indexes'],
      // Host is a tab of the instance page: the link switches to it, keeping the database.
      ['Host', '/instances/i-1?db=app&section=host'],
    ])
    expect(nav).toHaveClass('xl:sticky', 'xl:top-14')
  })

  it('OVC-005 marks the first visible group with aria-current and follows the scroll', () => {
    vi.stubGlobal('IntersectionObserver', FakeObserver)
    const view = renderWithProviders(<Page />, { route: '/instances/i-1?db=app' })
    const observer = observers.at(-1)
    // The Host tab link has no target on the Overview: only the groups are observed.
    expect(observer?.observed.map((element) => element.id)).toEqual(
      OVERVIEW_NAV_LINKS.filter((link) => link.section === undefined).map((link) => link.id),
    )
    expect(observer?.options.rootMargin).toBe('-120px 0px -60% 0px')
    expect(current()).toEqual([])

    report([
      ['memory-io', true],
      ['workload', true],
    ])
    expect(current()).toEqual(['Workload'])

    report([['workload', false]])
    expect(current()).toEqual(['Memory & I/O'])

    // Leaving every group keeps the last one marked.
    report([['memory-io', false]])
    expect(current()).toEqual(['Memory & I/O'])

    view.unmount()
    expect(observer?.disconnected).toBe(true)
  })

  it('OVC-005 renders the links without a current one when IntersectionObserver is missing', async () => {
    vi.stubGlobal('IntersectionObserver', undefined)
    const { container } = renderWithProviders(<Page />, { route: '/instances/i-1?db=app' })

    expect(screen.getAllByRole('link')).toHaveLength(6)
    expect(current()).toEqual([])
    vi.useRealTimers()
    await expectNoA11yViolations(container)
  })
})
