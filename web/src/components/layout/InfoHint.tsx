import { Info } from 'lucide-react'
import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from 'react'

import { createPortal } from 'react-dom'

import { CAPTIONS, columnHelp, type CaptionId, type ColumnHelpId } from '@/lib/captions'
import { cn } from '@/lib/utils'

import { placeTooltip, type TooltipSide } from './tooltipPlacement'

/** Hover waits this long before the tooltip opens, so passing the pointer over it does nothing. */
export const HINT_OPEN_DELAY_MS = 150
/** Leaving waits this long before the tooltip closes, so the pointer can move onto it (WCAG 1.4.13). */
export const HINT_CLOSE_DELAY_MS = 100

interface HintButtonProps {
  label: string
  className?: string | undefined
  children: ReactNode
}

/**
 * A circled ⓘ button whose tooltip opens on hover (after a short delay) or
 * keyboard focus. The tooltip stays open while the pointer is on the button or
 * on the tooltip, and closes on mouse leave, blur or Escape (anywhere on the
 * page). A click or tap pins it open for touch screens (`aria-expanded`); a tap
 * outside closes it. It is placed above the button, flips to stay inside the
 * viewport and follows the button when the page or a table scrolls.
 */
function HintButton({ label, className, children }: HintButtonProps) {
  const tooltipId = `${useId().replaceAll(':', '')}-hint`
  const button = useRef<HTMLButtonElement>(null)
  const tip = useRef<HTMLDivElement>(null)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const [open, setOpen] = useState(false)
  const [pinned, setPinned] = useState(false)
  const [position, setPosition] = useState<{ side: TooltipSide; style: CSSProperties }>()

  const cancel = useCallback(() => {
    if (timer.current !== undefined) clearTimeout(timer.current)
    timer.current = undefined
  }, [])
  const close = useCallback(() => {
    cancel()
    setOpen(false)
    setPinned(false)
  }, [cancel])
  const later = (action: () => void, delay: number) => {
    cancel()
    timer.current = setTimeout(action, delay)
  }
  // The pointer left the button or the tooltip: close unless pinned or focused.
  const leave = () => {
    if (pinned || document.activeElement === button.current) {
      cancel()
      return
    }
    later(() => setOpen(false), HINT_CLOSE_DELAY_MS)
  }

  useEffect(() => cancel, [cancel])

  const place = useCallback(() => {
    if (!button.current || !tip.current) return
    const anchor = button.current.getBoundingClientRect()
    const box = tip.current.getBoundingClientRect()
    const placement = placeTooltip(
      { top: anchor.top, left: anchor.left, width: anchor.width, height: anchor.height },
      { width: box.width, height: box.height },
      { width: window.innerWidth, height: window.innerHeight },
    )
    setPosition({ side: placement.side, style: { top: placement.top, left: placement.left } })
  }, [])

  useLayoutEffect(() => {
    if (open) place()
  }, [open, place])

  useEffect(() => {
    if (!open) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') close()
    }
    const onPointer = (event: PointerEvent) => {
      const target = event.target as Node | null
      if (target && (button.current?.contains(target) || tip.current?.contains(target))) return
      close()
    }
    document.addEventListener('keydown', onKey)
    document.addEventListener('pointerdown', onPointer)
    // Capture: a scrolling table card does not bubble its scroll to the window.
    window.addEventListener('scroll', place, true)
    window.addEventListener('resize', place)
    return () => {
      document.removeEventListener('keydown', onKey)
      document.removeEventListener('pointerdown', onPointer)
      window.removeEventListener('scroll', place, true)
      window.removeEventListener('resize', place)
    }
  }, [close, open, place])

  return (
    <span className={cn('inline-flex', className)}>
      <button
        ref={button}
        type="button"
        aria-label={`About ${label}`}
        aria-expanded={pinned}
        aria-controls={tooltipId}
        aria-describedby={tooltipId}
        className="text-muted-foreground hover:text-foreground focus-visible:ring-ring/50 inline-flex size-5 shrink-0 items-center justify-center rounded-full outline-none focus-visible:ring-[3px]"
        onMouseEnter={() => later(() => setOpen(true), HINT_OPEN_DELAY_MS)}
        onMouseLeave={leave}
        onFocus={() => {
          cancel()
          setOpen(true)
        }}
        onBlur={() => {
          if (!pinned) close()
        }}
        onClick={() => {
          cancel()
          setPinned(!pinned)
          setOpen(!pinned)
        }}
      >
        <Info aria-hidden="true" className="size-4" />
      </button>
      {/* Always rendered (hidden when closed), so the button's description and
          controls point at a stable element; in a portal because the button
          may sit inside a paragraph or a scrolling table card. */}
      {createPortal(
        <div
          ref={tip}
          id={tooltipId}
          role="tooltip"
          hidden={!open}
          data-side={position?.side ?? 'top'}
          className="bg-popover text-popover-foreground shadow-raised fixed z-50 w-72 max-w-[calc(100vw-1rem)] rounded-lg border p-3 text-left text-xs font-normal tracking-normal normal-case"
          style={position?.style ?? { top: 0, left: 0, visibility: 'hidden' }}
          onMouseEnter={cancel}
          onMouseLeave={leave}
        >
          {children}
        </div>,
        document.body,
      )}
    </span>
  )
}

const FIELDS = [
  ['source', 'Source'],
  ['unit', 'Unit'],
  ['window', 'Window'],
  ['howToRead', 'How to read'],
  ['thresholds', 'Thresholds'],
] as const

/**
 * The ⓘ of a caption: the `what` sentence and the fields the entry has.
 * `extra` comes first: the live state a compact element cannot show in full
 * (a truncated status line), so keyboard and touch users reach it too, as the
 * button's description and in the pinned tooltip.
 */
export function InfoHint({
  id,
  label,
  className,
  extra,
}: {
  id: CaptionId
  label: string
  className?: string
  extra?: ReactNode
}) {
  const entry = CAPTIONS[id]
  return (
    <HintButton className={className} label={label}>
      {extra === undefined ? null : (
        <div className="text-foreground mb-2 space-y-1 border-b pb-2 text-sm">{extra}</div>
      )}
      <p className="text-foreground mb-2 text-sm">{entry.what}</p>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
        {FIELDS.map(([key, name]) => {
          const value = (entry as Partial<Record<(typeof FIELDS)[number][0], string>>)[key]
          return value === undefined ? null : (
            <div key={key} className="contents">
              <dt className="text-muted-foreground font-medium">{name}</dt>
              <dd>{value}</dd>
            </div>
          )
        })}
      </dl>
    </HintButton>
  )
}

/** The ⓘ after a column header: one short sentence about the column. */
export function ColumnHint({ id, label }: { id: ColumnHelpId; label: string }) {
  return (
    <HintButton label={label}>
      <p>{columnHelp[id]}</p>
    </HintButton>
  )
}
