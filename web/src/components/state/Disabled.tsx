interface DisabledProps {
  feature: string
  configKey: string
}

export function Disabled({ feature, configKey }: DisabledProps) {
  return (
    <div role="status" className="border-muted bg-muted/30 text-sm">
      <strong>{feature} disabled.</strong> The configuration key {configKey} is off.
    </div>
  )
}
