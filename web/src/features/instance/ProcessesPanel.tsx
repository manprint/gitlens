import { useId, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'

import { useHostProcesses, useHostProcessesTop } from '@/api/queries'
import { HelpLink } from '@/components/layout/HelpLink'
import { Section } from '@/components/layout/Section'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState, ErrorState } from '@/components/state'
import { NativeSelect } from '@/components/ui/native-select'
import { useTimeRange } from '@/hooks/useTimeRange'
import type { MessageKey } from '@/i18n/messages/en'
import { useFormat, useT } from '@/i18n/react'
import { formatBytes, formatTimestamp } from '@/lib/format'
import {
  formatByteRate,
  formatCoreShare,
  isProcessGrouping,
  PROCESS_GROUPINGS,
  processKey,
  snapshotIsEmpty,
  type HostProcess,
  type HostProcessGroup,
  type ProcessGroupBy,
  type ProcessGrouping,
} from '@/lib/processes'
import { queryPath } from '@/lib/statements'
import { queryWindowOf } from '@/lib/timerange'

const GROUP_LABEL: Record<ProcessGrouping, MessageKey> = {
  none: 'instance.processes.group.none',
  backend_type: 'instance.processes.group.backend_type',
  datname: 'instance.processes.group.datname',
  usename: 'instance.processes.group.usename',
  application_name: 'instance.processes.group.application_name',
}

function IoRate({ value }: { value: number | null }) {
  const t = useT()
  const fmt = useFormat()
  const shown = formatByteRate(value, fmt.intl)
  if (shown !== null) return <>{shown}</>
  const reason = t('instance.processes.ioUnknown')
  return (
    <span aria-label={reason} title={reason}>
      —
    </span>
  )
}

function ProcessTable({ instanceId, rows }: { instanceId: string; rows: readonly HostProcess[] }) {
  const t = useT()
  const fmt = useFormat()
  const columns = useMemo<SimpleColumn<HostProcess>[]>(
    () => [
      {
        id: 'pid',
        header: t('instance.processes.col.pid'),
        rowHeader: true,
        sortValue: (row) => row.pid,
        cell: (row) => row.pid,
      },
      {
        id: 'type',
        header: t('instance.processes.col.type'),
        sortValue: (row) => row.backend_type,
        cell: (row) => row.backend_type,
      },
      {
        id: 'database',
        header: t('instance.processes.col.database'),
        sortValue: (row) => row.datname,
        cell: (row) => row.datname,
      },
      {
        id: 'user',
        header: t('instance.processes.col.user'),
        sortValue: (row) => row.usename,
        cell: (row) => row.usename,
      },
      {
        id: 'application',
        header: t('instance.processes.col.application'),
        sortValue: (row) => row.application_name,
        cell: (row) => row.application_name,
      },
      {
        id: 'state',
        header: t('instance.processes.col.state'),
        sortValue: (row) => row.state,
        cell: (row) => row.state,
      },
      {
        id: 'cpu',
        header: t('instance.processes.col.cpu'),
        firstDirection: 'desc',
        sortValue: (row) => row.cpu_ratio,
        cell: (row) => formatCoreShare(row.cpu_ratio, fmt.intl),
      },
      {
        id: 'memory',
        header: t('instance.processes.col.memory'),
        firstDirection: 'desc',
        sortValue: (row) => row.rss_bytes,
        cell: (row) => formatBytes(row.rss_bytes, fmt.intl),
      },
      {
        id: 'read',
        header: t('instance.processes.col.read'),
        firstDirection: 'desc',
        sortValue: (row) => row.read_bytes_per_s,
        cell: (row) => <IoRate value={row.read_bytes_per_s} />,
      },
      {
        id: 'write',
        header: t('instance.processes.col.write'),
        firstDirection: 'desc',
        sortValue: (row) => row.write_bytes_per_s,
        cell: (row) => <IoRate value={row.write_bytes_per_s} />,
      },
      {
        id: 'query',
        header: t('instance.processes.col.query'),
        cell: (row) =>
          row.queryid === null ? null : (
            <Link
              className="font-mono underline"
              to={queryPath(instanceId, row.queryid, row.datname || null)}
            >
              {row.queryid}
            </Link>
          ),
      },
    ],
    [fmt.intl, instanceId, t],
  )
  return (
    <SimpleTable
      ariaLabel={t('instance.processes.tableAria')}
      captionId="host.processes"
      columns={columns}
      defaultSort={{ id: 'cpu', desc: true }}
      emptyState={null}
      rowKey={processKey}
      rows={rows}
      tableId="host-processes"
    />
  )
}

