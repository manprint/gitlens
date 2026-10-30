/**
 * Tighter cells and sort buttons for a wide table from 1600 px (D26): 8 px cell
 * padding and 20 px sort buttons instead of the shared table's 12 px and 24 px,
 * so a table of ten or eleven sortable columns fits a Full HD line. Put it on
 * the table's scroll container (`SimpleTable` `className`) or on a wrapper.
 */
export const DENSE_TABLE =
  'min-[1600px]:[&_td]:px-2 min-[1600px]:[&_th]:px-2 min-[1600px]:[&_thead_.size-6]:size-5'
