import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { CopyButton } from './CopyButton'

function stubClipboard(writeText: ((text: string) => Promise<void>) | undefined) {
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: writeText ? { writeText } : undefined,
  })
}

function stubExecCommand(result: boolean | Error) {
  const execCommand = vi.fn(() => {
    if (result instanceof Error) throw result
    return result
  })
  Object.defineProperty(document, 'execCommand', { configurable: true, value: execCommand })
  return execCommand
}

async function clickCopy() {
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: 'Copy query text' }))
    await Promise.resolve()
  })
}

afterEach(() => {
  Reflect.deleteProperty(navigator, 'clipboard')
  Reflect.deleteProperty(document, 'execCommand')
})

describe('CopyButton', () => {
  it('copies through the clipboard API and returns to idle after a moment', async () => {
    const writeText = vi.fn(() => Promise.resolve())
    stubClipboard(writeText)
    render(<CopyButton text="SELECT 1" />)

    await clickCopy()

    expect(writeText).toHaveBeenCalledWith('SELECT 1')
    expect(screen.getByRole('button', { name: 'Query text copied' })).toHaveTextContent('Copied')

    act(() => {
      vi.advanceTimersByTime(1_500)
    })
    expect(screen.getByRole('button', { name: 'Copy query text' })).toHaveTextContent('Copy')
  })

  it('copies through a selection when the page is not a secure context', async () => {
    stubClipboard(undefined)
    const execCommand = stubExecCommand(true)
    render(<CopyButton text="SELECT 2" />)

    await clickCopy()

    expect(execCommand).toHaveBeenCalledWith('copy')
    expect(screen.getByRole('button', { name: 'Query text copied' })).toBeInTheDocument()
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
  })

  it('falls back to the selection when the clipboard permission is denied', async () => {
    stubClipboard(() => Promise.reject(new Error('denied')))
    const execCommand = stubExecCommand(true)
    render(<CopyButton text="SELECT 3" />)

    await clickCopy()

    expect(execCommand).toHaveBeenCalledWith('copy')
    expect(screen.getByRole('button', { name: 'Query text copied' })).toBeInTheDocument()
  })

  it('says so when no copy path works', async () => {
    stubClipboard(undefined)
    stubExecCommand(new Error('unsupported'))
    render(<CopyButton text="SELECT 4" />)

    await clickCopy()

    expect(screen.getByRole('button', { name: 'Could not copy query text' })).toHaveTextContent(
      'Copy failed',
    )
  })
})
