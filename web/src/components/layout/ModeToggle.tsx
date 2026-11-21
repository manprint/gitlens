import { cn } from '@/lib/utils'

export interface ModeToggleProps<T extends string> {
  ariaLabel: string
  name: string
  value: T
  options: readonly { value: T; label: string }[]
  onChange: (value: T) => void
}

/** A segmented radio group, the look of the Wait events chart layout switch. */
export function ModeToggle<T extends string>({
  ariaLabel,
  name,
  value,
  options,
  onChange,
}: ModeToggleProps<T>) {
  return (
    <div
      aria-label={ariaLabel}
      className="bg-muted/40 inline-flex items-center gap-1 rounded-lg border p-1 text-sm"
      role="radiogroup"
    >
      {options.map((option) => (
        <label
          className={cn(
            'focus-within:ring-ring cursor-pointer rounded-md px-3 py-1 whitespace-nowrap focus-within:ring-2',
            value === option.value
              ? 'bg-background font-medium shadow-sm'
              : 'text-muted-foreground',
          )}
          key={option.value}
        >
          <input
            checked={value === option.value}
            className="sr-only"
            name={name}
            onChange={() => onChange(option.value)}
            type="radio"
            value={option.value}
          />
          {option.label}
        </label>
      ))}
    </div>
  )
}
