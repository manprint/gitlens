/**
 * The tabs of the advisor findings page (15.3, D26): one table per view. The
 * registry is used by the page and the tests.
 */

import type { MessageKey } from '@/i18n/messages/en'

/** `label` names the view in tests and route lists; the tab shows `labelKey`. */
export const FINDINGS_VIEWS = [
  { id: 'findings', label: 'Findings', labelKey: 'findings.view.findings' },
  { id: 'rules', label: 'Rule catalogue', labelKey: 'findings.ruleCatalogue.title' },
] as const satisfies readonly { id: string; label: string; labelKey: MessageKey }[]

export type FindingsViewId = (typeof FINDINGS_VIEWS)[number]['id']

export const DEFAULT_FINDINGS_VIEW: FindingsViewId = 'findings'

/** The tab a `?view=` value names; absent or unknown values mean Findings. */
export function readFindingsView(value: string | null): FindingsViewId {
  return FINDINGS_VIEWS.find((view) => view.id === value)?.id ?? DEFAULT_FINDINGS_VIEW
}
