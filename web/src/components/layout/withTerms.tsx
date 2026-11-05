import type { ReactNode } from 'react'

import { glossary, isTerm } from '@/lib/glossary'

import { Term } from './Term'

const WORD = '[A-Za-z0-9_]'
const TERM_PATTERN = new RegExp(
  `(?<!${WORD})(${Object.keys(glossary)
    .sort((a, b) => b.length - a.length)
    .map((term) => term.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
    .join('|')})(?!${WORD})`,
  'g',
)

/**
 * The text with the first occurrence of each glossary term (case-sensitive, whole
 * word) wrapped in a `Term`. The text content is unchanged.
 */
export function withTerms(text: string): ReactNode {
  const seen = new Set<string>()
  const parts: ReactNode[] = []
  let last = 0
  for (const match of text.matchAll(TERM_PATTERN)) {
    const word = match[0]
    if (seen.has(word) || !isTerm(word)) continue
    seen.add(word)
    const at = match.index ?? 0
    parts.push(text.slice(last, at), <Term key={at} bare term={word} />)
    last = at + word.length
  }
  if (parts.length === 0) return text
  parts.push(text.slice(last))
  return parts
}
