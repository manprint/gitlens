import { useStatements } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { useTimeRange } from '@/hooks/useTimeRange'
import { queryWindowOf } from '@/lib/timerange'

export type Statement = Schemas['Statement']

// pg_stat_statements has no per-query endpoint, so the statement is looked up
// in the instance's statements for the selected window, as wide as the API allows.
const LOOKUP_LIMIT = 1000

export function findStatement(
  statements: readonly Statement[],
  queryid: string,
  datname: string | undefined,
): Statement | undefined {
  return statements.find(
    (statement) =>
      statement.queryid === queryid && (datname === undefined || statement.datname === datname),
  )
}

/** The statement of a query detail page; the overview and the plan panel share the request. */
export function useQueryStatement(
  instanceId: string | undefined,
  queryid: string,
  datname?: string,
) {
  const { range } = useTimeRange()
  const statementsQuery = useStatements(
    {
      instance_id: instanceId ?? '',
      limit: LOOKUP_LIMIT,
      order_by: 'total_exec_time',
      ...(datname === undefined ? {} : { database: datname }),
    },
    queryWindowOf(range),
  )
  const response = statementsQuery.data as Schemas['StatementsResponse'] | undefined
  return { statementsQuery, statement: findStatement(response?.statements ?? [], queryid, datname) }
}
