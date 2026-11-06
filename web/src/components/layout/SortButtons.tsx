import { ArrowDown, ArrowUp } from 'lucide-react'

import type { SortDirection } from '@/lib/tableControls'
import { cn } from '@/lib/utils'

export interface SortButtonsProps {
  /** Column name used in the buttons' accessible names. */
  label: string
  /** The column's current direction, or null when the table is not sorted by it. */
  direction: SortDirection | null
  /** Called with the chosen direction, or null when the active one is pressed again. */
  onChange: (direction: SortDirection | null) => void
}

const DIRECTIONS = [
  { dir: 'asc', name: 'ascending', Icon: ArrowUp },
  { dir: 'desc', name: 'descending', Icon: ArrowDown },
] as const

/**
 * Two compact sort buttons for a column header (D13). The surrounding `th`
 * carries `aria-sort`; pressing the active direction clears the sort.
 */
export function SortButtons({ label, direction, onChange }: SortButtonsProps) {
  return (
    <span className="inline-flex items-center align-middle">
      {DIRECTIONS.map(({ dir, name, Icon }) => {
        const active = direction === dir
        return (
          <button
            key={dir}
            type="button"
            aria-label={`Sort ${label} ${name}`}
            aria-pressed={active}
            onClick={() => onChange(active ? null : dir)}
            className={cn(
              'inline-flex size-6 items-center justify-center rounded transition-colors outline-none min-[1600px]:size-5',
              'focus-visible:ring-ring/50 focus-visible:ring-[3px]',
              active
                ? 'text-foreground bg-muted'
                : 'text-muted-foreground/60 hover:text-foreground hover:bg-muted/60',
            )}
          >
            <Icon aria-hidden="true" className="size-3.5" />
          </button>
        )
      })}
    </span>
  )
}
