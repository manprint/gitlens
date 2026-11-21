import { Link } from 'react-router-dom'

import type { MessageKey } from '@/i18n/messages/en'
import { useT } from '@/i18n/react'
import { channelLabel, channelVariables, NOTIFICATION_CHANNELS } from '@/lib/notificationChannels'

/** The sentence before each channel's variables. */
const VARIABLE_LEAD: Record<(typeof NOTIFICATION_CHANNELS)[number]['kind'], MessageKey> = {
  slack: 'alerts.delivery.slackLead',
  webhook: 'alerts.delivery.webhookLead',
  google_chat: 'alerts.delivery.googleChatLead',
}

function VariableList({ variables }: { variables: readonly string[] }) {
  return (
    <ul className="text-muted-foreground mt-2 space-y-1 text-sm">
      {variables.map((variable) => (
        <li key={variable}>
          <code>{variable}</code>
        </li>
      ))}
    </ul>
  )
}

export function DeliverySection() {
  const t = useT()
  return (
    <div className="space-y-4" data-testid="alert-delivery">
      <p className="text-sm">
        {t('alerts.delivery.intro')}{' '}
        <Link className="underline underline-offset-2" to="/settings?tab=notifications">
          {t('alerts.delivery.settingsLink')}
        </Link>{' '}
        {t('alerts.delivery.introAfter')}
      </p>

      <ul aria-label={t('alerts.delivery.channelsLabel')} className="grid gap-4 sm:grid-cols-2">
        {NOTIFICATION_CHANNELS.map((channel) => (
          <li className="border-border rounded-md border p-3" key={channel.kind}>
            <h3 className="font-medium">{channelLabel(channel)}</h3>
            <p className="text-muted-foreground mt-1 text-sm">{t(VARIABLE_LEAD[channel.kind])}</p>
            <VariableList variables={channelVariables(channel)} />
          </li>
        ))}
      </ul>

      <p className="border-warning/40 bg-warning/10 p-3 text-sm" role="note">
        {t('alerts.delivery.note')}
      </p>
    </div>
  )
}
