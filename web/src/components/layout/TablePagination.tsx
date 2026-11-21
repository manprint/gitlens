import { ChevronFirst, ChevronLast, ChevronLeft, ChevronRight } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import { useT } from '@/i18n/react'
import { PAGE_SIZES } from '@/lib/tableControls'

export interface TablePaginationProps {
  page: number
  pageCount: number
  pageSize: number
  onPage: (page: number) => void
  onPageSize: (size: number) => void
  /** Accessible name of the navigation, `<table> pagination`. */
  ariaLabel: string
}

/**
 * First / previous / next / last buttons, "Page X of Y" and the page-size
 * select (D23). The buttons are hidden when everything fits on one page;
 * the size select stays so a larger page can be chosen back.
 */
export function TablePagination({
  page,
  pageCount,
  pageSize,
  onPage,
  onPageSize,
  ariaLabel,
}: TablePaginationProps) {
  const t = useT()
  const steps = [
    { label: t('shell.table.firstPage'), Icon: ChevronFirst, target: 1, disabled: page <= 1 },
    {
      label: t('shell.table.previousPage'),
      Icon: ChevronLeft,
      target: page - 1,
      disabled: page <= 1,
    },
    {
      label: t('shell.table.nextPage'),
      Icon: ChevronRight,
      target: page + 1,
      disabled: page >= pageCount,
    },
    {
      label: t('shell.table.lastPage'),
      Icon: ChevronLast,
      target: pageCount,
      disabled: page >= pageCount,
    },
  ]
  return (
    <nav
      aria-label={ariaLabel}
      data-table-pager=""
      className="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs"
    >
      {pageCount > 1 ? (
        <div className="flex items-center gap-1">
          {steps.slice(0, 2).map(({ label, Icon, target, disabled }) => (
            <Button
              key={label}
              aria-label={label}
              disabled={disabled}
              onClick={() => onPage(target)}
              className="min-[1600px]:size-7"
              size="icon-sm"
              type="button"
              variant="outline"
            >
              <Icon aria-hidden="true" />
            </Button>
          ))}
          <span aria-live="polite" className="text-muted-foreground px-2 tabular-nums">
            {t('shell.table.pageOf', { page, pageCount })}
          </span>
          {steps.slice(2).map(({ label, Icon, target, disabled }) => (
            <Button
              key={label}
              aria-label={label}
              disabled={disabled}
              onClick={() => onPage(target)}
              className="min-[1600px]:size-7"
              size="icon-sm"
              type="button"
              variant="outline"
            >
              <Icon aria-hidden="true" />
            </Button>
          ))}
        </div>
      ) : null}
      <label className="text-muted-foreground flex items-center gap-2">
        {t('shell.table.rowsPerPage')}
        <NativeSelect
          aria-label={t('shell.table.rowsPerPage')}
          className="h-8 px-2 text-xs min-[1600px]:h-7"
          onChange={(event) => onPageSize(Number(event.target.value))}
          value={pageSize}
        >
          {PAGE_SIZES.map((size) => (
            <option key={size} value={size}>
              {size}
            </option>
          ))}
        </NativeSelect>
      </label>
    </nav>
  )
}
