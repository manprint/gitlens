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
  'min-[1600px]:py-1 min-[1600px]:align-middle min-[1600px]:[&_*]:align-top'
export const FULLHD_HEADER_CELL = 'min-[1600px]:py-1'
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
