interface TruncatedProps {
  shown: number
  budget: number
  scope: string
}

export function Truncated({ shown, budget, scope }: TruncatedProps) {
  return (
    <div role="status" className="text-muted-foreground text-sm">
      Showing {shown} of {budget} {scope} (truncated; budget {budget}).
    </div>
  )
}
