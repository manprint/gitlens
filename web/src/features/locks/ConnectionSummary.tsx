import { useState } from 'react'

import { Caption } from '@/components/layout/Caption'
import { HelpLink } from '@/components/layout/HelpLink'
import { InfoHint } from '@/components/layout/InfoHint'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { MessageKey } from '@/i18n/messages/en'
import { useT } from '@/i18n/react'
import { formatCount, formatPercent } from '@/lib/format'
import {
  CONNECTION_GROUPS,
  connectionGroup,
  connectionKeyLabel,
  withShare,
  type ConnectionGroupId,
  type ConnectionsSummary,
} from '@/lib/sessions'

const GROUP_KEYS: Record<ConnectionGroupId, { label: MessageKey; noun: MessageKey }> = {
  by_origin: { label: 'locks.connections.tab.byOrigin', noun: 'locks.connections.noun.origin' },
  by_application: {
    label: 'locks.connections.tab.byApplication',
    noun: 'locks.connections.noun.application',
  },
  by_user: { label: 'locks.connections.tab.byUser', noun: 'locks.connections.noun.user' },
  by_client: { label: 'locks.connections.tab.byClient', noun: 'locks.connections.noun.client' },
  by_database: {
    label: 'locks.connections.tab.byDatabase',
    noun: 'locks.connections.noun.database',
  },
  by_state: { label: 'locks.connections.tab.byState', noun: 'locks.connections.noun.state' },
}

function GroupRows({
  connections,
  group,
}: {
  connections: ConnectionsSummary
  group: ConnectionGroupId
}) {
  const t = useT()
  const noun = t(GROUP_KEYS[group].noun)
  const rows = withShare(connectionGroup(connections, group))
  if (rows.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">
        {group === 'by_application' && !connections.by_application_enabled
          ? t('locks.connections.countsOff')
          : t('locks.connections.noneInSample', { noun })}
      </p>
    )
  }
  return (
    <ul aria-label={t('locks.connections.listLabel', { noun })} className="space-y-1 text-sm">
      {rows.map((row) => (
        <li
          key={row.key}
          className="grid grid-cols-[minmax(0,12rem)_5rem_4rem_1fr] items-center gap-3"
        >
          <span className="truncate" title={connectionKeyLabel(row.key)}>
            {connectionKeyLabel(row.key)}
          </span>
          <span className="text-right tabular-nums">{formatCount(row.count)}</span>
          <span className="text-muted-foreground text-right tabular-nums">
            {formatPercent(row.share, 1) ?? '—'}
          </span>
          <span aria-hidden="true" className="bg-muted h-1.5 overflow-hidden rounded-full">
            <span
              className="bg-primary block h-full rounded-full"
              style={{ width: `${Math.round(row.share * 100)}%` }}
            />
          </span>
        </li>
      ))}
    </ul>
  )
}

/**
 * Client connections counted by origin, application, user, client address,
 * database and state, each with its share of the group. The counts are the
 * server's; when the sample was cut they are the activity totals.
 */
export function ConnectionSummary({ connections }: { connections: ConnectionsSummary }) {
  const t = useT()
  const [group, setGroup] = useState<ConnectionGroupId>('by_origin')
  return (
    <div className="space-y-2" data-caption-id="locks.connection-summary" data-card="">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <div className="flex items-center gap-1">
          <h3 className="font-medium">{t('locks.connections.title')}</h3>
          <InfoHint id="locks.connection-summary" label={t('locks.connections.title')} />
          <HelpLink anchor="locks#connection-summary" label={t('locks.connections.title')} />
        </div>
        <Caption className="text-xs" id="locks.connection-summary" marked={false} oneLine />
      </div>
      {connections.truncated ? (
        <p className="text-muted-foreground text-xs">{t('locks.connections.truncated')}</p>
      ) : null}
      <Tabs onValueChange={(value) => setGroup(value as ConnectionGroupId)} value={group}>
        <TabsList aria-label={t('locks.connections.groupsLabel')}>
          {CONNECTION_GROUPS.map((entry) => (
            <TabsTrigger key={entry.id} value={entry.id}>
              {t(GROUP_KEYS[entry.id].label)}
            </TabsTrigger>
          ))}
        </TabsList>
        {CONNECTION_GROUPS.map((entry) => (
          <TabsContent key={entry.id} value={entry.id}>
            <GroupRows connections={connections} group={entry.id} />
          </TabsContent>
        ))}
      </Tabs>
    </div>
  )
}
