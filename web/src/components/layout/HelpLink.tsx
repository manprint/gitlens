import { BookOpen } from 'lucide-react'
import { Link } from 'react-router-dom'

import { helpHref, type HelpAnchor } from '@/lib/help/href'
import { useT } from '@/i18n/react'
import { cn } from '@/lib/utils'

/**
 * A link to the Inline Help, opened in a new tab so the page being read stays
 * where it is. `anchor` is `topic` or `topic#section`; a test checks that
 * every anchor used in the console exists.
 */
export function HelpLink({
  anchor,
  label,
  className,
  compact = false,
}: {
  anchor: HelpAnchor
  /** What the help explains, for the accessible name and tooltip. */
  label: string
  className?: string
  /** Only the icon, for a tight header line. */
  compact?: boolean
}) {
  const t = useT()
  return (
    <Link
      aria-label={t('shell.help.aria', { label })}
      className={cn(
        'text-primary inline-flex shrink-0 items-center gap-1 text-xs font-medium whitespace-nowrap underline-offset-2 hover:underline',
        className,
      )}
      rel="noopener"
      target="_blank"
      title={t('shell.help.title', { label })}
      to={helpHref(anchor)}
    >
      <BookOpen aria-hidden="true" className="size-3.5" />
      {compact ? null : t('shell.help.link')}
    </Link>
  )
}
