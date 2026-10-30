import { useId, type ReactNode } from 'react'

import { glossary, type Term as GlossaryTerm } from '@/lib/glossary'

/** A glossary term with a dotted underline; its definition is the element's description (D30). */
export function Term({ term, children }: { term: GlossaryTerm; children?: ReactNode }) {
  const definitionId = `${useId().replaceAll(':', '')}-term`
  const { short } = glossary[term]
  return (
    // Focusable, so keyboard and screen-reader users reach the definition too.
    <span
      aria-describedby={definitionId}
      tabIndex={0}
      className="focus-visible:ring-ring/50 cursor-help rounded-sm underline decoration-dotted underline-offset-2 outline-none focus-visible:ring-[3px]"
      title={short}
    >
      {children ?? term}
      <span hidden id={definitionId}>
        {short}
      </span>
    </span>
  )
}
