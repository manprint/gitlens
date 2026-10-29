import { sparklinePoints } from '@/lib/sparkline'

interface SparklineProps {
  values: readonly number[]
  /** Spoken in place of the drawing, which carries no text of its own. */
  label: string
  width?: number
  height?: number
  className?: string
}

/**
 * A tiny trend line with no axes. It reads as a shape, not as figures: callers
 * put the numbers beside it. Fewer than two values draw nothing.
 */
export function Sparkline({ values, label, width = 96, height = 24, className }: SparklineProps) {
  if (values.length < 2) return null
  return (
    <svg
      aria-label={label}
      className={className}
      height={height}
      role="img"
      viewBox={`0 0 ${width} ${height}`}
      width={width}
    >
      <polyline
        fill="none"
        points={sparklinePoints(values, width, height)}
        stroke="currentColor"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth={1.5}
      />
    </svg>
  )
}
