export type TooltipSide = 'top' | 'bottom' | 'right' | 'left'

export interface Box {
  top: number
  left: number
  width: number
  height: number
}

export interface Placement {
  side: TooltipSide
  top: number
  left: number
}

/** The gap between the anchor and the tooltip, and the margin kept from the viewport edge. */
const GAP = 8
const MARGIN = 8

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), Math.max(min, max))
}

/**
 * Where a tooltip goes so it stays inside the viewport: above the anchor by
 * default, else below, right or left, the first side with room; the other axis
 * is centred on the anchor and clamped to the viewport.
 */
export function placeTooltip(
  anchor: Box,
  tip: { width: number; height: number },
  viewport: { width: number; height: number },
): Placement {
  const centreLeft = clamp(
    anchor.left + anchor.width / 2 - tip.width / 2,
    MARGIN,
    viewport.width - tip.width - MARGIN,
  )
  const centreTop = clamp(
    anchor.top + anchor.height / 2 - tip.height / 2,
    MARGIN,
    viewport.height - tip.height - MARGIN,
  )
  const above = anchor.top - GAP - tip.height
  if (above >= MARGIN) return { side: 'top', top: above, left: centreLeft }
  const below = anchor.top + anchor.height + GAP
  if (below + tip.height <= viewport.height - MARGIN) {
    return { side: 'bottom', top: below, left: centreLeft }
  }
  const right = anchor.left + anchor.width + GAP
  if (right + tip.width <= viewport.width - MARGIN) {
    return { side: 'right', top: centreTop, left: right }
  }
  const left = anchor.left - GAP - tip.width
  if (left >= MARGIN) return { side: 'left', top: centreTop, left }
  // No side has room: below, kept inside the viewport as far as possible.
  return {
    side: 'bottom',
    top: clamp(below, MARGIN, viewport.height - tip.height - MARGIN),
    left: centreLeft,
  }
}
