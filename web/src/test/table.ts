import { fireEvent, screen, within } from '@testing-library/react'

/** The table named `name`; several data tables can share a page. */
export function tableNamed(name: string): HTMLElement {
  return screen.getByRole('table', { name })
}

/** The body rows of the table named `name` (the header row excluded). */
export function dataRows(name: string): HTMLElement[] {
  const body = within(tableNamed(name)).getAllByRole('rowgroup')[1]
  if (!body) throw new Error(`table ${name} has no body`)
  return within(body).queryAllByRole('row')
}

/** The text of one column (0-based) in every body row of the table named `name`. */
export function columnTexts(name: string, column = 0): string[] {
  return dataRows(name).map((row) => within(row).getAllByRole('cell')[column]?.textContent ?? '')
}

export function searchTable(name: string, query: string, label = `Search ${name}`): void {
  fireEvent.change(screen.getByRole('searchbox', { name: label }), { target: { value: query } })
}

export function filterTable(name: string, column: string, value: string): void {
  fireEvent.change(screen.getByRole('combobox', { name: `Filter ${name} by ${column}` }), {
    target: { value },
  })
}

export function sortTable(
  name: string,
  column: string,
  direction: 'ascending' | 'descending',
): void {
  fireEvent.click(
    within(tableNamed(name)).getByRole('button', { name: `Sort ${column} ${direction}` }),
  )
}

/** `01` … `25`: zero-padded so text order equals number order. */
export function seq(count: number): string[] {
  return Array.from({ length: count }, (_, index) => String(index + 1).padStart(2, '0'))
}
