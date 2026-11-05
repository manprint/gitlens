import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { CAPTIONS } from '@/lib/captions'
import { glossary } from '@/lib/glossary'

import { Caption } from './Caption'
import { InfoHint } from './InfoHint'
import { withTerms } from './withTerms'

function textOf(text: string): { html: string; text: string | null } {
  const { container } = render(<p>{withTerms(text)}</p>)
  return { html: container.innerHTML, text: container.textContent }
}

describe('withTerms (D30)', () => {
  it('CAPCMP-006 keeps the text and wraps the first occurrence of each term', () => {
    const text = 'WAL is read by every standby; the WAL grows until a checkpoint, not checkpoints.'
    const { text: content } = textOf(text)
    expect(content).toBe(text)
    const wal = screen.getAllByTitle(glossary.WAL.short)
    expect(wal).toHaveLength(1)
    expect(wal[0]).toHaveTextContent('WAL')
    expect(screen.getAllByTitle(glossary.standby.short)).toHaveLength(1)
    expect(screen.getAllByTitle(glossary.checkpoint.short)).toHaveLength(1)
  })

  it('CAPCMP-006 matches whole words, case-sensitively, never inside a word', () => {
    const text = 'Autovacuum, wal, WALs, pg_stat_statements_info and TPSx are not terms.'
    const { html, text: content } = textOf(text)
    expect(content).toBe(text)
    expect(html).not.toContain('title=')
  })

  it('CAPCMP-006 prefers the longer term and returns plain text without a term', () => {
    textOf('A dead tuples count and a replication slot.')
    expect(screen.getByTitle(glossary['dead tuples'].short)).toHaveTextContent('dead tuples')
    expect(screen.getByTitle(glossary['replication slot'].short)).toHaveTextContent(
      'replication slot',
    )
    expect(withTerms('nothing here')).toBe('nothing here')
  })

  it('CAPCMP-006 a tooltip keeps the catalogue text and links its terms without a tab stop', () => {
    render(<InfoHint id="overview.section" label="Overview" />)
    fireEvent.focus(screen.getByRole('button', { name: 'About Overview' }))
    const tip = screen.getByRole('tooltip')
    const entry = CAPTIONS['overview.section']
    const terms = within(tip).getAllByTitle(/./)
    expect(terms.length).toBeGreaterThan(0)
    expect(tip).toHaveTextContent(entry.what)
    for (const term of terms) expect(term).not.toHaveAttribute('tabindex')
  })

  it('CAPCMP-006 the caption line clamps to two lines from 1600 px with the sentence in its title', () => {
    render(<Caption id="overview.tps" oneLine />)
    const line = screen.getByTitle(/./)
    expect(line).toHaveClass('min-[1600px]:line-clamp-2')
    expect(line).not.toHaveClass('min-[1600px]:truncate')
  })
})
