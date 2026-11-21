import { ExternalLink } from 'lucide-react'

import { useSession } from '@/api/queries'
import { useT } from '@/i18n/react'
import { shortTraceId, traceLink } from '@/lib/traceLink'

import { CopyButton } from './CopyButton'

/**
 * A row's trace id: its first 8 characters, linked to the tracing tool when
 * PGLENS_TRACE_URL_TEMPLATE gives a safe link, else plain text with a copy
 * button for the full id.
 */
export function TraceId({ id }: { id: string }) {
  const t = useT()
  const session = useSession()
  const href = traceLink(session.data?.trace_url_template, id)
  const short = shortTraceId(id)
  if (href) {
    return (
      <a
        aria-label={t('shell.trace.open', { id })}
        className="text-primary inline-flex items-center gap-1 font-mono text-xs underline-offset-4 hover:underline"
        data-trace-id={id}
        href={href}
        rel="noopener noreferrer"
        target="_blank"
        title={id}
      >
        {short}
        <ExternalLink aria-hidden="true" className="size-3" />
      </a>
    )
  }
  return (
    <span className="inline-flex items-center gap-1" data-trace-id={id}>
      <span className="font-mono text-xs" title={id}>
        {short}
      </span>
      <CopyButton label={t('shell.trace.copy')} subject={t('shell.trace.subject')} text={id} />
    </span>
  )
}
