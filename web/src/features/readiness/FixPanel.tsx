import { Link } from 'react-router-dom'

import { CopyButton } from '@/components/layout/CopyButton'
import { Badge } from '@/components/ui/badge'
import { CHANNEL_LABEL, groupByChannel, type ReadinessFix } from '@/lib/readiness'

/**
 * A fix's way to close a gap: the channel, the ordered steps, the snippet to
 * copy and whether a restart follows. Every string comes from the API and is
 * rendered as text, never as markup (RDY-UI-008).
 */
function Fix({ fix }: { fix: ReadinessFix }) {
  return (
    <li className="space-y-2 rounded-md border p-3" data-testid="readiness-fix">
      <div className="flex flex-wrap items-center gap-2">
        <h5 className="text-sm font-medium">{fix.title}</h5>
        {fix.requires_restart ? <Badge variant="warning">Requires restart</Badge> : null}
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
          <CopyButton subject={`${fix.title} snippet`} text={fix.snippet} />
        </div>
      ) : null}
      {fix.channel === 'console' && fix.route !== undefined && fix.route !== '' ? (
        <Link className="text-primary text-sm underline" to={fix.route}>
          Open in the console
        </Link>
      ) : null}
    </li>
  )
}

/** The fixes of one item, grouped by channel. */
export function FixPanel({ fixes }: { fixes: readonly ReadinessFix[] }) {
  const groups = groupByChannel(fixes)
  if (groups.length === 0) return null
  return (
    <div className="space-y-3" data-testid="readiness-fixes">
      {groups.map((group) => (
        <div key={group.channel} className="space-y-2">
          <h4 className="text-muted-foreground text-xs font-semibold uppercase">
            {CHANNEL_LABEL[group.channel]}
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
