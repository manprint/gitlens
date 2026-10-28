interface DisabledProps {
  feature: string
  configKey: string
}

export function Disabled({ feature, configKey }: DisabledProps) {
  return (
    <div
      role="status"
      className="bg-muted/50 text-muted-foreground rounded-lg border px-3 py-2 text-sm"
    >
      <strong className="text-foreground">{feature} disabled.</strong> The configuration key{' '}
      {configKey} is off.
    </div>
  )
}
