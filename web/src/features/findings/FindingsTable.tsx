import { useMemo } from 'react'

import { HighlightedText } from '@/components/layout/SearchBox'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { useT, type Translate } from '@/i18n/react'
import { TIMESTAMP_COLUMN_WIDTH, formatTimestamp } from '@/lib/format/timestamp'
import type { JoinedFinding } from '@/lib/findings'
import { findingSubject, subjectLabel } from '@/lib/findingSubject'
import { instanceNameOrId, type NameLookup } from '@/lib/names'
import { ruleText } from '@/lib/ruleText'

import { FindingCard } from './FindingCard'
import { severityText, stateText } from './findingDisplay'
import { findingPlace, severityBadge } from './findingDisplay'

const severityRank: Record<string, number> = { critical: 0, warning: 1, info: 2 }

function objectText(finding: JoinedFinding, names?: NameLookup): string {
  const subject = findingSubject(finding)
  return subjectLabel(subject, (id) => (names ? instanceNameOrId(names, id) : id))
}

// One line per finding (D26): the card with the statement, what to do, how to
// verify it on the database and the evidence opens under the row. The title is
// never cut; the object shows the statement text or the object name, never an
// id, on at most two lines. A search term is marked in both (D20).
const findingColumns = (
  t: Translate,
  q: string,
  names: NameLookup | undefined,
): readonly SimpleColumn<JoinedFinding>[] => [
  {
    id: 'severity',
    header: t('findings.table.severity'),
    width: '6.5rem',
    sortValue: (finding) => severityRank[finding.severity] ?? 3,
    cell: (finding) => (
      <span className="flex flex-col items-start gap-1">
        <span
          className={`rounded-full border px-2 py-0.5 text-xs font-semibold uppercase ${severityBadge(finding.severity)}`}
        >
          {severityText(t, finding.severity)}
        </span>
        {finding.state !== 'open' && (
          <span className="text-muted-foreground text-xs">{stateText(t, finding.state)}</span>
        )}
      </span>
    ),
  },
  {
    id: 'title',
    header: t('findings.table.finding'),
    rowHeader: true,
    className: 'whitespace-normal',
    sortValue: (finding) => finding.title,
    cell: (finding) => (
      <span className="block break-words" data-finding-row={finding.finding_id}>
        <HighlightedText q={q} text={finding.title} />
        {ruleText(finding.rule_id) && (
          <span className="text-muted-foreground mt-0.5 block text-xs font-normal">
            {ruleText(finding.rule_id)}
          </span>
        )}
      </span>
    ),
  },
  {
    id: 'object',
    header: t('findings.table.object'),
    width: '30%',
    help: 'findings.object',
    className: 'whitespace-normal',
    cell: (finding) => {
      const object = objectText(finding, names)
      const place = findingPlace(finding, names).join(' · ')
      const statement = finding.object_name?.startsWith('queryid:') ?? false
      return (
        <span className="block min-w-0">
          {object !== '' && (
            <span
              className={`line-clamp-2 break-words ${statement ? 'font-mono text-xs' : ''}`}
              title={object}
            >
              <HighlightedText q={q} text={object} />
            </span>
          )}
          {place !== '' && (
            <span className="text-muted-foreground block truncate text-xs" title={place}>
              {place}
            </span>
          )}
        </span>
      )
    },
  },
  {
    id: 'last_seen',
    header: t('findings.table.lastSeen'),
    width: TIMESTAMP_COLUMN_WIDTH,
    className: 'whitespace-nowrap',
    sortValue: (finding) => finding.last_seen,
    firstDirection: 'desc',
    cell: (finding) =>
      finding.last_seen ? (
        <time dateTime={finding.last_seen}>{formatTimestamp(finding.last_seen)}</time>
      ) : (
        '—'
      ),
  },
]

export interface FindingsTableProps {
  /** The filtered findings, in the advisor's rank order. */
  findings: readonly JoinedFinding[]
  /** The search term whose matches are marked. */
  q?: string
  /** Names of the clusters and instances; ids are shown without it. */
  names?: NameLookup | undefined
}

/** The advisor findings, 20 per page, each row expanding to its finding card. */
export function FindingsTable({ findings, q = '', names }: FindingsTableProps) {
  const t = useT()
  const columns = useMemo(() => findingColumns(t, q, names), [t, q, names])
  return (
    <SimpleTable
      ariaLabel={t('findings.table.ariaLabel')}
      captionId="findings.table"
      columns={columns}
      detailLabel={(finding, expanded) =>
        expanded
          ? t('findings.table.hideDetails', { title: finding.title })
          : t('findings.table.showDetails', { title: finding.title })
      }
      emptyState={null}
      renderDetail={(finding) => <FindingCard finding={finding} names={names} />}
      rowKey={(finding) => finding.finding_id}
      rows={findings}
      scope="findings"
      tableId="findings"
    />
  )
}
