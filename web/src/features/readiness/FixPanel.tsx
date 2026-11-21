import { Link } from 'react-router-dom'

import { CopyButton } from '@/components/layout/CopyButton'
import { Badge } from '@/components/ui/badge'
import { useT } from '@/i18n/react'
import { captionOf } from '@/lib/captions'
import { CHANNEL_LABEL_KEY, groupByChannel, type ReadinessFix } from '@/lib/readiness'

/**
 * A fix's way to close a gap: the channel, the ordered steps, the snippet to
 * copy and whether a restart follows. Every string comes from the API and is
 * rendered as text, never as markup (RDY-UI-008).
 */
function Fix({ fix }: { fix: ReadinessFix }) {
  const t = useT()
  return (
    <li className="space-y-2 rounded-md border p-3" data-testid="readiness-fix">
      <div className="flex flex-wrap items-center gap-2">
        <h5 className="text-sm font-medium">{fix.title}</h5>
        {fix.requires_restart ? (
          <Badge
            data-caption-id="readiness.requires-restart"
            title={captionOf('readiness.requires-restart').what}
            variant="warning"
          >
            {t('readiness.fix.requiresRestart')}
          </Badge>
        ) : null}
      </div>
      {fix.steps.length > 0 ? (
        <ol className="list-decimal space-y-1 pl-5 text-sm">
          {fix.steps.map((step, index) => (
            <li key={index}>{step}</li>
          ))}
        </ol>
      ) : null}
      {fix.snippet !== undefined && fix.snippet !== '' ? (
        <div className="space-y-1">
          <pre className="bg-muted overflow-x-auto rounded-md p-2 font-mono text-xs whitespace-pre-wrap">
            {fix.snippet}
          </pre>
          <CopyButton
            subject={t('readiness.fix.snippet', { title: fix.title })}
            text={fix.snippet}
          />
        </div>
      ) : null}
      {fix.channel === 'console' && fix.route !== undefined && fix.route !== '' ? (
        <Link className="text-primary text-sm underline" to={fix.route}>
          {t('readiness.fix.openConsole')}
        </Link>
      ) : null}
    </li>
  )
}

/** The fixes of one item, grouped by channel. */
export function FixPanel({ fixes }: { fixes: readonly ReadinessFix[] }) {
  const t = useT()
  const groups = groupByChannel(fixes)
  if (groups.length === 0) return null
  return (
    <div className="space-y-3" data-testid="readiness-fixes">
      {groups.map((group) => (
        <div key={group.channel} className="space-y-2">
          <h4 className="text-muted-foreground text-xs font-semibold uppercase">
            {t(CHANNEL_LABEL_KEY[group.channel])}
          </h4>
          <ul className="space-y-2">
            {group.fixes.map((fix, index) => (
              <Fix key={`${fix.title}-${index}`} fix={fix} />
            ))}
          </ul>
        </div>
      ))}
    </div>
  )
}