function GroupTable({ rows }: { rows: readonly HostProcessGroup[] }) {
  const t = useT()
  const fmt = useFormat()
  const columns = useMemo<SimpleColumn<HostProcessGroup>[]>(
    () => [
      {
        id: 'group',
        header: t('instance.processes.col.group'),
        rowHeader: true,
        sortValue: (row) => row.group,
        cell: (row) => row.group || t('instance.processes.unnamed'),
      },
      {
        id: 'cpu',
        header: t('instance.processes.col.avgCpu'),
        firstDirection: 'desc',
        sortValue: (row) => row.avg_cpu_ratio,
        cell: (row) => formatCoreShare(row.avg_cpu_ratio, fmt.intl),
      },
      {
        id: 'memory',
        header: t('instance.processes.col.maxMemory'),
        firstDirection: 'desc',
        sortValue: (row) => row.max_rss_bytes,
        cell: (row) => formatBytes(row.max_rss_bytes, fmt.intl),
      },
      {
        id: 'read',
        header: t('instance.processes.col.avgRead'),
        firstDirection: 'desc',
        sortValue: (row) => row.avg_read_bytes_per_s,
        cell: (row) => <IoRate value={row.avg_read_bytes_per_s} />,
      },
      {
        id: 'write',
        header: t('instance.processes.col.avgWrite'),
        firstDirection: 'desc',
        sortValue: (row) => row.avg_write_bytes_per_s,
        cell: (row) => <IoRate value={row.avg_write_bytes_per_s} />,
      },
    ],
    [fmt.intl, t],
  )
  return (
    <SimpleTable
      ariaLabel={t('instance.processes.groupsAria')}
      captionId="host.processes"
      columns={columns}
      defaultSort={{ id: 'cpu', desc: true }}
      emptyState={
        <p className="text-muted-foreground text-sm">{t('instance.processes.noSamples')}</p>
      }
      rowKey={(row) => row.group}
      rows={rows}
      tableId="host-process-groups"
    />
  )
}

function GroupedProcesses({
  instanceId,
  groupBy,
}: {
  instanceId: string
  groupBy: ProcessGroupBy
}) {
  const t = useT()
  const { range } = useTimeRange()
  const query = useHostProcessesTop(instanceId, groupBy, queryWindowOf(range))
  if (query.error && !query.data) {
    return (
      <ErrorState
        endpoint="host processes"
        failure={query.error}
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (query.data === undefined) {
    return (
      <p aria-busy="true" role="status" className="text-sm">
        {t('instance.processes.loading')}
      </p>
    )
  }
  return (
    <div className="space-y-2">
      <p className="text-muted-foreground text-sm">{t('instance.processes.rangeNote')}</p>
      <GroupTable rows={query.data.groups} />
    </div>
  )
}

export function ProcessesPanel({ instanceId }: { instanceId: string }) {
  const t = useT()
  const selectId = useId()
  const [grouping, setGrouping] = useState<ProcessGrouping>('none')
  const snapshot = useHostProcesses(instanceId)
  const empty = snapshotIsEmpty(snapshot.data)

  let body
  if (snapshot.error && !snapshot.data) {
    body = (
      <ErrorState
        endpoint="host processes"
        failure={snapshot.error}
        onRetry={() => void snapshot.refetch()}
      />
    )
  } else if (snapshot.data === undefined) {
    body = (
      <p aria-busy="true" role="status" className="text-sm">
        {t('instance.processes.loading')}
      </p>
    )
  } else if (empty) {
    body = (
      <EmptyState
        title={t('instance.processes.emptyTitle')}
        description={t('instance.processes.emptyDescription')}
      />
    )
  } else if (grouping === 'none') {
    body = (
      <div className="space-y-2">
        {snapshot.data.ts ? (
          <p className="text-muted-foreground text-sm">
            {t('instance.processes.sampledAt', { ts: formatTimestamp(snapshot.data.ts) })}
          </p>
        ) : null}
        <ProcessTable instanceId={instanceId} rows={snapshot.data.processes} />
      </div>
    )
  } else {
    body = <GroupedProcesses groupBy={grouping} instanceId={instanceId} />
  }

  return (
    <Section
      actions={
        <div className="flex items-end gap-3">
          {empty ? null : (
            <label className="flex items-center gap-2 text-sm" htmlFor={selectId}>
              <span className="font-medium">{t('instance.processes.groupLabel')}</span>
              <NativeSelect
                className="h-8 w-56"
                id={selectId}
                onChange={(event) => {
                  if (isProcessGrouping(event.target.value)) setGrouping(event.target.value)
                }}
                value={grouping}
              >
                {PROCESS_GROUPINGS.map((g) => (
                  <option key={g} value={g}>
                    {t(GROUP_LABEL[g])}
                  </option>
                ))}
              </NativeSelect>
            </label>
          )}
          <HelpLink
            anchor="instance-overview#host-processes"
            label={t('instance.processes.title')}
          />
        </div>
      }
      captionId="host.processes"
      id="host-processes"
      title={t('instance.processes.title')}
    >
      {body}
    </Section>
  )
}
