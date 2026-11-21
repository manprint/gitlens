import type { Translate } from '@/i18n/react'
import { translate } from '@/i18n/store'

/**
 * Why an index list is empty. The API lists the indexes the agent reported in
 * the last 15 minutes, so an empty list for the whole server means no index
 * usage statistics arrived in that time; with a database picked it can also
 * mean that this database has no index in the collection. Foreign keys
 * without an index come from the table definitions alone, so they can have
 * results while the statistics are missing.
 */
export function noIndexStatsText({
  database,
  reported,
  foreignKeys,
  t = translate,
}: {
  database: string
  reported: number
  foreignKeys: number
  /** The page's `useT()`, so the text follows the console language. */
  t?: Translate
}): string {
  if (reported > 0 && database !== '') {
    return t('indexes.empty.databaseNoIndex', { database })
  }
  const empty = t('indexes.empty.noStats')
  return foreignKeys > 0 ? `${empty} ${t('indexes.empty.foreignKeys')}` : empty
}

/** The empty text of an index list with no statistics at all, in the active language. */
export function noIndexStats(): string {
  return noIndexStatsText({ database: '', reported: 0, foreignKeys: 0 })
}
