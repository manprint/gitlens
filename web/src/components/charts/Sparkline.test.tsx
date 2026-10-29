import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { Sparkline } from './Sparkline'

describe('Sparkline', () => {
  it('SPARK-004 is an image with a name', () => {
    render(<Sparkline label="Dead tuples of orders" values={[1, 5, 3]} />)
    expect(screen.getByRole('img', { name: 'Dead tuples of orders' })).toBeInTheDocument()
  })

  it('SPARK-005 draws nothing for fewer than two values', () => {
    render(<Sparkline label="none" values={[4]} />)
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
  })
})
