import { useMemo } from 'react'

import { tokenizeSql, type SqlTokenKind } from '@/lib/sql'
import { cn } from '@/lib/utils'

const TOKEN_CLASS: Record<SqlTokenKind, string> = {
  comment: 'text-muted-foreground italic',
  keyword: 'text-primary font-semibold',
  number: 'text-fuchsia-700 dark:text-fuchsia-300',
  parameter: 'font-medium text-cyan-700 dark:text-cyan-300',
  string: 'text-success-foreground dark:text-success',
  text: '',
}

interface SqlTextProps {
  sql: string
  className?: string
}

/** Statement text with syntax colouring, as monospaced inline text. */
export function SqlText({ sql, className }: SqlTextProps) {
  const tokens = useMemo(() => tokenizeSql(sql), [sql])

  return (
    <code className={cn('font-mono text-[0.8125rem] leading-relaxed', className)}>
      {tokens.map((token, index) =>
        token.kind === 'text' ? (
          token.text
        ) : (
          <span key={index} className={TOKEN_CLASS[token.kind]}>
            {token.text}
          </span>
        ),
      )}
    </code>
  )
}
