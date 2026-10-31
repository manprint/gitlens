import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

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
      stats={[{ label: 'Indexes', value: '302', unit: 'reported' }]}
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
  it('HDR-UI-001 the title block cannot shrink below the whole title, so the notes cannot cover it', () => {
    renderPanel()
    const header = screen.getByTestId('index-panel-header')
    // A row from 1600 px: the parts share the width (HDR-UI-003 covers the wrap).
    expect(header).toHaveClass('min-[1600px]:flex-row')

    const title = screen.getByRole('heading', { level: 2, name: 'Index inventory' })
    // The title stays on one line and keeps its full width.
    expect(title).toHaveClass('min-[1600px]:shrink-0', 'min-[1600px]:whitespace-nowrap')
    for (const name of fullHdClasses(title)) {
      expect(name).not.toMatch(/:(truncate|overflow-|min-w-|max-w-|w-)/)
    }

    // The title block keeps its automatic minimum width (no `min-w-*` and no
    // `overflow-*`, which would both let it shrink below its content).
    const titleBlock = screen.getByTestId('index-panel-title-block')
    expect(header).toContainElement(titleBlock)
    expect(titleBlock).toContainElement(title)
    expect(titleBlock).toHaveClass('min-[1600px]:flex', 'min-[1600px]:flex-1')
    expect(titleBlock.className).not.toMatch(/min-w-|overflow|truncate|w-0/)

    // The hint adds nothing to that minimum: it takes only the room left over.
    const hint = screen.getByTitle(
      'Every index the agent reported, with its size, scans and advisor flags.',
    )
    expect(titleBlock).toContainElement(hint)
    expect(hint).toHaveClass('min-[1600px]:w-0', 'min-[1600px]:flex-1', 'min-[1600px]:truncate')
  })

  it('HDR-UI-002 the notes give way to the title: their row and each chip shrink and truncate', () => {
    renderPanel()
    const header = screen.getByTestId('index-panel-header')
    const notes = screen.getAllByRole('status')
    expect(notes).toHaveLength(2)

    const row = screen.getByTestId('index-panel-notes')
    expect(header).toContainElement(row)
    for (const note of notes) expect(row).toContainElement(note)
    expect(row).toHaveClass('min-[1600px]:flex-1', 'min-[1600px]:flex-row')
    expect(row).not.toHaveClass('shrink-0', 'min-[1600px]:shrink-0')

    for (const note of notes) {
      expect(note).toHaveClass('min-[1600px]:min-w-0', 'min-[1600px]:truncate')
      expect(note).not.toHaveClass('shrink-0', 'min-[1600px]:shrink-0')
      // The full text stays reachable once the chip is cut.
      expect(note).toHaveAttribute('title', note.textContent)
    }
  })

  it('HDR-UI-003 a line too narrow for its parts wraps instead of letting the notes spill or the actions leave the card', () => {
    renderPanel()
    const header = screen.getByTestId('index-panel-header')
    // The line may wrap from 1600 px, with a tight row gap; at 1920 px the parts fit one line.
    expect(header).toHaveClass('min-[1600px]:flex-wrap', 'min-[1600px]:gap-y-2')

    // The notes keep a floor (two chips cut to an ellipsis), so the line wraps
    // before they are squeezed below their padding; they take the room the
    // title leaves, never more than their text needs.
    const row = screen.getByTestId('index-panel-notes')
    expect(row).toHaveClass(
      'min-[1600px]:min-w-24',
      'min-[1600px]:flex-1',
      'min-[1600px]:max-w-fit',
    )
    expect(row).not.toHaveClass('min-[1600px]:min-w-0')

    // The actions keep their width and sit at the right end, also on a second line.
    const actions = screen.getByTestId('index-panel-actions')
    expect(header).toContainElement(actions)
    expect(actions).toContainElement(screen.getByRole('button', { name: 'Copy' }))
    expect(actions).toHaveClass('shrink-0', 'min-[1600px]:ml-auto')
  })
})
