import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { Schemas } from '@/api/types'
import { captionOf } from '@/lib/captions'
import { expectCaptionedCards } from '@/test/clearCard'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { base as coverage } from '@/test/fixtures/getCoverage'
import { partial as partialReport } from '@/test/fixtures/getReadiness'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'

import { CoverageBanner } from '../instance/CoverageBanner'
import { ReadinessSection } from './ReadinessSection'

type Report = Schemas['ReadinessReport']

const restartReport: Report = {
  ...partialReport,
  items: [
    {
      ...partialReport.items[0]!,
      fixes: [
        {
          channel: 'postgres_config',
          title: 'Edit postgresql.conf',
          steps: ['Set the value.'],
          requires_restart: true,
        },
      ],
    },
    ...partialReport.items.slice(1),
  ],
}

async function renderSection(report: Report) {
  server.use(ok('getReadiness', report))
  const view = renderWithProviders(<ReadinessSection instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}?section=readiness`,
  })
  await flushNetwork()
  return view
}

describe('readiness and coverage captions', () => {
  it('CAPTION-PAGE-050 the readiness section is captioned', async () => {
    const { container } = await renderSection(partialReport)

    expect(expectCaptionedCards(container)).toEqual(['instance.readiness'])
    expect(screen.getAllByText(captionOf('instance.readiness').what).length).toBeGreaterThan(0)
  })

  it('CAPTION-PAGE-051 the severity and restart badges carry their caption', async () => {
    await renderSection(restartReport)

    const severity = screen.getAllByTitle(captionOf('readiness.severity').what)
    expect(severity.length).toBeGreaterThan(0)
    severity.forEach((badge) =>
      expect(badge).toHaveAttribute('data-caption-id', 'readiness.severity'),
    )
    const restart = screen.getByText('Requires restart')
    expect(restart).toHaveAttribute('data-caption-id', 'readiness.requires-restart')
    expect(restart).toHaveAttribute('title', captionOf('readiness.requires-restart').what)
  })

  it('CAPTION-PAGE-052 the coverage banner is captioned', async () => {
    server.use(ok('getCoverage', coverage))
    const { container } = renderWithProviders(<CoverageBanner instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}`,
    })
    await flushNetwork()

    expectCaptionedCards(container)
    expect(screen.getByTestId('coverage-banner')).toHaveAttribute(
      'data-caption-id',
      'instance.coverage',
    )
  })
})
