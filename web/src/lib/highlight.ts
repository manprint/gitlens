/**
 * The full search of the console (D20): marks where a search term occurs in a
 * text, and the words the result lines use. Pure: the pages render the
 * segments as text nodes and `<mark>` elements, so nothing is escaped here.
 */

/** A run of the text; `match` marks a run equal to the term, ignoring case. */
export interface HighlightSegment {
  text: string
  match: boolean
}

/** The shortest term the server searches for (D20); shorter terms mark nothing. */
export const HIGHLIGHT_MIN_LENGTH = 2

// The regular-expression syntax characters; with the `u` flag only these may be escaped.
const SPECIAL = /[.*+?^${}()|[\]\\/]/g

/**
 * `text` cut into runs, every case-insensitive, non-overlapping occurrence of
 * the trimmed `q` marked, in reading order and with the original case. A term
 * shorter than 2 characters, or one that does not occur, gives one unmarked run.
 */
export function highlightMatches(text: string, q: string | null | undefined): HighlightSegment[] {
  const term = (q ?? '').trim()
  if ([...term].length < HIGHLIGHT_MIN_LENGTH || text === '') return [{ text, match: false }]

  const pattern = new RegExp(term.replace(SPECIAL, '\\$&'), 'giu')
  const segments: HighlightSegment[] = []
  let last = 0
  for (const found of text.matchAll(pattern)) {
    const start = found.index
    if (start > last) segments.push({ text: text.slice(last, start), match: false })
    segments.push({ text: found[0], match: true })
    last = start + found[0].length
  }
  if (last < text.length) segments.push({ text: text.slice(last), match: false })
  return segments.length === 0 ? [{ text, match: false }] : segments
}

/** The `[start, end)` offsets of the marked runs of `highlightMatches(text, q)`. */
export function matchRanges(text: string, q: string | null | undefined): [number, number][] {
  const ranges: [number, number][] = []
  let offset = 0
  for (const segment of highlightMatches(text, q)) {
    if (segment.match) ranges.push([offset, offset + segment.text.length])
    offset += segment.text.length
  }
  return ranges
}

/** The trimmed URL `q`, or '' when it is too short to search (the hooks then ask nothing). */
export function searchTermOf(q: string | null | undefined): string {
  const term = (q ?? '').trim()
  return [...term].length >= HIGHLIGHT_MIN_LENGTH ? term : ''
}

/**
 * The footer sentence of a server search (D20, I-9): how many statements
 * match, or that more match than the page loaded.
 */
export function matchCountNote(count: number, hasMore: boolean): string {
  return hasMore
    ? `More than ${count} matches — narrow the search.`
    : `${count} ${count === 1 ? 'match' : 'matches'}.`
}
