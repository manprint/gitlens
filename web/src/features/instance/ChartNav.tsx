import { useEffect, useState } from 'react'

import { Link, useLocation } from 'react-router-dom'

import type { MessageKey } from '@/i18n/messages/en'
import { useT } from '@/i18n/react'
import { cn } from '@/lib/utils'

export interface ChartNavLink {
  /** Element id of the target on the same page. */
  id: string
  label: string
  /** Catalog key of the label; without it the English label is shown. */
  labelKey?: MessageKey
  /** An instance-page tab (`?section=`) instead of an anchor on this page. */
  section?: string
}

interface ChartNavProps {
  links: readonly ChartNavLink[]
}

/**
 * The band below the sticky header in which a group counts as the one being
 * read: from just under the header and this bar to 60 % of the viewport.
 */
const READING_BAND = '-120px 0px -60% 0px'

/**
 * Anchor links to the chart groups, kept in view while the charts scroll from
 * the `xl` width up; below it the header wraps to a variable height, so the
 * bar stays in the flow instead of sliding under the header.
 * The group in the reading band is marked with `aria-current`.
 */
const LINK =
  'text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring inline-flex rounded-md px-2.5 py-1 text-sm font-medium focus-visible:ring-2 focus-visible:outline-none'

/** The current query string with `section` set, the other parameters kept. */
function withSection(search: string, section: string): string {
  const params = new URLSearchParams(search)
  params.set('section', section)
  return `?${params.toString()}`
}

export function ChartNav({ links }: ChartNavProps) {
  const t = useT()
  const labelOf = (link: ChartNavLink) =>
    link.labelKey === undefined ? link.label : t(link.labelKey)
  const current = useCurrentTarget(links.filter((link) => link.section === undefined))
  const { search } = useLocation()

  return (
    <nav
      aria-label={t('instance.chartNav.label')}
      className="bg-card/95 supports-[backdrop-filter]:bg-card/80 shadow-card rounded-xl border px-3 py-2 backdrop-blur xl:sticky xl:top-14 xl:z-10"
    >
      <ul className="flex gap-1.5 overflow-x-auto whitespace-nowrap">
        {links.map((link) => (
          <li key={link.id}>
            {link.section === undefined ? (
              <a
                aria-current={current === link.id ? 'location' : undefined}
                className={cn(LINK, current === link.id && 'bg-muted text-foreground')}
                href={`#${link.id}`}
              >
                {labelOf(link)}
              </a>
            ) : (
              <Link
                className={LINK}
                replace
                state={{ focusSection: true }}
                to={{ search: withSection(search, link.section) }}
              >
                {labelOf(link)}
              </Link>
            )}
          </li>
        ))}
      </ul>
    </nav>
  )
}

function useCurrentTarget(links: readonly ChartNavLink[]): string | null {
  const [current, setCurrent] = useState<string | null>(null)
  const ids = links.map((link) => link.id).join(' ')

  useEffect(() => {
    if (typeof IntersectionObserver === 'undefined') return undefined
    const order = ids.split(' ')
    const visible = new Set<string>()
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) visible.add(entry.target.id)
          else visible.delete(entry.target.id)
        }
        // The first group of the page order that is in the band.
        const first = order.find((id) => visible.has(id))
        if (first !== undefined) setCurrent(first)
      },
      { rootMargin: READING_BAND },
    )
    for (const id of order) {
      const target = document.getElementById(id)
      if (target) observer.observe(target)
    }
    return () => observer.disconnect()
  }, [ids])

  return current
}
