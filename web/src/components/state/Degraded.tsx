interface DegradedProps {
  reason: string
  requires?: string
}

export function Degraded({ reason, requires }: DegradedProps) {
  return (
    <div
      role="status"
      className="border-warning/40 bg-warning/10 text-foreground rounded-lg border px-3 py-2 text-sm"
    >
      <strong className="text-warning-foreground">Degraded:</strong> {reason}
      {requires ? ` Required: ${requires}.` : ''}
    </div>
  )
}
