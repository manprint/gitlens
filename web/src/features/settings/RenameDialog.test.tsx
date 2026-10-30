import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { expectNoA11yViolations } from '@/test/a11y'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'

import { renameErrorMessage } from '@/lib/names'

import { RenameDialog, type RenameDialogProps } from './RenameDialog'

function renderDialog(overrides: Partial<RenameDialogProps> = {}) {
  const onSave = vi.fn<(name: string | null) => Promise<unknown>>(() => Promise.resolve())
  const view = renderWithProviders(
    <RenameDialog
      current="production"
      fallback="Cluster 42"
      kind="cluster"
      onSave={onSave}
      subject="production"
      {...overrides}
    />,
  )
  return { ...view, onSave: overrides.onSave ?? onSave }
}

async function openDialog(name = 'Rename cluster production') {
  fireEvent.click(screen.getByRole('button', { name }))
  await flushNetwork(2)
  return screen.getByRole('dialog', { name: /Rename/ })
}

describe('RenameDialog', () => {
  it('CLNAME-UI-002 opens with the current value and focuses the input', async () => {
    renderDialog()
    const dialog = await openDialog()

    const input = within(dialog).getByRole('textbox', { name: 'Cluster name' })
    expect(input).toHaveValue('production')
    expect(input).toHaveFocus()
    expect(
      within(dialog).getByText(/1–64 characters; leave empty to restore the default/),
    ).toHaveTextContent('(Cluster 42)')
  })

  it('CLNAME-UI-002 an instance without a name opens empty', async () => {
    renderDialog({
      current: undefined,
      fallback: 'db.example.test:5432',
      kind: 'instance',
      subject: 'db.example.test:5432',
    })
    const dialog = await openDialog('Rename instance db.example.test:5432')

    expect(within(dialog).getByRole('textbox', { name: 'Instance name' })).toHaveValue('')
  })

  it('CLNAME-UI-003 Save is disabled while the name is unchanged or invalid', async () => {
    const { onSave } = renderDialog()
    const dialog = await openDialog()
    const input = within(dialog).getByRole('textbox', { name: 'Cluster name' })
    const save = within(dialog).getByRole('button', { name: 'Save' })

    // Unchanged, also after trimming.
    expect(save).toBeDisabled()
    fireEvent.change(input, { target: { value: '  production  ' } })
    expect(save).toBeDisabled()

    // Too long: the error is shown and linked to the input.
    fireEvent.change(input, { target: { value: 'a'.repeat(65) } })
    expect(save).toBeDisabled()
    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(input).toHaveAccessibleDescription(/Use at most 64 characters\./)

    // A control character.
    fireEvent.change(input, { target: { value: 'a\tb' } })
    expect(save).toBeDisabled()
    expect(input).toHaveAccessibleDescription(/Remove tabs, line breaks/)

    // A valid new name.
    fireEvent.change(input, { target: { value: 'prod-eu' } })
    expect(save).toBeEnabled()
    expect(input).not.toHaveAttribute('aria-invalid')

    // The form does not submit an invalid name with Enter either.
    fireEvent.change(input, { target: { value: 'a'.repeat(65) } })
    fireEvent.submit(input)
    expect(onSave).not.toHaveBeenCalled()
  })

  it('CLNAME-UI-004 saves the trimmed name, closes and returns the focus to Rename', async () => {
    const { onSave } = renderDialog()
    const dialog = await openDialog()
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Cluster name' }), {
      target: { value: '  prod-eu ' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await flushNetwork()

    expect(onSave).toHaveBeenCalledWith('prod-eu')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Rename cluster production' })).toHaveFocus()
  })

  it('CLNAME-UI-005 an empty name is sent as null', async () => {
    const { onSave } = renderDialog()
    const dialog = await openDialog()
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Cluster name' }), {
      target: { value: '   ' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await flushNetwork()

    expect(onSave).toHaveBeenCalledWith(null)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('CLNAME-UI-006 a refused name keeps the dialog open with the typed value', async () => {
    // The query layer throws an Error that carries the ApiFailure fields.
    const onSave = vi.fn(() =>
      Promise.reject(
        Object.assign(new Error('name already in use'), {
          kind: 'server',
          status: 409,
          error: 'name already in use',
          detail: 'another cluster has this name',
        }),
      ),
    )
    renderDialog({ onSave })
    const dialog = await openDialog()
    const input = within(dialog).getByRole('textbox', { name: 'Cluster name' })
    fireEvent.change(input, { target: { value: 'staging' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
    await flushNetwork()

    expect(within(dialog).getByRole('alert')).toHaveTextContent('That name is already used')
    expect(input).toHaveValue('staging')
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    // Typing again clears the server's answer.
    fireEvent.change(input, { target: { value: 'staging-2' } })
    expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument()
  })

  it('CLNAME-UI-006 other failures show the server message', () => {
    expect(
      renameErrorMessage(
        {
          kind: 'server',
          status: 400,
          error: 'invalid name',
          detail: 'the name may not exceed 64 characters',
        },
        'cluster',
      ),
    ).toBe('invalid name: the name may not exceed 64 characters')
    expect(
      renameErrorMessage(
        { kind: 'server', status: 503, error: 'store unavailable', detail: '' },
        'instance',
      ),
    ).toBe('store unavailable')
    expect(renameErrorMessage({ kind: 'not_found' }, 'instance')).toBe(
      'The instance no longer exists. Reload the page.',
    )
    expect(renameErrorMessage({ kind: 'network', message: 'Failed to fetch' }, 'cluster')).toBe(
      'Failed to fetch',
    )
    expect(renameErrorMessage(new Error('boom'), 'cluster')).toBe(
      'The name could not be saved. Try again.',
    )
  })

  it('CLNAME-UI-009 the open dialog has no serious accessibility violations', async () => {
    renderDialog()
    const dialog = await openDialog()
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Cluster name' }), {
      target: { value: 'a'.repeat(65) },
    })

    expect(dialog).toHaveAccessibleName('Rename cluster')

    // axe schedules its own timers; the suite's fake clock would hold them.
    vi.useRealTimers()
    await expectNoA11yViolations(dialog)
  }, 15000)
})
