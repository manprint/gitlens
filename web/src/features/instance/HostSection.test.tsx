import { act, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { expectNoA11yViolations } from '@/test/a11y'
import { INSTANCE_ID, NOW } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { FILESYSTEM_HINT, NO_SWAP_TEXT } from './HostCharts'
import { DISK_NOT_VISIBLE, HostSection } from './HostSection'

vi.mock('echarts-for-react/esm/core', () => ({
  default: () => null,
}))

async function settle() {
  await act(async () => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      await vi.advanceTimersByTimeAsync(0)
      await Promise.resolve()
    }
  })
}

// The server answers with each host metric as a top-level key, which the
// generated schema does not list.
type HostFixture = Schemas['HostResponse'] & Record<string, unknown>

function renderHost(data: HostFixture) {
  server.use(ok('getInstanceHost', data), ok('queryMetrics', { series: [] }))
  return renderWithProviders(<HostSection instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}`,
  })
}

describe('HostSection', () => {
  it('UI-INST-030 renders measured host values', async () => {
    renderHost({
      instance_id: INSTANCE_ID,
      available: true,
      source: 'cgroup_v2',
      sampled_at: NOW,
      stale: false,
      host_cpu_used_ratio: 0.25,
      host_cpu_count: 4,
      host_mem_total_bytes: 8 * 1024 ** 3,
      host_load1: 0.42,
      host_disk_free_bytes: 5 * 1024 ** 3,
    })
    await settle()

    expect(screen.getByText('25.0%')).toBeInTheDocument()
    expect(screen.getByText('8.0 GiB')).toBeInTheDocument()
    expect(screen.getByText('0.42')).toBeInTheDocument()
    expect(screen.getByText('5.0 GiB')).toBeInTheDocument()
  })

  it('UI-INST-031 keeps an unmeasured host field explicitly unknown', async () => {
    renderHost({
      instance_id: INSTANCE_ID,
      available: true,
      source: 'host',
      host_mem_total_bytes: 1024,
    })
    await settle()

    expect(screen.getByRole('article', { name: 'Memory available' })).toHaveTextContent('—')
    expect(screen.getByRole('article', { name: 'CPU used (host-wide)' })).toHaveTextContent('—')
  })

  it('UI-INST-032 renders an unavailable response as degraded with the exact reason', async () => {
    renderHost({
      instance_id: INSTANCE_ID,
      available: false,
      reason: 'target is not local to any agent',
    })
    await settle()

    expect(screen.getByText('Degraded:')).toBeInTheDocument()
    expect(screen.getByText('target is not local to any agent')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'agent configuration documentation' })).toHaveAttribute(
      'href',
      '/README.md#agent-configuration',
    )
    expect(screen.queryByRole('article', { name: 'Memory total' })).not.toBeInTheDocument()
  })

  it('UI-INST-033 displays the cgroup source for limited memory', async () => {
    renderHost({
      instance_id: INSTANCE_ID,
      available: true,
      source: 'cgroup_v2',
      host_mem_total_bytes: 4 * 1024 ** 3,
    })
    await settle()

    expect(screen.getByText('cgroup_v2')).toBeInTheDocument()
    expect(screen.getByText('4.0 GiB')).toBeInTheDocument()
  })

  it('UI-INST-066 names the container limit in the CPU label only for a cgroup source', async () => {
    renderHost({
      instance_id: INSTANCE_ID,
      available: true,
      source: 'cgroup_v1',
      host_cpu_used_ratio: 0.3,
    })
    await settle()

    expect(
      screen.getByRole('article', {
        name: 'CPU used (of container CPU limit, or host if none set)',
      }),
    ).toHaveTextContent('30.0%')
  })

  it('UI-INST-034 keeps the host section visible when metrics are unavailable', async () => {
    renderHost({
      instance_id: INSTANCE_ID,
      available: false,
      reason: 'target is not local to any agent',
    })
    await settle()

    expect(screen.getByRole('region', { name: 'Host metrics' })).toBeInTheDocument()
    expect(screen.getByText('target is not local to any agent')).toBeInTheDocument()
  })

  it('HOSTCH-009 says the filesystem is not visible to the agent instead of a dash', async () => {
    renderHost({
      instance_id: INSTANCE_ID,
      available: true,
      source: 'host',
      host_mem_total_bytes: 1024,
      host_swap_total_bytes: 0,
    })
    await settle()

    for (const name of ['Filesystem total', 'Filesystem free', 'Filesystem free ratio']) {
      const tile = screen.getByRole('article', { name })
      expect(tile).toHaveTextContent(DISK_NOT_VISIBLE)
      expect(tile).not.toHaveTextContent('—')
    }
    expect(screen.getByRole('article', { name: 'Memory available' })).toHaveTextContent('—')
    expect(screen.getByText(FILESYSTEM_HINT)).toBeInTheDocument()
    expect(screen.getByText(NO_SWAP_TEXT)).toBeInTheDocument()
  })

  it('HOSTCH-009 shows measured filesystem tiles and the host charts below the tiles', async () => {
    renderHost({
      instance_id: INSTANCE_ID,
      available: true,
      source: 'host',
      host_disk_total_bytes: 100 * 1024 ** 3,
      host_disk_free_bytes: 25 * 1024 ** 3,
      host_disk_free_ratio: 0.25,
      host_swap_total_bytes: 2 * 1024 ** 3,
    })
    await settle()

    expect(screen.getByRole('article', { name: 'Filesystem total' })).toHaveTextContent('100.0 GiB')
    expect(screen.getByRole('article', { name: 'Filesystem free ratio' })).toHaveTextContent(
      '25.0%',
    )
    expect(screen.queryByText(DISK_NOT_VISIBLE)).not.toBeInTheDocument()
    expect(screen.queryByText(FILESYSTEM_HINT)).not.toBeInTheDocument()
    expect(screen.queryByText(NO_SWAP_TEXT)).not.toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Host charts' })).toBeInTheDocument()
  })

  it('HOSTCH-011 the section with tiles and charts has no serious accessibility violations', async () => {
    const view = renderHost({
      instance_id: INSTANCE_ID,
      available: true,
      source: 'host',
      host_cpu_used_ratio: 0.2,
      host_swap_total_bytes: 0,
    })
    await settle()
    expect(screen.getByRole('region', { name: 'Host charts' })).toBeInTheDocument()

    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
  })

  it('does not render the host charts when host metrics are unavailable', async () => {
    renderHost({ instance_id: INSTANCE_ID, available: false, reason: 'not local' })
    await settle()

    expect(screen.queryByRole('region', { name: 'Host charts' })).not.toBeInTheDocument()
  })

  it('supports the flat response emitted by the server', async () => {
    renderHost({
      instance_id: INSTANCE_ID,
      available: true,
      source: 'host',
      host_cpu_used_ratio: 0.5,
    })
    await settle()

    expect(screen.getByText('50.0%')).toBeInTheDocument()
  })
})
