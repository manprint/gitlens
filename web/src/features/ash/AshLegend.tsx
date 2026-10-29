import { formatAverage, swatchLetters } from '@/lib/ashGroups'
import { cn } from '@/lib/utils'

export interface AshLegendEntry {
  name: string
  /** Colour token of the series, as the chart draws it. */
  color: string
  description: string
  /** Average active backends of the series over the window. */
  average: number
}

export interface AshLegendProps {
  label: string
  entries: readonly AshLegendEntry[]
  /** The series shown alone in its chart, or null when every series shows. */
  isolated: string | null
  onToggle: (name: string) => void
}

/**
 * HTML legend of one wait-group chart. Each swatch carries the series' first
 * letter, so a series is told apart without relying on colour alone. A row
 * shows its series alone in the chart; pressing it again shows them all.
 */
export function AshLegend({ entries, isolated, label, onToggle }: AshLegendProps) {
  const letters = swatchLetters(entries.map(({ name }) => name))
  return (
    <ul aria-label={label} className="mt-3 space-y-1">
      {entries.map((entry, index) => {
        const letter = letters[index] ?? ''
        const pressed = isolated === entry.name
        const dimmed = isolated !== null && !pressed
        return (
          <li key={entry.name}>
            <button
              aria-pressed={pressed}
              className={cn(
                'hover:bg-muted focus-visible:ring-ring flex w-full flex-wrap items-start gap-x-2 gap-y-0.5 rounded px-2 py-1 text-left text-sm focus-visible:ring-2 focus-visible:outline-none',
                dimmed && 'opacity-50',
              )}
              onClick={() => onToggle(entry.name)}
              type="button"
            >
              <span
                aria-hidden="true"
                className="mt-0.5 inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-sm text-[10px] leading-none font-bold text-white"
                data-swatch-letter={letter}
                data-testid="ash-legend-swatch"
                style={{ backgroundColor: entry.color }}
              >
                {letter}
              </span>
              <span className="min-w-0 flex-1 basis-0">
                <span className="font-medium">{entry.name}</span>{' '}
                <span className="text-muted-foreground">— {entry.description}</span>
              </span>{' '}
              <span className="text-muted-foreground basis-full pl-6 tabular-nums sm:basis-auto sm:pl-0">
                avg {formatAverage(entry.average)} active backends
              </span>
            </button>
          </li>
        )
      })}
    </ul>
  )
}
