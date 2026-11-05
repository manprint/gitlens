import type { ReactNode } from 'react'
import { Search, X } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import type { TableState, TableStatePatch } from '@/hooks/useTableState'
import type { FilterOption, FilterSpec } from '@/lib/tableControls'

export interface TableToolbarProps<T> {
  /** The table's name, used in the control labels (`Search <table>`, `Filter <table> by …`). */
  label: string
  /** Overrides `Search <table>`. */
  searchLabel?: string
  filters: readonly FilterSpec<T>[]
  /** The choices of each select/multi filter, by filter id. */
  options: Readonly<Record<string, readonly FilterOption[]>>
  state: TableState
  activeFilterCount: number
  onChange: (patch: TableStatePatch) => void
  onClear: () => void
  /** Page controls placed after the filters, on the same line (D26). */
  extra?: ReactNode
}

/**
 * Search box, one control per filter, the active-filter count and "Clear
 * filters" (D12). From 1600 px the toolbar is one line (D26): the column
 * names above the controls are hidden and each select's empty choice names
 * its column (`Kind: all`). Each control is named only by its table-scoped aria-label;
 * the column name above it is hidden from the accessibility tree so it never
 * shares a label with another control on the page.
 */
export function TableToolbar<T>({
  label,
  searchLabel,
  filters,
  options,
  state,
  activeFilterCount,
  onChange,
  onClear,
  extra,
}: TableToolbarProps<T>) {
  const setFilter = (id: string, value: string) =>
    onChange({ filters: { ...state.filters, [id]: value } })

  return (
    <div className="flex flex-wrap items-end gap-3 min-[1600px]:gap-x-2">
      <div role="search" aria-label={`${label} search`} className="relative w-full max-w-xs">
        <Search
          aria-hidden="true"
          className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2"
        />
        <Input
          aria-label={searchLabel ?? `Search ${label}`}
          className="h-8 pl-8 text-sm min-[1600px]:h-7"
          onChange={(event) => onChange({ q: event.target.value })}
          placeholder="Search"
          type="search"
          value={state.q}
        />
      </div>

      {filters.map((spec) => {
        const name = `Filter ${label} by ${spec.label}`
        const value = state.filters[spec.id] ?? ''
        const choices = options[spec.id] ?? spec.options ?? []
        if (spec.kind === 'min') {
          return (
            <div key={spec.id} className="text-muted-foreground flex flex-col gap-1 text-xs">
              <span aria-hidden="true" className="min-[1600px]:hidden">
                {spec.label} at least
              </span>
              <Input
                aria-label={name}
                className="h-8 w-28 text-sm min-[1600px]:h-7"
                inputMode="numeric"
                min={0}
                onChange={(event) => setFilter(spec.id, event.target.value)}
                type="number"
                value={value}
              />
            </div>
          )
        }
        if (spec.kind === 'multi') {
          const selected = new Set(value.split(',').filter(Boolean))
          return (
            <fieldset key={spec.id} className="flex flex-col gap-1 text-xs">
              <legend className="sr-only">{name}</legend>
              <span aria-hidden="true" className="text-muted-foreground min-[1600px]:hidden">
                {spec.label}
              </span>
              <div className="flex h-8 flex-wrap items-center gap-3 min-[1600px]:h-7">
                {choices.map((choice) => (
                  <label key={choice.value} className="flex items-center gap-1.5 text-sm">
                    <input
                      checked={selected.has(choice.value)}
                      onChange={(event) => {
                        const next = new Set(selected)
                        if (event.target.checked) next.add(choice.value)
                        else next.delete(choice.value)
                        setFilter(
                          spec.id,
                          choices
                            .filter((item) => next.has(item.value))
                            .map((item) => item.value)
                            .join(','),
                        )
                      }}
                      type="checkbox"
                    />
                    {choice.label}
                  </label>
                ))}
              </div>
            </fieldset>
          )
        }
        return (
          <div key={spec.id} className="text-muted-foreground flex flex-col gap-1 text-xs">
            <span aria-hidden="true" className="min-[1600px]:hidden">
              {spec.label}
            </span>
            <NativeSelect
              aria-label={name}
              className="h-8 px-2 text-sm min-[1600px]:h-7"
              onChange={(event) => setFilter(spec.id, event.target.value)}
              value={value}
            >
              <option value="">{`${spec.label}: all`}</option>
              {choices.map((choice) => (
                <option key={choice.value} value={choice.value}>
                  {choice.label}
                </option>
              ))}
            </NativeSelect>
          </div>
        )
      })}

      {extra}

      {activeFilterCount > 0 ? (
        <div className="flex h-8 items-center gap-2 text-xs min-[1600px]:h-7">
          <span className="text-muted-foreground">
            {activeFilterCount} {activeFilterCount === 1 ? 'filter' : 'filters'} active
          </span>
          <Button onClick={onClear} size="xs" type="button" variant="ghost">
            <X aria-hidden="true" />
            Clear filters
          </Button>
        </div>
      ) : null}
    </div>
  )
}
