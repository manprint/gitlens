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
