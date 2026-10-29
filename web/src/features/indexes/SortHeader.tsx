interface SortHeaderProps {
  active: boolean
  descending: boolean
  label: string
  onSort: () => void
}

export function SortHeader({ active, descending, label, onSort }: SortHeaderProps) {
  return (
    <th
      aria-sort={active ? (descending ? 'descending' : 'ascending') : 'none'}
      className="px-3 py-2 text-left font-medium"
      scope="col"
    >
      <button
        className="hover:text-foreground inline-flex items-center gap-1 uppercase"
        onClick={onSort}
        type="button"
      >
        {label}
        {active ? <span aria-hidden="true">{descending ? '↓' : '↑'}</span> : null}
      </button>
    </th>
  )
}
