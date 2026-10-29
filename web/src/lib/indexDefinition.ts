/** One key or included column of an index, as `pg_get_indexdef` writes it. */
export interface IndexColumn {
  /** The column name, or the whole expression for an expression index. */
  text: string
  /** True when the element is an expression, not a plain column. */
  expression: boolean
  /** Everything after the column: operator class, COLLATE, ASC/DESC, NULLS FIRST/LAST. */
  modifiers: string
}

export interface ParsedIndexDefinition {
  unique: boolean
  /** Access method: btree, hash, gin, gist, brin, spgist or an extension's. */
  method: string | null
  /** The indexed key columns, in order. */
  columns: IndexColumn[]
  /** Columns carried only to allow index-only scans (INCLUDE). */
  include: IndexColumn[]
  /** The WHERE clause of a partial index, without the enclosing parentheses. */
  predicate: string | null
  /** Storage parameters, such as `fillfactor=70`. */
  options: string | null
  nullsNotDistinct: boolean
}

const EMPTY: ParsedIndexDefinition = {
  unique: false,
  method: null,
  columns: [],
  include: [],
  predicate: null,
  options: null,
  nullsNotDistinct: false,
}

/**
 * Index of the first top-level occurrence of `needle` (case-insensitive),
 * skipping quoted identifiers, string literals and parenthesised groups.
 */
function indexOfTopLevel(text: string, needle: string, from = 0): number {
  const lower = needle.toLowerCase()
  const haystack = text.toLowerCase()
  let depth = 0
  let quote = ''
  for (let i = from; i < text.length; i += 1) {
    const ch = text[i]
    if (quote !== '') {
      if (ch === quote) quote = ''
      continue
    }
    if (ch === '"' || ch === "'") quote = ch
    else if (ch === '(') depth += 1
    else if (ch === ')') depth -= 1
    else if (depth === 0 && haystack.startsWith(lower, i)) return i
  }
  return -1
}

/** The text between the parenthesis at `open` and its match, and the index after it. */
function balancedGroup(text: string, open: number): { inner: string; end: number } | null {
  if (text[open] !== '(') return null
  let depth = 0
  let quote = ''
  for (let i = open; i < text.length; i += 1) {
    const ch = text[i]
    if (quote !== '') {
      if (ch === quote) quote = ''
      continue
    }
    if (ch === '"' || ch === "'") quote = ch
    else if (ch === '(') depth += 1
    else if (ch === ')') {
      depth -= 1
      if (depth === 0) return { inner: text.slice(open + 1, i), end: i + 1 }
    }
  }
  return null
}

function splitTopLevel(text: string): string[] {
  const parts: string[] = []
  let depth = 0
  let quote = ''
  let start = 0
  for (let i = 0; i < text.length; i += 1) {
    const ch = text[i]
    if (quote !== '') {
      if (ch === quote) quote = ''
      continue
    }
    if (ch === '"' || ch === "'") quote = ch
    else if (ch === '(') depth += 1
    else if (ch === ')') depth -= 1
    else if (ch === ',' && depth === 0) {
      parts.push(text.slice(start, i))
      start = i + 1
    }
  }
  parts.push(text.slice(start))
  return parts.map((part) => part.trim()).filter((part) => part !== '')
}

const IDENTIFIER = /^("(?:[^"]|"")+"|[A-Za-z_][A-Za-z0-9_$]*)(?![\w$(])\s*(.*)$/s

function toColumn(element: string): IndexColumn {
  const match = IDENTIFIER.exec(element)
  if (match?.[1] !== undefined) {
    return { text: match[1], expression: false, modifiers: (match[2] ?? '').trim() }
  }
  // An expression: a parenthesised group or a function call. The modifiers
  // follow its closing parenthesis.
  const open = element.indexOf('(')
  const group = open >= 0 ? balancedGroup(element, open) : null
  if (group === null) return { text: element, expression: true, modifiers: '' }
  return {
    text: element.slice(0, group.end),
    expression: true,
    modifiers: element.slice(group.end).trim(),
  }
}

/**
 * Reads the columns, access method and predicate out of the text of
 * `pg_get_indexdef`. An unreadable definition parses to no columns rather than
 * throwing: the page then shows the raw text.
 */
export function parseIndexDefinition(definition: string | null | undefined): ParsedIndexDefinition {
  if (!definition) return EMPTY
  const using = indexOfTopLevel(definition, ' using ')
  if (using < 0) return { ...EMPTY, unique: /^\s*create\s+unique\s+index/i.test(definition) }
  const afterUsing = using + ' using '.length
  const open = definition.indexOf('(', afterUsing)
  if (open < 0) return EMPTY
  const method = definition.slice(afterUsing, open).trim() || null
  const keys = balancedGroup(definition, open)
  if (keys === null) return { ...EMPTY, method }

  const parsed: ParsedIndexDefinition = {
    unique: /^\s*create\s+unique\s+index/i.test(definition),
    method,
    columns: splitTopLevel(keys.inner).map(toColumn),
    include: [],
    predicate: null,
    options: null,
    nullsNotDistinct: false,
  }

  const rest = definition.slice(keys.end)
  const includeAt = indexOfTopLevel(rest, ' include ')
  if (includeAt >= 0) {
    const group = balancedGroup(rest, rest.indexOf('(', includeAt))
    if (group !== null) parsed.include = splitTopLevel(group.inner).map(toColumn)
  }
  parsed.nullsNotDistinct = indexOfTopLevel(rest, ' nulls not distinct') >= 0
  const withAt = indexOfTopLevel(rest, ' with ')
  if (withAt >= 0) {
    const group = balancedGroup(rest, rest.indexOf('(', withAt))
    if (group !== null) parsed.options = group.inner.trim()
  }
  const whereAt = indexOfTopLevel(rest, ' where ')
  if (whereAt >= 0) {
    let predicate = rest.slice(whereAt + ' where '.length).trim()
    const group = predicate.startsWith('(') ? balancedGroup(predicate, 0) : null
    if (group !== null && group.end === predicate.length) predicate = group.inner.trim()
    parsed.predicate = predicate
  }
  return parsed
}

/** A short column list for a table cell: `a, b, lower(name)`, with INCLUDE after a plus sign. */
export function summariseColumns(parsed: ParsedIndexDefinition): string {
  const keys = parsed.columns.map((column) => column.text).join(', ')
  if (parsed.include.length === 0) return keys
  return `${keys} + ${parsed.include.map((column) => column.text).join(', ')}`
}
