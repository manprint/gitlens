import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { beforeAll, describe, expect, it, vi } from 'vitest'

import { base as hostBase } from '@/test/fixtures/getInstanceHost'
import { expectNoA11yViolations } from '@/test/a11y'
import { CLUSTER, INSTANCE_ID, INSTANCE_SUMMARY, NOW } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { preloadLazyRoutes, renderWithProviders } from '@/test/render'

import { SettingsPage } from './SettingsPage'
import {
  AUDIT_VIEWS,
  DEFAULT_SETTINGS_TAB,
  INVENTORY_VIEWS,
  readSettingsTab,
  SETTINGS_TABS,
} from './settingsTabs'

async function settle() {
  vi.useRealTimers()
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 25))
  })
  // The page renders its tabs once the page-level queries land; a loaded run can take longer.
  await screen.findByRole('tablist', { name: 'Settings sections' })
}

function renderSettings(route = '/') {
  server.use(
    ok('getInstances', [INSTANCE_SUMMARY]),
    ok('getClusters', [CLUSTER]),
    ok('getAlerts', []),
    ok('getAdvisorRules', []),
    ok('getInstanceCommandAudit', []),
    ok('getSession', { authenticated: true, configured: true, expires_at: NOW }),
    ok('getInstanceDatabases', {
      instance_id: INSTANCE_ID,
      databases: [],
      not_monitored_count: 0,
    }),
    ok('getInstanceHost', hostBase),
  )
  return renderWithProviders(<SettingsPage />, { route })
}

function settingsTab(name: string) {
  return within(screen.getByRole('tablist', { name: 'Settings sections' })).getByRole('tab', {
    name,
  })
}

