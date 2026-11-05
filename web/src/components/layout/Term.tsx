import { useId, type ReactNode } from 'react'

import { glossary, type Term as GlossaryTerm } from '@/lib/glossary'

/**
 * A glossary term with a dotted underline; its definition is the element's description (D30).
 * `bare` is for text that must stay identical (a tooltip): no hidden definition node, no
 * tab stop (focus would close the tooltip); the definition is the title and `aria-description`.
 */
export function Term({
  term,
  children,
  bare = false,
}: {
  term: GlossaryTerm
  children?: ReactNode
  bare?: boolean
}) {
  const definitionId = `${useId().replaceAll(':', '')}-term`
  const { short } = glossary[term]
  const look = 'cursor-help rounded-sm underline decoration-dotted underline-offset-2 outline-none'
  if (bare) {
    return (
      <span aria-description={short} className={look} title={short}>
        {children ?? term}
      </span>
    )
  }
  return (
    // Focusable, so keyboard and screen-reader users reach the definition too.
    <span
      aria-describedby={definitionId}
      tabIndex={0}
      className={`${look} focus-visible:ring-ring/50 focus-visible:ring-[3px]`}
      title={short}
    >
      {children ?? term}
      <span hidden id={definitionId}>
        {short}
      </span>
    </span>
  )
}
