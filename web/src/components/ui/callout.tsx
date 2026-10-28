import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'

import { cn } from '@/lib/utils'

const calloutVariants = cva(
  'relative w-full rounded-lg border px-4 py-3 text-sm [&>[data-slot=button]]:mt-2 [&>p]:mt-1 [&>strong]:mr-1',
  {
    variants: {
      variant: {
        default: 'bg-card text-card-foreground',
        destructive: 'border-destructive/35 bg-destructive/10 text-foreground',
        warning: 'border-warning/40 bg-warning/10 text-foreground',
      },
    },
    defaultVariants: {
      variant: 'default',
    },
  },
)

/**
 * A callout box. It announces itself as an alert; a notice that is not urgent
 * passes role="status" instead.
 */
function Callout({
  className,
  variant,
  ...props
}: React.ComponentProps<'div'> & VariantProps<typeof calloutVariants>) {
  return (
    <div
      data-slot="callout"
      role="alert"
      className={cn(calloutVariants({ variant }), className)}
      {...props}
    />
  )
}

export { Callout }