describe('Settings tabs', () => {
  beforeAll(preloadLazyRoutes)

  it('SETTABS-001 opens on the Inventory tab, with the tabs in the registry order', async () => {
    renderSettings()
    await settle()

    const tabs = within(screen.getByRole('tablist', { name: 'Settings sections' })).getAllByRole(
      'tab',
    )
    expect(tabs.map((tab) => tab.textContent)).toEqual([
      'Inventory',
      'Preferences',
      'Data retention',
      'Advisor rules',
      'Notifications',
      'Names',
      'Audit & server',
    ])
    expect(settingsTab('Inventory')).toHaveAttribute('aria-selected', 'true')
    expect(await screen.findByRole('table', { name: 'Monitored instances' })).toBeInTheDocument()
    expect(readSettingsTab(null)).toBe(DEFAULT_SETTINGS_TAB)
  })

  it('SETTABS-002 ?tab=retention selects Data retention and states what it will configure', async () => {
    renderSettings('/?tab=retention')
    await settle()

    expect(settingsTab('Data retention')).toHaveAttribute('aria-selected', 'true')
    expect(settingsTab('Inventory')).toHaveAttribute('aria-selected', 'false')
    expect(screen.getByRole('heading', { name: 'Data retention' })).toBeInTheDocument()
    expect(screen.getByText(/How many days of history each metrics family keeps/)).toBeVisible()
    // Lazy mount: the inactive Inventory tab renders nothing.
    expect(screen.queryByRole('table', { name: 'Monitored instances' })).not.toBeInTheDocument()
  })

  it('SETTABS-003 an unknown tab falls back to Inventory', async () => {
    const view = renderSettings('/?tab=nonsense&monitored-instances.q=postgres')
    await settle()

    expect(settingsTab('Inventory')).toHaveAttribute('aria-selected', 'true')
    expect(await screen.findByRole('table', { name: 'Monitored instances' })).toBeInTheDocument()
    expect(readSettingsTab('nonsense')).toBe('inventory')
    // The unknown value is removed from the URL; other parameters stay.
    expect(view.router.state.location.search).toBe('?monitored-instances.q=postgres')
    expect(view.router.state.historyAction).toBe('REPLACE')
  })

  it('SETTABS-004 every existing section is reachable in its tab', async () => {
    const view = renderSettings()
    await settle()

    // Inventory: the instance table, and the other inventory views.
    expect(await screen.findByRole('table', { name: 'Monitored instances' })).toBeInTheDocument()
    const inventoryViews = screen.getByRole('tablist', { name: 'Inventory views' })
    fireEvent.mouseDown(within(inventoryViews).getByRole('tab', { name: 'Permission tiers' }))
    expect(screen.getByRole('heading', { name: 'Permission tiers' })).toBeInTheDocument()
    expect(view.router.state.location.search).toBe('?view=tiers')
    fireEvent.mouseDown(within(inventoryViews).getByRole('tab', { name: 'Agents' }))
    expect(screen.getByRole('table', { name: 'Derived agent inventory' })).toBeInTheDocument()
    fireEvent.mouseDown(within(inventoryViews).getByRole('tab', { name: 'Databases by instance' }))
    expect(await screen.findByRole('heading', { name: 'Databases by instance' })).toBeVisible()

    // Names: the names table; the inventory view is dropped with its tab.
    fireEvent.mouseDown(settingsTab('Names'))
    expect(view.router.state.location.search).toBe('?tab=names')
    expect(screen.getByRole('table', { name: 'Cluster and instance names' })).toBeInTheDocument()
    expect(screen.queryByRole('tablist', { name: 'Inventory views' })).not.toBeInTheDocument()

    // Audit & server: the command audit and the server information.
    fireEvent.mouseDown(settingsTab('Audit & server'))
    expect(view.router.state.location.search).toBe('?tab=audit')
    await settle()
    expect(screen.getByRole('heading', { name: 'Command audit' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Server information' })).toBeNull()
    expect(screen.queryByRole('table', { name: 'Cluster and instance names' })).toBeNull()
    const auditViews = screen.getByRole('tablist', { name: 'Audit views' })
    fireEvent.mouseDown(within(auditViews).getByRole('tab', { name: 'Server info' }))
    expect(view.router.state.location.search).toBe('?tab=audit&view=server')
    await settle()
    expect(screen.getByRole('heading', { name: 'Server information' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Product limits' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Command audit' })).toBeNull()
    // The default view is never written.
    fireEvent.mouseDown(within(auditViews).getByRole('tab', { name: 'Command audit' }))
    expect(view.router.state.location.search).toBe('?tab=audit')
    // Switching tab drops the view of the tab left.
    fireEvent.mouseDown(within(auditViews).getByRole('tab', { name: 'Server info' }))
    fireEvent.mouseDown(settingsTab('Names'))
    expect(view.router.state.location.search).toBe('?tab=names')

    // The planned tabs state what they will configure and offer no control.
    for (const [name, text] of [
      ['Preferences', /the theme, the default time range/],
      ['Advisor rules', /Which advisor rules run/],
      ['Notifications', /Whether each notification channel/],
    ] as const) {
      fireEvent.mouseDown(settingsTab(name))
      const panel = screen.getByRole('tabpanel')
      expect(within(panel).getByText(text)).toBeVisible()
      expect(within(panel).queryByRole('button')).toBeNull()
      expect(within(panel).queryByRole('textbox')).toBeNull()
    }
  })

  it('SETTABS-005 has no serious accessibility violations on every tab', async () => {
    const view = renderSettings()
    await settle()
    await waitFor(() =>
      expect(screen.getByRole('table', { name: 'Monitored instances' })).toBeInTheDocument(),
    )

    for (const { label } of SETTINGS_TABS) {
      fireEvent.mouseDown(settingsTab(label))
      await settle()
      await expectNoA11yViolations(view.container)
      // The non-default views of the tabs that have them.
      const views: { list: string; labels: readonly string[] } | null =
        label === 'Inventory'
          ? { list: 'Inventory views', labels: INVENTORY_VIEWS.map((entry) => entry.label) }
          : label === 'Audit & server'
            ? { list: 'Audit views', labels: AUDIT_VIEWS.map((entry) => entry.label) }
            : null
      if (views === null) continue
      for (const name of views.labels.slice(1)) {
        fireEvent.mouseDown(
          within(screen.getByRole('tablist', { name: views.list })).getByRole('tab', { name }),
        )
        await settle()
        await expectNoA11yViolations(view.container)
      }
    }
  }, 60000)

  it('SETTABS-006 never writes the default tab: the first render keeps an empty search, Inventory removes tab', async () => {
    const view = renderSettings()
    await settle()

    expect(view.router.state.location.search).toBe('')
    fireEvent.mouseDown(settingsTab('Names'))
    expect(view.router.state.location.search).toBe('?tab=names')
    fireEvent.mouseDown(settingsTab('Inventory'))
    expect(view.router.state.location.search).toBe('')
    expect(settingsTab('Inventory')).toHaveAttribute('aria-selected', 'true')

    // A tab change replaces the history entry, like the cluster page.
    fireEvent.mouseDown(settingsTab('Data retention'))
    expect(view.router.state.location.search).toBe('?tab=retention')
    expect(view.router.state.historyAction).toBe('REPLACE')
  })

  it('SETTABS-007 a view of another tab falls back to the first view of the open tab', async () => {
    const view = renderSettings('/?tab=audit&view=tiers')
    await settle()

    const auditViews = screen.getByRole('tablist', { name: 'Audit views' })
    expect(within(auditViews).getByRole('tab', { name: 'Command audit' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(screen.getByRole('heading', { name: 'Command audit' })).toBeInTheDocument()
    // The view of another tab is removed from the URL.
    expect(view.router.state.location.search).toBe('?tab=audit')
    view.unmount()

    const second = renderSettings('/?view=server')
    await settle()
    expect(second.router.state.location.search).toBe('')
    const inventoryViews = await screen.findByRole('tablist', { name: 'Inventory views' })
    expect(within(inventoryViews).getByRole('tab', { name: 'Instances' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(await screen.findByRole('table', { name: 'Monitored instances' })).toBeInTheDocument()
  })

  it('SETTABS-008 the Databases view says the Instances search filters it and clears it', async () => {
    const view = renderSettings('/?view=databases&monitored-instances.q=nothing-like-this')
    await settle()

    const notice = await screen.findByText(/Filtered by the Instances search “nothing-like-this”/)
    expect(screen.getAllByRole('status').some((status) => status.contains(notice))).toBe(true)
    expect(screen.getByText('No instances match this filter')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Clear filter' }))
    expect(view.router.state.location.search).toBe('?view=databases')
    expect(view.router.state.historyAction).toBe('REPLACE')
    expect(screen.queryByText(/Filtered by the Instances search/)).not.toBeInTheDocument()
    expect(screen.queryByText('No instances match this filter')).not.toBeInTheDocument()
  })

  it('SETTABS-009 ArrowRight on the selected tab selects the next tab (automatic activation)', async () => {
    const view = renderSettings()
    await settle()

    const inventory = settingsTab('Inventory')
    act(() => inventory.focus())
    fireEvent.keyDown(inventory, { key: 'ArrowRight' })

    // Radix moves the focus on the next tick; the focus selects the tab.
    await waitFor(() => expect(settingsTab('Preferences')).toHaveFocus())
    await waitFor(() => expect(settingsTab('Preferences')).toHaveAttribute('aria-selected', 'true'))
    expect(view.router.state.location.search).toBe('?tab=preferences')
  })
})
