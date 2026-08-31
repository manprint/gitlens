import { act, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Cluster, Schemas } from '@/api/types'
import { expectNoA11yViolations } from '@/test/a11y'
import { CLUSTER, CLUSTER_ID, INSTANCE_ID } from '@/test/fixture-helpers'
import { makeGetInstance } from '@/test/fixtures/getInstance'
import { renderWithProviders } from '@/test/render'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'

import { InstancePage } from './InstancePage'

const cluster = {
  ...(CLUSTER as unknown as Cluster),
  max_replay_lag_seconds: 1.25,
}
const baseInstance = makeGetInstance() as unknown as Schemas['Instance']

function makeInstance(overrides: Partial<Schemas['Instance']> = {}): Schemas['Instance'] {
  return { ...baseInstance, ...overrides }
}

async function settle() {
  await act(async () => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      await vi.advanceTimersByTimeAsync(0)
      await Promise.resolve()
    }
  })
}

function renderPage(instance: Schemas['Instance'] = baseInstance) {
  server.use(ok('getInstance', instance), ok('getClusters', [cluster]))
  return renderWithProviders(<InstancePage instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}`,
  })
}

describe('InstancePage', () => {
  it('UI-INST-002 renders a standby read-only banner with its replay lag', async () => {
    renderPage(makeInstance({ pg_version: 170011, role: 'standby' }))
    await settle()

    expect(screen.getByText('standby (read-only)')).toBeInTheDocument()
    expect(screen.getByText('Replay lag: 1.25 s')).toBeInTheDocument()
    expect(screen.getByText('17.11')).toBeInTheDocument()
    vi.useRealTimers()
    await expectNoA11yViolations(document.body)
  })

  it('UI-INST-003 renders no read-only banner for a primary', async () => {
    renderPage(makeInstance({ pg_version: 160002, role: 'primary' }))
    await settle()

    expect(screen.queryByText('standby (read-only)')).not.toBeInTheDocument()
    expect(screen.getByText('16.2')).toBeInTheDocument()
  })

  it('UI-INST-004 marks an instance with up=false as down', async () => {
    renderPage(makeInstance({ up: false }))
    await settle()

    expect(screen.getByText('down')).toBeInTheDocument()
    expect(
      screen.getByText('Instance is down and its latest values may be stale.'),
    ).toBeInTheDocument()
  })

  it('UI-INST-005 links the owning cluster with the exact cluster id string', async () => {
    renderPage()
    await settle()

    expect(screen.getByRole('link', { name: CLUSTER_ID })).toHaveAttribute(
      'href',
      `/clusters/${CLUSTER_ID}`,
    )
  })
})
