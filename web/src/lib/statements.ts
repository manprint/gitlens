import type { Schemas } from '@/api/types'

export interface StatementRow {
  queryid: number | string
  datname?: string | null
  query_text?: string | null
  calls?: number | null
  total_exec_time_ms?: number | null
  rows?: number | null
  mean_exec_time?: number | null
  shared_blks_hit?: number | null
  shared_blks_read?: number | null
  temp_blks_written?: number | null
  cluster_id?: string | null
  pg_version?: number | string | null
  major_version?: number | string | null
  truncated?: Schemas['Truncated']
}

export type StatementOrderBy =
  'total_exec_time' | 'mean_exec_time' | 'calls' | 'rows' | 'shared_blks_read' | 'temp_blks_written'

export interface StatementSummary {
  mean: number | null
  calls: number | null
  total: number | null
  rowsPerCall: number | null
  cacheHitRatio: number | null
  tempBytesPerCall: number | null
}

export type ComparableStatement = Pick<StatementRow, 'cluster_id' | 'pg_version' | 'major_version'>

const POSTGRES_BLOCK_SIZE_BYTES = 8192

function finiteNumber(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function metricValue(row: StatementRow, orderBy: StatementOrderBy): number | null {
  switch (orderBy) {
    case 'total_exec_time':
      return finiteNumber(row.total_exec_time_ms)
    case 'mean_exec_time': {
      const explicitMean = finiteNumber(row.mean_exec_time)
      if (explicitMean !== null) return explicitMean

      const total = finiteNumber(row.total_exec_time_ms)
      const calls = finiteNumber(row.calls)
      return total !== null && calls !== null && calls > 0 ? total / calls : null
    }
    case 'calls':
      return finiteNumber(row.calls)
    case 'rows':
      return finiteNumber(row.rows)
    case 'shared_blks_read':
      return finiteNumber(row.shared_blks_read)
    case 'temp_blks_written':
      return finiteNumber(row.temp_blks_written)
  }
}

function compareQueryIDs(left: number | string, right: number | string): number {
  const leftText = String(left)
  const rightText = String(right)
  if (leftText === rightText) return 0

  const integerPattern = /^-?\d+$/u
  if (integerPattern.test(leftText) && integerPattern.test(rightText)) {
    const leftNegative = leftText.startsWith('-')
    const rightNegative = rightText.startsWith('-')
    if (leftNegative !== rightNegative) return leftNegative ? -1 : 1

    const leftDigits = leftText.replace(/^-?0+/u, '') || '0'
    const rightDigits = rightText.replace(/^-?0+/u, '') || '0'
    if (leftDigits.length !== rightDigits.length) {
      const result = leftDigits.length < rightDigits.length ? -1 : 1
      return leftNegative ? -result : result
    }
    if (leftDigits !== rightDigits) {
      const result = leftDigits < rightDigits ? -1 : 1
      return leftNegative ? -result : result
    }
  }

  return leftText.localeCompare(rightText)
}

/** Sort descending by the selected metric and ascending by queryid on ties. */
export function rankStatements(
  rows: readonly StatementRow[],
  orderBy: StatementOrderBy,
): StatementRow[] {
  return [...rows].sort((left, right) => {
    const leftMetric = metricValue(left, orderBy)
    const rightMetric = metricValue(right, orderBy)

    if (leftMetric === null && rightMetric !== null) return 1
    if (leftMetric !== null && rightMetric === null) return -1
    if (leftMetric !== null && rightMetric !== null && leftMetric !== rightMetric) {
      return rightMetric - leftMetric
    }

    return compareQueryIDs(left.queryid, right.queryid)
  })
}

/** Return one execution-time share per row, preserving null for unavailable values. */
export function shareOfTotal(rows: readonly StatementRow[]): (number | null)[] {
  const values = rows.map((row) => finiteNumber(row.total_exec_time_ms))
  const total = values.reduce<number>((sum, value) => sum + (value ?? 0), 0)
  if (total === 0) return values.map(() => null)

  return values.map((value) => (value === null ? null : value / total))
}

/** Derive display metrics without manufacturing values from missing or zero inputs. */
export function summariseStatement(row: StatementRow): StatementSummary {
  const total = finiteNumber(row.total_exec_time_ms)
  const calls = finiteNumber(row.calls)
  const explicitMean = finiteNumber(row.mean_exec_time)
  const mean =
    explicitMean ?? (total !== null && calls !== null && calls > 0 ? total / calls : null)

  const rowCount = finiteNumber(row.rows)
  const rowsPerCall = rowCount !== null && calls !== null && calls > 0 ? rowCount / calls : null

  const sharedBlocksHit = finiteNumber(row.shared_blks_hit)
  const sharedBlocksRead = finiteNumber(row.shared_blks_read)
  const sharedBlocksTotal =
    sharedBlocksHit !== null && sharedBlocksRead !== null
      ? sharedBlocksHit + sharedBlocksRead
      : null
  const cacheHitRatio =
    sharedBlocksHit !== null && sharedBlocksTotal !== null && sharedBlocksTotal > 0
      ? sharedBlocksHit / sharedBlocksTotal
      : null

  const temporaryBlocks = finiteNumber(row.temp_blks_written)
  const tempBytesPerCall =
    temporaryBlocks !== null && calls !== null && calls > 0
      ? (temporaryBlocks * POSTGRES_BLOCK_SIZE_BYTES) / calls
      : null

  return { cacheHitRatio, calls, mean, rowsPerCall, tempBytesPerCall, total }
}

/** Collapse whitespace for compact list rendering; callers retain the original text separately. */
export function normaliseQueryText(text: string): string {
  return text.trim().replace(/\s+/gu, ' ')
}

function majorVersion(row: ComparableStatement): string | null {
  const value = row.major_version ?? row.pg_version
  if (value === null || value === undefined) return null

  const text = String(value).trim()
  if (text === '') return null

  const numeric = Number(text)
  if (Number.isFinite(numeric) && numeric >= 10000 && Number.isInteger(numeric)) {
    return String(Math.floor(numeric / 10000))
  }

  const match = /^(\d+)/u.exec(text)
  return match?.[1] ?? null
}

/** Query ids can only be compared when cluster and major-version scope match. */
export function comparableWithin(a: ComparableStatement, b: ComparableStatement): boolean {
  const aVersion = majorVersion(a)
  const bVersion = majorVersion(b)
  return (
    a.cluster_id !== null &&
    a.cluster_id !== undefined &&
    b.cluster_id !== null &&
    b.cluster_id !== undefined &&
    a.cluster_id === b.cluster_id &&
    aVersion !== null &&
    aVersion === bVersion
  )
}
