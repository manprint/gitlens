import type { MessageKey } from '@/i18n/messages/en'
import { translate } from '@/i18n/store'
import type { FindingRecord } from './findings'

/*
 * What a finding is about, in words: the statement with its text, the table,
 * index, setting, subscription or slot. Never a bare id: the advisor names a
 * statement `queryid:<n>` and an instance-wide finding by the instance UUID,
 * and neither tells the operator anything.
 */

type SubjectSource = Pick<
  FindingRecord,
  'object_name' | 'instance_id' | 'cluster_id' | 'query_text' | 'scope'
>

export type FindingSubject =
  | { kind: 'statement'; queryid: string; text: string | null }
  | { kind: 'setting'; name: string }
  | { kind: 'subscription'; name: string }
  | { kind: 'slot'; name: string }
  /** A relation or another database object; `sibling` is the instance an index differs on. */
  | { kind: 'object'; name: string; sibling?: string }
  | { kind: 'none' }

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

// Placeholders some rules write where there is no single object.
const PLACEHOLDERS: Readonly<Record<string, MessageKey>> = {
  'table statistics': 'findings.placeholder.tableStatistics',
  postgis: 'findings.placeholder.postgis',
  sequences: 'findings.placeholder.sequences',
  functions: 'findings.placeholder.functions',
  pg_wal: 'findings.placeholder.pgWal',
  recovery_conflicts: 'findings.placeholder.recoveryConflicts',
  wal_replay: 'findings.placeholder.walReplay',
  vacuum: 'findings.placeholder.autovacuum',
  cluster: 'findings.placeholder.autovacuum',
}

export function findingSubject(finding: SubjectSource): FindingSubject {
  const name = (finding.object_name ?? '').trim()
  const statement = /^queryid:(-?\d+)$/.exec(name)
  if (statement) {
    const text = finding.query_text?.trim()
    return { kind: 'statement', queryid: statement[1]!, text: text === '' ? null : (text ?? null) }
  }
  if (
    name === '' ||
    UUID.test(name) ||
    name === String(finding.instance_id ?? '') ||
    name === String(finding.cluster_id ?? '')
  ) {
    return { kind: 'none' }
  }
  for (const [prefix, kind] of [
    ['setting:', 'setting'],
    ['subscription:', 'subscription'],
    ['slot:', 'slot'],
  ] as const) {
    if (name.startsWith(prefix)) return { kind, name: name.slice(prefix.length) }
  }
  const sibling = /^(.+)@([0-9a-f-]{36})$/i.exec(name)
  if (sibling && UUID.test(sibling[2]!)) {
    return { kind: 'object', name: sibling[1]!, sibling: sibling[2]! }
  }
  const placeholder = Object.hasOwn(PLACEHOLDERS, name) ? PLACEHOLDERS[name] : undefined
  return { kind: 'object', name: placeholder === undefined ? name : translate(placeholder) }
}

/** The statement text on one line, white space collapsed. */
export function oneLine(text: string): string {
  return text.replace(/\s+/g, ' ').trim()
}

/**
 * The subject on one line, for the table and search; `instanceName` turns the
 * sibling instance of an index divergence into its name.
 */
export function subjectLabel(
  subject: FindingSubject,
  instanceName: (id: string) => string = (id) => id,
): string {
  switch (subject.kind) {
    case 'statement':
      return subject.text
        ? oneLine(subject.text)
        : translate('findings.subject.statementGone', { queryid: subject.queryid })
    case 'setting':
      return translate('findings.subject.setting', { name: subject.name })
    case 'subscription':
      return translate('findings.subject.subscription', { name: subject.name })
    case 'slot':
      return translate('findings.subject.slot', { name: subject.name })
    case 'object':
      return subject.sibling
        ? translate('findings.subject.compared', {
            name: subject.name,
            instance: instanceName(subject.sibling),
          })
        : subject.name
    case 'none':
      return ''
  }
}
