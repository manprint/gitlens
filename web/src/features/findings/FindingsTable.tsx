import { useMemo } from 'react'

import { HighlightedText } from '@/components/layout/SearchBox'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { formatTimestamp } from '@/lib/format/timestamp'
import type { JoinedFinding } from '@/lib/findings'
import type { NameLookup } from '@/lib/names'
import { ruleText } from '@/lib/ruleText'

import { FindingCard } from './FindingCard'
import { findingTarget, scopeLabel, severityBadge } from './findingDisplay'

function ruleLine(finding: JoinedFinding): string {
  const text = ruleText(finding.rule_id)
  return text === undefined ? finding.rule_id : `${text} ${finding.rule_id}`
}

function RuleCell({ ruleId }: { ruleId: string }) {
  const text = ruleText(ruleId)
  if (text === undefined) return <>{ruleId}</>
  return (
    <>
      {text} <span className="text-muted-foreground font-mono text-xs">{ruleId}</span>
    </>
  )
}

const severityRank: Record<string, number> = { critical: 0, warning: 1, info: 2 }

function targetText(finding: JoinedFinding, names?: NameLookup): string {
  const target = findingTarget(finding, names)
  return target.length > 0 ? target.join(' · ') : '—'
}

// One line per finding (D26): the card with the detail, remediation, evidence
// and the mute action opens under the row. State and scope are not sortable:
// the filters above the table cover them. A search term is marked in the title
// and the object (D20).
const findingColumns = (
  q: string,
  names: NameLookup | undefined,
): readonly SimpleColumn<JoinedFinding>[] => [
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
    cell: (finding) => (
      <span data-finding-row={finding.finding_id}>
        <HighlightedText q={q} text={finding.title} />
      </span>
    ),
  },
  {
    id: 'object',
    header: 'Object',
    width: '24%',
    help: 'findings.object',
    truncate: (finding) => targetText(finding, names),
    cell: (finding) => <HighlightedText q={q} text={targetText(finding, names)} />,
  },
  {
    id: 'rule',
    header: 'Rule',
    width: '21rem',
    help: 'rules.rule',
    truncate: ruleLine,
    cell: (finding) => <RuleCell ruleId={finding.rule_id} />,
  },
  {
    id: 'scope',
    header: 'Scope',
    className: 'whitespace-nowrap',
    width: '9rem',
    help: 'findings.scope',
    cell: (finding) => scopeLabel(finding.scope),
  },
  {
    id: 'state',
    header: 'State',
    width: '6.5rem',
    help: 'findings.state',
    cell: (finding) => finding.state,
  },
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
  /** The search term whose matches are marked. */
  q?: string
  /** Names of the clusters and instances; ids are shown without it. */
  names?: NameLookup | undefined
}

/** The advisor findings, 20 per page, each row expanding to its finding card. */
export function FindingsTable({ findings, q = '', names }: FindingsTableProps) {
  const columns = useMemo(() => findingColumns(q, names), [q, names])
  return (
    <SimpleTable
      ariaLabel="Advisor findings"
      captionId="findings.table"
      columns={columns}
      detailLabel={(finding, expanded) =>
        `${expanded ? 'Hide' : 'Show'} details of ${finding.title}`
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
