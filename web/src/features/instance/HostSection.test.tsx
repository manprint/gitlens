import { act, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { INSTANCE_ID, NOW } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { HostSection } from './HostSection'

vi.mock('echarts-for-react', () => ({
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

function renderHost(data: Schemas['HostResponse']) {
  server.use(ok('getInstanceHost', data))
  return renderWithProviders(<HostSection instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}`,
  })
}

describe('HostSection', () => {
  it('UI-INST-030 renders measured host values and identifies the source', async () => {
    renderHost({
      instance_id: INSTANCE_ID,
      available: true,
      source: 'cgroup_v2',
      sampled_at: NOW,
      stale: false,
      metrics: {
        host_cpu_used_ratio: 0.25,
        host_cpu_count: 4,
        host_mem_total_bytes: 8 * 1024 ** 3,
        host_load1: 0.42,
        host_disk_free_bytes: 5 * 1024 ** 3,
      },
    })
    await settle()

    expect(screen.getByText('cgroup_v2')).toBeInTheDocument()
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
      metrics: { host_mem_total_bytes: 1024 },
    })
    await settle()

    expect(screen.getByRole('article', { name: 'Memory available' })).toHaveTextContent('—')
    expect(screen.getByRole('article', { name: 'CPU used' })).toHaveTextContent('—')
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

  it('UI-INST-033 supports the flat response emitted by the server', async () => {
    renderHost({
      instance_id: INSTANCE_ID,
      available: true,
      source: 'host',
      host_cpu_used_ratio: 0.5,
    } as Schemas['HostResponse'] & { host_cpu_used_ratio: number })
    await settle()

    expect(screen.getByText('50.0%')).toBeInTheDocument()
  })
})
