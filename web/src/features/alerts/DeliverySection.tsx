const slackVariables = [
  'PGLENS_ALERT_SLACK_WEBHOOK_URL',
  'PGLENS_ALERT_SLACK_WEBHOOK_URL_FILE',
  'PGLENS_SLACK_WEBHOOK_URL',
  'PGLENS_SLACK_WEBHOOK_URL_FILE',
] as const

const webhookVariables = ['PGLENS_WEBHOOK_URL'] as const

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
        does not receive webhook URLs or a channel-configuration status.
      </p>

      <ul aria-label="Supported notification channels" className="grid gap-4 sm:grid-cols-2">
        <li className="border-border rounded-md border p-3">
          <h3 className="font-medium">Slack</h3>
          <p className="text-muted-foreground mt-1 text-sm">
            Configure the Slack webhook with one of these server variables:
          </p>
          <VariableList variables={slackVariables} />
        </li>
        <li className="border-border rounded-md border p-3">
          <h3 className="font-medium">Generic webhook</h3>
          <p className="text-muted-foreground mt-1 text-sm">
            Configure the generic webhook with this server variable:
          </p>
          <VariableList variables={webhookVariables} />
        </li>
      </ul>

      <p className="border-warning/40 bg-warning/10 p-3 text-sm" role="note">
        If no delivery channel is configured, alerts are persisted but not delivered. Silencing an
        alert suppresses notification only; the alert remains visible and continues to be evaluated.
      </p>
    </div>
  )
}
