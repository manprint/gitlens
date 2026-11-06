/**
 * Full HD table density classes (D26), shared by `DataTable` and `SimpleTable`.
 * Every rule applies from 1600 px only; below it the tables keep their
 * existing responsive layout (I-7).
 */

export const FULLHD_TABLE = 'w-full border-collapse text-sm'
/**
 * Fixed layout, so per-column widths hold and nothing scrolls sideways at 1920:
 * only for a table whose columns carry width or truncation hints (an unhinted
 * fixed table would split its columns equally).
 */
export const FULLHD_FIXED = 'min-[1600px]:table-fixed'
/** One compact line per row: the `--table-row-h` token (28 px). */
export const FULLHD_ROW = 'min-[1600px]:h-(--table-row-h)'
// Inline badges, buttons and chips align to the top of the 20 px line, so their
// baseline does not stretch the line box and the row stays 28 px.
export const FULLHD_CELL =
  'min-[1600px]:px-2 min-[1600px]:py-1 min-[1600px]:align-middle min-[1600px]:[&_*]:align-top'
export const FULLHD_HEADER_CELL = 'min-[1600px]:px-2 min-[1600px]:py-1'
/** A free-text cell: one line with an ellipsis; the full text is in its `title`. */
export const FULLHD_TRUNCATE = 'min-[1600px]:block min-[1600px]:truncate'
/**
 * A page of 50 or 100 rows scrolls inside its card, under the sticky header. Below
 * 1600 px the card stops at 80 % of the screen (unchanged); from 1600 px it stops
 * where the page chrome (shell header, page header, tabs, toolbar, pager,
 * padding) ends, so the page itself never scrolls (D26).
 */
export const FULLHD_BOX_CAP =
  'bg-card relative max-h-[80vh] min-[1600px]:max-h-[calc(100vh-26rem)] overflow-auto rounded-lg border'

/**
 * Narrowest width (rem) at which a column header keeps its longest word, the two
 * sort buttons and the hint inside its own cell: padding, about 0.5 rem per
 * uppercase letter, 2.75 rem of sort buttons, 1.5 rem of hint. A label with several
 * words wraps onto a second line below that; a declared column width never goes
 * below it, so a header never spills over the next column.
 */
export function headerMinRem(label: string, sortable: boolean, hinted: boolean): number {
  const longestWord = Math.max(0, ...label.split(/\s+/).map((word) => word.length))
  return 1 + longestWord * 0.5 + (sortable ? 2.75 : 0) + (hinted ? 1.5 : 0)
}

/** The width class of a header cell: the declared width, at least `--col-min`. */
export const FULLHD_COLUMN_WIDTH = 'min-[1600px]:w-[max(var(--col-w),var(--col-min))]'

/**
 * The content of a header cell: label, sort buttons and hint on one line, the
 * label wrapping when the cell is narrower than all three.
 */
export const HEADER_CONTENT =
  'inline-flex max-w-full items-center gap-1 [&>:not(:first-child)]:shrink-0'
/** The label of a header cell: it may wrap (the cell itself does not). */
export const HEADER_LABEL = 'text-left leading-4 whitespace-normal'
