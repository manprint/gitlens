interface DegradedProps {
  reason: string
  requires?: string
}

export function Degraded({ reason, requires }: DegradedProps) {
  return (
    <div role="status" className="border-warning/40 bg-warning/10 text-sm">
      <strong>Degraded:</strong> {reason}
      {requires ? ` Required: ${requires}.` : ''}
    </div>
  )
}
