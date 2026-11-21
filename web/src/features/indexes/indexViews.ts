/**
 * The views of the Indexes tab, one table or one summary each (15.3, D26):
 * `?view=` in the URL, the default never written, an unknown value read as
 * the default.
 */
export const INDEX_VIEWS = [
  { id: 'inventory', labelKey: 'indexes.view.inventory' },
  { id: 'summary', labelKey: 'indexes.view.summary' },
  { id: 'findings', labelKey: 'indexes.view.findings' },
  { id: 'foreign-keys', labelKey: 'indexes.view.foreignKeys' },
] as const

export type IndexView = (typeof INDEX_VIEWS)[number]['id']

export const DEFAULT_INDEX_VIEW: IndexView = 'inventory'

/** The URL parameter of the view. */
export const INDEX_VIEW_PARAM = 'view'

export function readIndexView(value: string | null): IndexView {
  return INDEX_VIEWS.find((view) => view.id === value)?.id ?? DEFAULT_INDEX_VIEW
}

/** The tables of the Indexes tab: a database change sends each back to its first page (D23). */
export const INDEX_TABLE_IDS = ['indexes', 'index-findings', 'fk-gaps'] as const
