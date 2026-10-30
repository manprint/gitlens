/**
 * The tabs of the Settings page (11.1). `?tab=` names the tab; the default tab
 * is never written to the URL (D12) and an unknown value means the default.
 * The ids and labels follow the tab registry of `settings-inventory.md`, which
 * SETI-001 guards. Data only: the Full HD route inventory imports it.
 */

export const SETTINGS_TABS = [
  { id: 'inventory', label: 'Inventory' },
  { id: 'preferences', label: 'Preferences' },
  { id: 'retention', label: 'Data retention' },
  { id: 'advisor', label: 'Advisor rules' },
  { id: 'notifications', label: 'Notifications' },
  { id: 'names', label: 'Names' },
  { id: 'audit', label: 'Audit & server' },
] as const

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
  { id: 'instances', label: 'Instances' },
  { id: 'tiers', label: 'Permission tiers' },
  { id: 'agents', label: 'Agents' },
  { id: 'databases', label: 'Databases by instance' },
] as const

export type InventoryViewId = (typeof INVENTORY_VIEWS)[number]['id']

/**
 * The views of the Audit & server tab (11.1, D26): the command audit and the
 * server information do not fit one Full HD screen together. Command audit is
 * the default and is never written to the URL.
 */
export const AUDIT_VIEWS = [
  { id: 'commands', label: 'Command audit' },
  { id: 'server', label: 'Server info' },
] as const

export type AuditViewId = (typeof AUDIT_VIEWS)[number]['id']

/**
 * The views of the Advisor rules tab (11.4, D26): the rules table at 20 rows
 * and the observation windows form do not fit one Full HD screen together.
 * Rules is the default and is never written to the URL.
 */
export const ADVISOR_VIEWS = [
  { id: 'rules', label: 'Rules' },
  { id: 'windows', label: 'Observation windows' },
] as const

export type AdvisorViewId = (typeof ADVISOR_VIEWS)[number]['id']
