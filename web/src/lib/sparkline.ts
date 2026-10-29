const PAD = 2

/**
 * The `points` of the polyline: x spread evenly, y scaled so the highest value
 * is at the top. A flat series sits on the middle line instead of dividing by zero.
 */
export function sparklinePoints(values: readonly number[], width: number, height: number): string {
  const min = Math.min(...values)
  const span = Math.max(...values) - min
  const x = (index: number) => PAD + (index * (width - 2 * PAD)) / Math.max(values.length - 1, 1)
  const y = (value: number) =>
    span === 0 ? height / 2 : PAD + (1 - (value - min) / span) * (height - 2 * PAD)
  return values.map((value, index) => `${x(index).toFixed(1)},${y(value).toFixed(1)}`).join(' ')
}
