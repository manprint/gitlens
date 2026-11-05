import { Link } from 'react-router-dom'

import { channelVariables, NOTIFICATION_CHANNELS } from '@/lib/notificationChannels'

/** The sentence before each channel's variables. */
const VARIABLE_LEAD: Record<(typeof NOTIFICATION_CHANNELS)[number]['kind'], string> = {
  slack: 'Configure the Slack webhook with one of these server variables:',
  webhook: 'Configure the generic webhook with this server variable:',
  google_chat: 'Configure the Google Chat space webhook with one of these server variables:',
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
  return (
    <div className="space-y-4" data-testid="alert-delivery">
      <p className="text-sm">
        Notification delivery is configured on the server with environment variables. The browser
        never receives a webhook URL.{' '}
        <Link className="underline underline-offset-2" to="/settings?tab=notifications">
          Settings, Notifications
        </Link>{' '}
        shows which channels are configured, stores an address that overrides the environment, and
        sends a test message.
      </p>

      <ul aria-label="Supported notification channels" className="grid gap-4 sm:grid-cols-2">
        {NOTIFICATION_CHANNELS.map((channel) => (
          <li className="border-border rounded-md border p-3" key={channel.kind}>
            <h3 className="font-medium">{channel.label}</h3>
            <p className="text-muted-foreground mt-1 text-sm">{VARIABLE_LEAD[channel.kind]}</p>
            <VariableList variables={channelVariables(channel)} />
          </li>
        ))}
      </ul>

      <p className="border-warning/40 bg-warning/10 p-3 text-sm" role="note">
        If no delivery channel is configured, alerts are persisted but not delivered. Silencing an
        alert suppresses notification only; the alert remains visible and continues to be evaluated.
      </p>
    </div>
  )
}
