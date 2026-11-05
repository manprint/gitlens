import { useState } from 'react'

import { useReadiness } from '@/api/queries'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { CAPTIONS } from '@/lib/captions'
import { severityRank, type ReadinessItem } from '@/lib/readiness'

import { FixPanel } from './FixPanel'

function ItemCard({ defaultOpen, item }: { defaultOpen: boolean; item: ReadinessItem }) {
  const [open, setOpen] = useState(defaultOpen)
  const panelId = `readiness-${item.id}`
  return (
    <li
      className="space-y-2 rounded-lg border p-4"
      data-testid="readiness-item"
      data-severity={item.severity}
    >
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <Badge
              data-caption-id="readiness.severity"
              title={CAPTIONS['readiness.severity'].what}
              variant={item.severity === 'warning' ? 'warning' : 'outline'}
            >
              {item.severity === 'warning' ? 'Warning' : 'Note'}
            </Badge>
            <h3 className="text-sm font-semibold">{item.title}</h3>
            <code className="text-muted-foreground text-xs">{item.id}</code>
          </div>
          <p className="text-sm">{item.summary}</p>
        </div>
        <Button
          aria-controls={panelId}
          aria-expanded={open}
          onClick={() => setOpen((value) => !value)}
          size="sm"
          type="button"
          variant="outline"
        >
          {open ? 'Hide details' : 'Show details'}
        </Button>
      </div>
      {open ? (
        <div className="space-y-3" id={panelId}>
          {item.impact.length > 0 ? (
            <div>
              <h4 className="text-muted-foreground text-xs font-semibold uppercase">
                What you lose
              </h4>
              <ul className="list-disc pl-5 text-sm">
                {item.impact.map((entry) => (
                  <li key={entry}>{entry}</li>
                ))}
              </ul>
            </div>
          ) : null}
          {item.evidence !== '' ? (
            <p className="text-muted-foreground text-xs">
              Evidence: <code>{item.evidence}</code>
            </p>
          ) : null}
          <FixPanel fixes={item.fixes} />
        </div>
      ) : null}
    </li>
  )
}

/**
 * The Readiness tab of an instance (14.2): every gap with what it costs and
 * how to close it, warnings first, notes collapsed (RDY-UI-005). Item text is
 * the API's and is rendered as text only.
 */
export function ReadinessSection({ instanceId }: { instanceId: string }) {
  const query = useReadiness(instanceId)
  if (query.error && !query.data) {
    return (
      <ErrorState
        endpoint="instance readiness"
        failure={query.error}
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (query.isPending || query.data === undefined) {
    return (
      <section aria-busy="true" aria-label="Loading readiness" role="status">
        Loading readiness…
      </section>
    )
  }
  const items = [...query.data.items].sort(
    (a, b) => severityRank(a.severity) - severityRank(b.severity) || a.id.localeCompare(b.id),
  )
  // A single warning opens at once; with more the list stays one line each so the tab
  // fits a Full HD screen (I-13). Notes are always collapsed (RDY-UI-005).
  const openFirst = query.data.counts.warning === 1
  return (
    <Section captionId="instance.readiness" title="Readiness">
      {items.length === 0 ? (
        <EmptyState
          description="Everything pglens can use is set up on this server."
          title="Monitored in full"
        />
      ) : (
        <ul className="space-y-3" data-testid="readiness-items">
          {items.map((item) => (
            <ItemCard
              defaultOpen={openFirst && item.id === items[0]?.id}
              key={item.id}
              item={item}
            />
          ))}
        </ul>
      )}
    </Section>
  )
}
