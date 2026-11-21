/**
 * The tabs of the Settings page (11.1). `?tab=` names the tab; the default tab
 * is never written to the URL (D12) and an unknown value means the default.
 * The ids and labels follow the tab registry of `settings-inventory.md`, which
 * SETI-001 guards. Data only: the Full HD route inventory imports it.
 */

import type { MessageKey } from '@/i18n/messages/en'

/** `label` names the tab in tests and route lists; the page shows `labelKey`. */
export const SETTINGS_TABS = [
  { id: 'inventory', label: 'Inventory', labelKey: 'settings.tabs.inventory' },
  { id: 'preferences', label: 'Preferences', labelKey: 'settings.tabs.preferences' },
  { id: 'retention', label: 'Data retention', labelKey: 'settings.tabs.retention' },
  { id: 'advisor', label: 'Advisor rules', labelKey: 'settings.tabs.advisor' },
  { id: 'notifications', label: 'Notifications', labelKey: 'settings.tabs.notifications' },
  { id: 'names', label: 'Names', labelKey: 'settings.tabs.names' },
  { id: 'audit', label: 'Audit & server', labelKey: 'settings.tabs.audit' },
] as const satisfies readonly { id: string; label: string; labelKey: MessageKey }[]

export type SettingsTabId = (typeof SETTINGS_TABS)[number]['id']

export const DEFAULT_SETTINGS_TAB: SettingsTabId = 'inventory'

/** The query parameter of the Settings tabs. */
export const SETTINGS_TAB_PARAM = 'tab'

/** The tab a `?tab=` value names; absent or unknown values mean Inventory. */
export function readSettingsTab(value: string | null): SettingsTabId {
  return SETTINGS_TABS.find((tab) => tab.id === value)?.id ?? DEFAULT_SETTINGS_TAB
}

/**
 * The views of the Inventory tab, one table or card group each so a view fits
 * a Full HD screen (D26, I-13). `?view=` names the view; the first one is the
 * default and is never written to the URL (the instance-tab pattern, 15.3).
 */
export const INVENTORY_VIEWS = [
  { id: 'instances', label: 'Instances', labelKey: 'settings.views.instances' },
  { id: 'tiers', label: 'Permission tiers', labelKey: 'settings.views.tiers' },
  { id: 'agents', label: 'Agents', labelKey: 'settings.views.agents' },
  { id: 'databases', label: 'Databases by instance', labelKey: 'settings.views.databases' },
] as const satisfies readonly { id: string; label: string; labelKey: MessageKey }[]

export type InventoryViewId = (typeof INVENTORY_VIEWS)[number]['id']

/**
 * The views of the Audit & server tab (11.1, D26): the command audit and the
 * server information do not fit one Full HD screen together. Command audit is
 * the default and is never written to the URL.
 */
export const AUDIT_VIEWS = [
  { id: 'commands', label: 'Command audit', labelKey: 'settings.views.commands' },
  { id: 'server', label: 'Server info', labelKey: 'settings.views.server' },
] as const satisfies readonly { id: string; label: string; labelKey: MessageKey }[]

export type AuditViewId = (typeof AUDIT_VIEWS)[number]['id']

/**
 * The views of the Advisor rules tab (11.4, D26): the rules table at 20 rows
 * and the observation windows form do not fit one Full HD screen together.
 * Rules is the default and is never written to the URL.
 */
export const ADVISOR_VIEWS = [
  { id: 'rules', label: 'Rules', labelKey: 'settings.views.rules' },
  { id: 'windows', label: 'Observation windows', labelKey: 'settings.views.windows' },
] as const satisfies readonly { id: string; label: string; labelKey: MessageKey }[]

export type AdvisorViewId = (typeof ADVISOR_VIEWS)[number]['id']
