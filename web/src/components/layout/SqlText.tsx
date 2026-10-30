import { useMemo, type ReactNode } from 'react'

import { matchRanges } from '@/lib/highlight'
import { tokenizeSql, type SqlTokenKind } from '@/lib/sql'
import { cn } from '@/lib/utils'

import { MARK_CLASS } from './SearchBox'

const TOKEN_CLASS: Record<SqlTokenKind, string> = {
  comment: 'text-muted-foreground italic',
  keyword: 'text-primary font-semibold',
  number: 'text-syntax-number',
  parameter: 'text-syntax-parameter font-medium',
  string: 'text-syntax-string',
  text: '',
}

interface SqlTextProps {
  sql: string
  className?: string
  /** A search term whose occurrences are marked, across token boundaries (D20). */
  highlight?: string | undefined
}

/** `text`, which starts at `offset` of the statement, with the parts inside `ranges` marked. */
function marked(text: string, offset: number, ranges: readonly [number, number][]): ReactNode {
  const parts: ReactNode[] = []
  let cursor = 0
  for (const [start, end] of ranges) {
    const from = Math.max(start - offset, cursor)
    const to = Math.min(end - offset, text.length)
    if (to <= from) continue
    if (from > cursor) parts.push(text.slice(cursor, from))
    parts.push(
      <mark key={from} className={MARK_CLASS}>
        {text.slice(from, to)}
      </mark>,
    )
    cursor = to
  }
  if (parts.length === 0) return text
  if (cursor < text.length) parts.push(text.slice(cursor))
  return parts
}

/** The tokens of `sql`, each with the offset it starts at, so a match can span tokens. */
function placedTokens(sql: string) {
  const placed = []
  let start = 0
  for (const token of tokenizeSql(sql)) {
    placed.push({ ...token, start })
    start += token.text.length
  }
  return placed
}

/** Statement text with syntax colouring, as monospaced inline text. */
export function SqlText({ sql, className, highlight }: SqlTextProps) {
  const tokens = useMemo(() => placedTokens(sql), [sql])
  const ranges = useMemo(() => matchRanges(sql, highlight), [sql, highlight])

  return (
    <code className={cn('font-mono text-[0.8125rem] leading-relaxed', className)}>
      {tokens.map((token, index) =>
        ranges.length === 0 ? (
          token.kind === 'text' ? (
            token.text
          ) : (
            <span key={index} className={TOKEN_CLASS[token.kind]}>
              {token.text}
            </span>
          )
        ) : (
          <span key={index} className={TOKEN_CLASS[token.kind] || undefined}>
            {marked(token.text, token.start, ranges)}
          </span>
        ),
      )}
    </code>
  )
}
