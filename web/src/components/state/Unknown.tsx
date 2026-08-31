interface UnknownProps {
  reason?: string
}

export function Unknown({ reason = 'Value was not measured.' }: UnknownProps) {
  return (
    <span aria-label="not measured" title={reason}>
      —
    </span>
  )
}
