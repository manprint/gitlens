import { expect } from 'vitest'

import { captionOf, type CaptionId } from '@/lib/captions'

/**
 * A summary card is clear (D31, CARD-INDEX-002): one primary figure (the
 * `.text-2xl` element) and no other bare number. Every digit sequence outside
 * the primary figure is followed by the noun it counts (`9 redundant prefix
 * indexes`), so a second number never stands alone.
 */
export function expectClearCard(article: HTMLElement): void {
  const label = article.getAttribute('aria-label') ?? article.textContent ?? 'tile'
  const primaries = article.querySelectorAll('.text-2xl')
  expect(primaries, `${label}: exactly one primary figure`).toHaveLength(1)
  const clone = article.cloneNode(true) as HTMLElement
  clone.querySelector('.text-2xl')?.remove()
  // The ⓘ tooltip (when open) and the sparkline are not card text.
  clone.querySelectorAll('[role="tooltip"], svg, canvas').forEach((node) => node.remove())
  const outside = (clone.textContent ?? '').replace(/\s+/g, ' ').trim()
  for (const match of outside.matchAll(/\d[\d.,]*(?!\d)/g)) {
    const rest = outside.slice((match.index ?? 0) + match[0].length)
    expect(rest, `${label}: "${match[0]}" must be followed by a noun`).toMatch(/^\s*[A-Za-z%]/)
  }
}

/**
 * Every card root (`[data-card]`) under `container` names its caption and
 * the caption text exists in the catalogue (CAPTION-PAGE-001). Returns the
 * ids found so a test can assert the page's expected ones.
 */
export function expectCaptionedCards(container: HTMLElement): CaptionId[] {
  const cards = [...container.querySelectorAll<HTMLElement>('[data-card]')]
  const ids: CaptionId[] = []
  for (const card of cards) {
    const id = card.getAttribute('data-caption-id')
    const where = card.getAttribute('aria-label') ?? card.querySelector('h2, h3')?.textContent
    expect(id, `card "${where ?? '?'}" has data-caption-id`).toBeTruthy()
    const entry = captionOf(id as CaptionId)
    expect(entry.what.length, `caption ${id} has text`).toBeGreaterThan(0)
    ids.push(id as CaptionId)
  }
  return ids
}
