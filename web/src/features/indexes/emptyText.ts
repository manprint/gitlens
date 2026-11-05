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
}: {
  database: string
  reported: number
  foreignKeys: number
}): string {
  if (reported > 0 && database !== '') {
    return `The database ${database} has no index in the latest collection.`
  }
  return `No index usage statistics arrived from the agent in the last 15 minutes, so this list is empty.${foreignKeys > 0 ? ' Foreign keys without an index (Foreign keys view) come from table definitions and already have results.' : ''}`
}

export const NO_INDEX_STATS = noIndexStatsText({ database: '', reported: 0, foreignKeys: 0 })
