import { Link } from 'react-router-dom'

import { useSession } from '@/api/queries'
import { HelpLink } from '@/components/layout/HelpLink'
import { Section } from '@/components/layout/Section'
import { Unknown } from '@/components/state'
import type { MessageKey } from '@/i18n/messages/en'
import { useT } from '@/i18n/react'
import { formatTimestamp } from '@/lib/format'

const LIMITS_URL = '/docs/LIMITS.md'

/** The Data retention tab, where each metrics family's retention is read and set (11.3). */
const RETENTION_TAB = '/settings?tab=retention'

/** Product limits: the texts are keys, translated at render. `id` names the row, not its text. */
const limits = [
  {
    id: 'rawRetention',
    labelKey: 'settings.limits.rawRetention.label',
    detailKey: 'settings.limits.rawRetention.detail',
  },
  {
    id: 'postgresVersions',
    labelKey: 'settings.limits.postgresVersions.label',
    detailKey: 'settings.limits.postgresVersions.detail',
  },
  {
    id: 'replication',
    labelKey: 'settings.limits.replication.label',
    detailKey: 'settings.limits.replication.detail',
  },
  {
    id: 'databasesPerInstance',
    labelKey: 'settings.limits.databasesPerInstance.label',
    detailKey: 'settings.limits.databasesPerInstance.detail',
  },
  {
    id: 'tableIndexLists',
    labelKey: 'settings.limits.tableIndexLists.label',
    detailKey: 'settings.limits.tableIndexLists.detail',
  },
  {
    id: 'bloat',
    labelKey: 'settings.limits.bloat.label',
    detailKey: 'settings.limits.bloat.detail',
  },
  {
    id: 'activeSessionHistory',
    labelKey: 'settings.limits.activeSessionHistory.label',
    detailKey: 'settings.limits.activeSessionHistory.detail',
  },
  {
    id: 'connectionPoolers',
    labelKey: 'settings.limits.connectionPoolers.label',
    detailKey: 'settings.limits.connectionPoolers.detail',
  },
] as const satisfies readonly { id: string; labelKey: MessageKey; detailKey: MessageKey }[]

function DocumentationLink() {
  const t = useT()
  return (
    <a className="text-primary underline underline-offset-2" href={LIMITS_URL}>
      {t('settings.server.limitsDocs')}
    </a>
  )
}

function SessionExpiry() {
  const t = useT()
  const sessionQuery = useSession()
  const expiresAt = sessionQuery.data?.expires_at

  if (sessionQuery.isPending) {
    return <span>{t('settings.server.sessionLoading')}</span>
  }
  if (expiresAt) {
    return <time dateTime={expiresAt}>{formatTimestamp(expiresAt)}</time>
  }
  return <Unknown reason={t('settings.server.sessionUnknown')} />
}

export function ServerInfo() {
  const t = useT()
  return (
    // From 1600 px the cards and the limit rows are tighter, so the view fits a Full HD screen (D26).
    <div className="grid gap-8 min-[1600px]:items-start min-[1600px]:gap-4 lg:grid-cols-2">
      <div className="max-[1599px]:*:h-full min-[1600px]:col-start-1 min-[1600px]:row-start-1">
        <Section
          actions={<HelpLink anchor="settings#server-info" label={t('settings.server.title')} />}
          captionId="settings.server-info"
          title={t('settings.server.title')}
        >
          <dl className="grid gap-3 sm:grid-cols-3">
            <div className="rounded-md border p-3">
              <dt className="text-muted-foreground text-sm">
                {t('settings.server.interfaceBuild')}
              </dt>
              <dd className="mt-1 font-medium">{__PGLENS_BUILD__}</dd>
            </div>
            <div className="rounded-md border p-3">
              <dt className="text-muted-foreground text-sm">{t('settings.server.apiContract')}</dt>
              <dd className="mt-1 font-medium">{__PGLENS_API_CONTRACT_VERSION__}</dd>
            </div>
            <div className="rounded-md border p-3">
              <dt className="text-muted-foreground text-sm">
                {t('settings.server.sessionExpiry')}
              </dt>
              <dd className="mt-1 font-medium">
                <SessionExpiry />
              </dd>
            </div>
          </dl>
        </Section>
      </div>

      <div className="max-[1599px]:*:h-full min-[1600px]:col-start-2 min-[1600px]:row-span-3 min-[1600px]:row-start-1">
        <Section
          actions={<DocumentationLink />}
          captionId="settings.product-limits"
          title={t('settings.server.limitsTitle')}
        >
          <ul className="space-y-3 text-sm min-[1600px]:space-y-1.5">
            {limits.map((limit) => (
              <li
                className="flex items-start justify-between gap-4 rounded-md border p-3 min-[1600px]:py-2"
                key={limit.id}
              >
                <span>
                  <strong>{t(limit.labelKey)}.</strong> {t(limit.detailKey)}
                </span>
                {limit.id === 'rawRetention' ? (
                  <Link
                    className="text-primary shrink-0 underline underline-offset-2"
                    to={RETENTION_TAB}
                  >
                    {t('settings.tabs.retention')}
                  </Link>
                ) : (
                  <a
                    className="text-primary shrink-0 underline underline-offset-2"
                    href={LIMITS_URL}
                  >
                    {t('settings.server.details')}
                  </a>
                )}
              </li>
            ))}
          </ul>
        </Section>
      </div>

      <div className="max-[1599px]:*:h-full min-[1600px]:col-start-1 min-[1600px]:row-start-2">
        <Section captionId="settings.authentication" title={t('settings.server.authTitle')}>
          <ul className="list-disc space-y-2 pl-5 text-sm min-[1600px]:space-y-1">
            <li>{t('settings.server.authSharedPassword')}</li>
            <li>{t('settings.server.authNoAccounts')}</li>
            <li>{t('settings.server.authNoRoles')}</li>
            <li>{t('settings.server.authMemory')}</li>
            <li>{t('settings.server.authAgentToken')}</li>
            <li>{t('settings.server.authNoEnrollment')}</li>
          </ul>
        </Section>
      </div>

      <div className="max-[1599px]:*:h-full min-[1600px]:col-start-1 min-[1600px]:row-start-3">
        <Section captionId="settings.references" title={t('settings.server.refsTitle')}>
          <nav aria-label={t('settings.server.refsLabel')} className="flex flex-wrap gap-4 text-sm">
            <a className="text-primary underline underline-offset-2" href="/docs/api.md">
              {t('settings.server.apiReference')}
            </a>
            <Link className="text-primary underline underline-offset-2" to="/alerts/rules">
              {t('shell.nav.alertRules')}
            </Link>
            <Link className="text-primary underline underline-offset-2" to="/alerts/silences">
              {t('shell.nav.silences')}
            </Link>
          </nav>
        </Section>
      </div>
    </div>
  )
}
