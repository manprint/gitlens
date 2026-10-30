import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { formatTimestamp } from '@/lib/format/timestamp'
import type { JoinedFinding } from '@/lib/findings'

import { FindingCard } from './FindingCard'
import { findingTarget, severityBadge } from './findingDisplay'

const severityRank: Record<string, number> = { critical: 0, warning: 1, info: 2 }

function targetText(finding: JoinedFinding): string {
  const target = findingTarget(finding)
  return target.length > 0 ? target.join(' · ') : '—'
}

// One line per finding (D26): the card with the detail, remediation, evidence
// and the mute action opens under the row. State and scope are not sortable:
// the filters above the table cover them.
const COLUMNS: readonly SimpleColumn<JoinedFinding>[] = [
  {
    id: 'severity',
    header: 'Severity',
    width: '7rem',
    sortValue: (finding) => severityRank[finding.severity] ?? 3,
    cell: (finding) => (
      <span
        className={`rounded-full border px-2 py-0.5 text-xs font-semibold uppercase ${severityBadge(finding.severity)}`}
      >
        {finding.severity}
      </span>
    ),
  },
  {
    id: 'title',
    header: 'Finding',
    rowHeader: true,
    sortValue: (finding) => finding.title,
    truncate: (finding) => finding.title,
    cell: (finding) => <span data-finding-row={finding.finding_id}>{finding.title}</span>,
  },
  {
    id: 'object',
    header: 'Object',
    width: '24%',
    truncate: targetText,
    cell: targetText,
  },
  {
    id: 'rule',
    header: 'Rule',
    width: '12rem',
    truncate: (finding) => finding.rule_id,
    cell: (finding) => finding.rule_id,
  },
  { id: 'scope', header: 'Scope', width: '6.5rem', cell: (finding) => finding.scope },
  { id: 'state', header: 'State', width: '6.5rem', cell: (finding) => finding.state },
  {
    id: 'last_seen',
    header: 'Last seen',
    width: '12.5rem',
    className: 'whitespace-nowrap',
    sortValue: (finding) => finding.last_seen,
    firstDirection: 'desc',
    cell: (finding) =>
      finding.last_seen ? (
        <time dateTime={finding.last_seen}>{formatTimestamp(finding.last_seen, 'UTC')}</time>
      ) : (
        '—'
      ),
  },
]

export interface FindingsTableProps {
  /** The filtered findings, in the advisor's rank order. */
  findings: readonly JoinedFinding[]
}

/** The advisor findings, 20 per page, each row expanding to its finding card. */
export function FindingsTable({ findings }: FindingsTableProps) {
  return (
    <SimpleTable
      ariaLabel="Advisor findings"
      columns={COLUMNS}
      detailLabel={(finding, expanded) =>
        `${expanded ? 'Hide' : 'Show'} details of ${finding.title}`
      }
      emptyState={null}
      renderDetail={(finding) => <FindingCard finding={finding} />}
      rowKey={(finding) => finding.finding_id}
      rows={findings}
      scope="findings"
      tableId="findings"
    />
  )
}
