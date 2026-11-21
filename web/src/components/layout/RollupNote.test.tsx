import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { refreshLocale, setServerLanguage } from '@/i18n/store'

import { RollupNote } from './RollupNote'

describe('RollupNote', () => {
  beforeEach(() => {
    vi.useRealTimers()
  })
  afterEach(async () => {
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('UI-RLP-003 a rollup response says hourly averages, in English and Italian', async () => {
    const view = render(<RollupNote resolution="1h" />)
    expect(screen.getByRole('note')).toHaveTextContent('Hourly averages (older than raw retention)')

    await setServerLanguage('it')
    view.rerender(<RollupNote resolution="1h" />)
    expect(
      await screen.findByText('Medie orarie (oltre la retention dei dati grezzi)'),
    ).toBeInTheDocument()
  })

  it('UI-RLP-005 a raw response, or one without resolution, shows no note', () => {
    render(
      <>
        <RollupNote resolution="raw" />
        <RollupNote resolution={undefined} />
      </>,
    )
    expect(screen.queryByRole('note')).toBeNull()
  })
})
