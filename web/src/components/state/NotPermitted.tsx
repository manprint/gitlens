import { cloneElement, type ReactElement } from 'react'

import type { PermTier } from '@/api/types'

interface ControlProps {
  'aria-disabled'?: boolean | 'true' | 'false'
  'aria-label'?: string
  disabled?: boolean
}

interface NotPermittedProps {
  required: 'T1' | 'T2'
  current: PermTier
  children?: ReactElement<ControlProps>
}

export function NotPermitted({ required, current, children }: NotPermittedProps) {
  const reason = `Requires ${required} permission; current tier is ${current}.`
  const control = children
    ? cloneElement(children, {
        'aria-disabled': true,
        'aria-label': [children.props['aria-label'], reason].filter(Boolean).join(' — '),
        disabled: true,
      })
    : null

  return (
    <div role="status" className="border-muted bg-muted/30 text-sm">
      <p>{reason}</p>
      {control}
    </div>
  )
}
