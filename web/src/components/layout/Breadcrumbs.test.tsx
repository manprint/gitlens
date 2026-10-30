import { act, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { qk } from '@/api/keys'
import { CLUSTER, CLUSTER_ID, INSTANCE_ID, INSTANCE_SUMMARY } from '@/test/fixture-helpers'
import { Breadcrumbs } from './Breadcrumbs'
import { renderWithProviders, type TestRenderResult } from '@/test/render'

/** Puts the cluster list in the cache, as the shell's poll does, and lets the crumbs re-render. */
async function cacheClusters(view: TestRenderResult, clusters: unknown[]) {
  await act(async () => {
    view.queryClient.setQueryData(qk.clusters(), clusters)
    await vi.advanceTimersByTimeAsync(1)
  })
}

// The separators are icons, so each crumb is read from its own list item.
function crumbs(): string[] {
  const navigation = screen.getByRole('navigation', { name: 'Breadcrumb' })
  return within(navigation)
    .getAllByRole('listitem')
    .map((item) => item.textContent ?? '')
}

describe('Breadcrumbs', () => {
  it('WU-17 shows a malformed percent-encoded segment as written instead of throwing', () => {
    // React Router warns that the path is malformed and still matches the
    // route, so the warning is the expected outcome for this path.
    vi.spyOn(console, 'warn').mockImplementation(() => undefined)
    renderWithProviders(<Breadcrumbs />, { route: '/instances/%E0' })

    expect(crumbs()).toEqual(['Instances', '%E0'])
  })

  it('WU-17 still decodes a well-formed segment', () => {
    renderWithProviders(<Breadcrumbs />, { route: '/instances/app%20one' })

    expect(crumbs()).toEqual(['Instances', 'app one'])
  })

  it('CLNAME-UI-011 names a cluster or instance crumb from the cached cluster list, id in the title', async () => {
    const view = renderWithProviders(<Breadcrumbs />, { route: `/clusters/${CLUSTER_ID}` })
    // Before the shell's cluster list arrives the crumb is the shortened id.
    expect(crumbs()).toEqual(['Clusters', '9007199254…'])

    await cacheClusters(view, [
      {
        ...CLUSTER,
        name: 'odoo-prod',
        instances: [{ ...INSTANCE_SUMMARY, display_name: 'odoo-db-1' }],
      },
    ])
    expect(crumbs()).toEqual(['Clusters', 'odoo-prod'])
    expect(screen.getByText('odoo-prod')).toHaveAttribute('title', CLUSTER_ID)
  })

  it('CLNAME-UI-011 an instance crumb shows its display name, else its address', async () => {
    const view = renderWithProviders(<Breadcrumbs />, {
      route: `/instances/${INSTANCE_ID}/queries`,
    })
    await cacheClusters(view, [CLUSTER])
    expect(crumbs()).toEqual(['Instances', 'postgres.example.test:5432', 'Queries'])
    expect(
      within(screen.getByRole('navigation', { name: 'Breadcrumb' })).getByRole('link', {
        name: 'postgres.example.test:5432',
      }),
    ).toHaveAttribute('title', `postgres.example.test:5432 (${INSTANCE_ID})`)
  })

  it('CLNAME-UI-017 a long instance name before the last crumb truncates and keeps its whole name', async () => {
    const long = `odoo-${'x'.repeat(59)}`
    const view = renderWithProviders(<Breadcrumbs />, {
      route: `/instances/${INSTANCE_ID}/queries`,
    })
    await cacheClusters(view, [
      { ...CLUSTER, instances: [{ ...INSTANCE_SUMMARY, display_name: long }] },
    ])
    const link = within(screen.getByRole('navigation', { name: 'Breadcrumb' })).getByRole('link', {
      name: long,
    })
    expect(link).toHaveClass('truncate', 'max-w-[16rem]', 'min-w-0')
    expect(link).toHaveAttribute('title', `${long} (${INSTANCE_ID})`)
    // The plain crumbs keep their width.
    expect(screen.getByRole('link', { name: 'Instances' })).not.toHaveClass('truncate')
  })

  it('CLNAME-UI-011 an id the cluster list does not hold keeps today’s label', async () => {
    const view = renderWithProviders(<Breadcrumbs />, {
      route: '/instances/00000000-0000-4000-8000-00000000abcd',
    })
    await cacheClusters(view, [CLUSTER])
    expect(crumbs()).toEqual(['Instances', '00000000…'])
  })
})
