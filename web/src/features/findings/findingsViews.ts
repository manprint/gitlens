/**
 * The tabs of the advisor findings page (15.3, D26): one table per view. The
 * registry is used by the page and the tests.
 */

export const FINDINGS_VIEWS = [
  { id: 'findings', label: 'Findings' },
  { id: 'rules', label: 'Rule catalogue' },
] as const

export type FindingsViewId = (typeof FINDINGS_VIEWS)[number]['id']

export const DEFAULT_FINDINGS_VIEW: FindingsViewId = 'findings'

/** The tab a `?view=` value names; absent or unknown values mean Findings. */
export function readFindingsView(value: string | null): FindingsViewId {
  return FINDINGS_VIEWS.find((view) => view.id === value)?.id ?? DEFAULT_FINDINGS_VIEW
}
