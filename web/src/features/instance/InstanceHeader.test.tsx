import { act, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { qk } from '@/api/keys'

import type { Schemas } from '@/api/types'
import { CLUSTER, CLUSTER_ID, INSTANCE_ID, NOW } from '@/test/fixture-helpers'
import { makeGetInstance } from '@/test/fixtures/getInstance'
import { renderWithProviders } from '@/test/render'

import { InstanceHeader } from './InstanceHeader'

function renderHeader(displayName?: string | null) {
  const instance = {
    ...makeGetInstance(),
    ...(displayName === undefined ? {} : { display_name: displayName }),
  } as unknown as Schemas['Instance']
  return renderWithProviders(
    <InstanceHeader
      dataUpdatedAt={Date.parse(NOW)}
      instance={instance}
      replayLag={{ state: 'loading' }}
    />,
    { route: `/instances/${INSTANCE_ID}` },
  )
}

describe('InstanceHeader names', () => {
  it('CLNAME-UI-008 shows the display name as title and the address as secondary text', () => {
    renderHeader('odoo-db-1')

    const header = screen.getByTestId('instance-header')
    const title = within(header).getByRole('heading', { level: 1, name: 'odoo-db-1' })
    // From 1600 px the name is one truncated line, so the header keeps its height (D26).
    expect(title).toHaveClass('min-[1600px]:truncate', 'min-[1600px]:max-w-md')
    expect(title).toHaveAttribute('title', 'odoo-db-1')
    const address = within(header).getByTestId('instance-address')
    expect(address).toHaveTextContent('postgres.example.test:5432')
    expect(address).toHaveAttribute('title', 'postgres.example.test:5432')
    // The address comes after the title, in the subtitle line.
    expect(title.compareDocumentPosition(address) & Node.DOCUMENT_POSITION_FOLLOWING).not.toBe(0)
  })

  it('CLNAME-UI-008 without a name the address is the title and is not repeated', () => {
    renderHeader(null)
    expect(
      screen.getByRole('heading', { level: 1, name: 'postgres.example.test:5432' }),
    ).toBeInTheDocument()
    expect(screen.queryByTestId('instance-address')).not.toBeInTheDocument()
  })

  it('CLNAME-UI-008 an absent display_name reads as no name', () => {
    renderHeader()
    expect(
      screen.getByRole('heading', { level: 1, name: 'postgres.example.test:5432' }),
    ).toBeInTheDocument()
    expect(screen.queryByTestId('instance-address')).not.toBeInTheDocument()
  })

  it('CLNAME-UI-008 the owning cluster shows its name from the cached list, the id in the title', async () => {
    const view = renderHeader()
    const owning = () => screen.getByRole('link', { name: /odoo-prod|9007199254740993/ })
    expect(owning()).toHaveTextContent(CLUSTER_ID)

    await act(async () => {
      view.queryClient.setQueryData(qk.clusters(), [{ ...CLUSTER, name: 'odoo-prod' }])
      await vi.advanceTimersByTimeAsync(1)
    })
    expect(owning()).toHaveTextContent('odoo-prod')
    expect(owning()).toHaveAttribute('title', CLUSTER_ID)
    expect(owning()).toHaveAttribute('href', `/clusters/${CLUSTER_ID}`)
  })
})
