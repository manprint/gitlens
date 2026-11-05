import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { noIndexStatsText } from './emptyText'
import { IndexPanel, PanelNote } from './IndexPanel'

const LONG_NOTE =
  'The list is incomplete: the agent stopped after the first indexes of a large database.'

function renderPanel() {
  return render(
    <IndexPanel
      actions={<button type="button">Copy</button>}
      description="Every index the agent reported, with its size, scans and advisor flags."
      notes={
        <>
          <PanelNote title={LONG_NOTE} tone="warning">
            {LONG_NOTE}
          </PanelNote>
          <PanelNote title="Unused-index findings need a longer observation window.">
            Unused-index findings need a longer observation window.
          </PanelNote>
        </>
      }
      stats={[{ label: 'Indexes', value: '302', unit: 'indexes' }]}
      title="Index inventory"
      titleId="panel-title"
    >
      <p>table</p>
    </IndexPanel>,
  )
}

/** The classes of `element` that apply from 1600 px. */
function fullHdClasses(element: Element): string[] {
  return [...element.classList].filter((name) => name.startsWith('min-[1600px]:'))
}

describe('IndexPanel header on a Full HD screen (bug-HDR)', () => {
  it('HDR-UI-001 the first row keeps the whole title and gives the description the rest, two lines at most', () => {
    renderPanel()
    const header = screen.getByTestId('index-panel-header')
    const first = screen.getByTestId('index-panel-first-row')
    expect(header).toContainElement(first)
    expect(first).toHaveClass('min-[1600px]:flex-row')

    const title = screen.getByRole('heading', { level: 2, name: 'Index inventory' })
    expect(title).toHaveClass('min-[1600px]:shrink-0', 'min-[1600px]:whitespace-nowrap')
    for (const name of fullHdClasses(title)) {
      expect(name).not.toMatch(/:(truncate|overflow-|min-w-|max-w-|w-)/)
    }

    const titleBlock = screen.getByTestId('index-panel-title-block')
    expect(first).toContainElement(titleBlock)
    expect(titleBlock).toContainElement(title)
    expect(titleBlock).toHaveClass('min-[1600px]:flex', 'min-[1600px]:flex-1')

    const description = screen.getByTitle(
      'Every index the agent reported, with its size, scans and advisor flags.',
    )
    expect(titleBlock).toContainElement(description)
    expect(description).toHaveClass(
      'min-[1600px]:line-clamp-2',
      'min-[1600px]:min-w-0',
      'min-[1600px]:flex-1',
    )
    expect(description.className).not.toMatch(/truncate/)
  })

  it('HDR-UI-002 the notes and figures sit on a second row; a note wraps and is never cut', () => {
    renderPanel()
    const header = screen.getByTestId('index-panel-header')
    const second = screen.getByTestId('index-panel-second-row')
    expect(header).toContainElement(second)
    expect(screen.getByTestId('index-panel-first-row')).not.toContainElement(second)

    const notes = screen.getAllByRole('status')
    expect(notes).toHaveLength(2)
    const row = screen.getByTestId('index-panel-notes')
    expect(second).toContainElement(row)
    for (const note of notes) {
      expect(row).toContainElement(note)
      expect(note.className).not.toMatch(/truncate/)
      expect(note).toHaveAttribute('title', note.textContent)
    }
    expect(second).toContainElement(screen.getByRole('article', { name: 'Indexes' }))
  })

  it('HDR-UI-003 a second row too narrow for its parts wraps; the actions stay at the right end of the first row', () => {
    renderPanel()
    const second = screen.getByTestId('index-panel-second-row')
    expect(second).toHaveClass('min-[1600px]:flex-wrap', 'min-[1600px]:gap-y-2')

    const row = screen.getByTestId('index-panel-notes')
    expect(row).toHaveClass('min-[1600px]:min-w-[28rem]', 'min-[1600px]:flex-1')

    const first = screen.getByTestId('index-panel-first-row')
    const actions = screen.getByTestId('index-panel-actions')
    expect(first).toContainElement(actions)
    expect(actions).toContainElement(screen.getByRole('button', { name: 'Copy' }))
    expect(actions).toHaveClass('shrink-0', 'min-[1600px]:ml-auto')
  })
})

describe('noIndexStatsText (M4)', () => {
  it('IDX-EMPTY-001 says no statistics arrived for a whole server, and names no agent fault', () => {
    const text = noIndexStatsText({ database: '', reported: 0, foreignKeys: 0 })
    expect(text).toContain(
      'No index usage statistics arrived from the agent in the last 15 minutes',
    )
    expect(text).not.toMatch(/has not reported/)
    expect(text).not.toMatch(/Foreign keys/)
  })

  it('IDX-EMPTY-002 points to the results that need only table definitions when foreign keys have gaps', () => {
    expect(noIndexStatsText({ database: '', reported: 0, foreignKeys: 3 })).toContain(
      'Foreign keys without an index',
    )
  })

  it('IDX-EMPTY-003 blames the database filter, not the agent, when other databases have indexes', () => {
    const text = noIndexStatsText({ database: 'app', reported: 12, foreignKeys: 0 })
    expect(text).toContain('The database app has no index in the latest collection.')
    expect(text).not.toMatch(/did not arrive|No index usage statistics/)
  })
})
