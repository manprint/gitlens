import { useEffect, useId, useRef } from 'react'
import { LoaderCircle, Search, X } from 'lucide-react'

import { Input } from '@/components/ui/input'
import { useSearchDraft, type SearchDraft } from '@/hooks/useSearch'
import { HIGHLIGHT_MIN_LENGTH, highlightMatches } from '@/lib/highlight'
import { cn } from '@/lib/utils'

/** How long the box waits after the last keystroke before it searches. */
export const SEARCH_DEBOUNCE_MS = 300

/** The hint shown while the term is too short to search. */
export const SEARCH_TOO_SHORT = 'Type at least 2 characters'

/** The line under the full-search boxes (D20): the search is bounded by what the agent kept. */
export const FULL_SEARCH_HINT = 'Searches everything pglens has collected in this range'

/** The longest term the server searches for (D20); the box takes no more. */
export const SEARCH_MAX_LENGTH = 200

export interface SearchBoxProps {
  /** The searched term, as the URL holds it. */
  value: string
  /**
   * Called 300 ms after the last keystroke with the typed text, or at once
   * with '' when the box is cleared. A term shorter than 2 characters once
   * trimmed is never passed on.
   */
  onChange: (value: string) => void
  /** The accessible name of the box, e.g. `Search statements`. */
  label: string
  placeholder?: string
  /** A search is running. */
  busy?: boolean
  /** The explanatory line under the box. */
  hint?: string
  /** The input's id, so a page that moves the box can give it focus back (`useKeepFocus`). */
  id?: string
  className?: string
  /** Classes of the box itself, e.g. its width on a crowded line. */
  boxClassName?: string
  /**
   * The draft from `useSearchDraft`, for a page whose box can remount (it
   * moves between the table toolbar and a bar): the typed text survives.
   */
  draft?: SearchDraft
}

const length = (text: string) => [...text.trim()].length

/** Passes `next` on, telling the draft first so the URL echo does not replace it. */
function pass(
  callbacks: { onChange: (value: string) => void; emitted: (value: string) => void },
  next: string,
) {
  callbacks.emitted(next)
  callbacks.onChange(next)
}

/**
 * The search box of the full-search pages (D20): controlled by the URL's `q`,
 * debounced, with a clear button and a busy state; it takes at most 200
 * characters, the server's limit. Below 1600 px the hint is a line under the
 * box; from 1600 px the box keeps the toolbar on one line (D26), so the hint
 * becomes the input's tooltip and stays its description, and the too-short
 * hint floats under the box.
 */
export function SearchBox({
  value,
  onChange,
  label,
  placeholder = 'Search',
  busy = false,
  hint,
  id,
  className,
  boxClassName,
  draft: pageDraft,
}: SearchBoxProps) {
  const descriptionId = `${useId().replaceAll(':', '')}-search-hint`
  const input = useRef<HTMLInputElement>(null)
  // A new value from outside (the URL, a clear action) replaces the draft,
  // unless it is the value the box itself passed on (useSearchDraft).
  const ownDraft = useSearchDraft(value)
  const { draft, setDraft, emitted } = pageDraft ?? ownDraft

  // The latest callbacks, so a parent that passes new functions each render
  // does not restart the wait.
  const callback = useRef({ onChange, emitted })
  useEffect(() => {
    callback.current = { onChange, emitted }
  }, [onChange, emitted])

  useEffect(() => {
    const term = draft.trim()
    if (term === value.trim()) return
    if (term !== '' && length(term) < HIGHLIGHT_MIN_LENGTH) return
    const timer = setTimeout(
      () => pass(callback.current, term === '' ? '' : draft),
      SEARCH_DEBOUNCE_MS,
    )
    return () => clearTimeout(timer)
  }, [draft, value])

  const tooShort = draft.trim() !== '' && length(draft) < HIGHLIGHT_MIN_LENGTH
  const description = tooShort ? SEARCH_TOO_SHORT : hint

  function clear() {
    setDraft('')
    if (value !== '') pass(callback.current, '')
    input.current?.focus()
  }

  return (
    <div
      aria-busy={busy}
      aria-label={label}
      className={cn('relative flex flex-col gap-1', className)}
      role="search"
    >
      <div
        className={cn(
          'relative w-full max-w-xs min-[1600px]:w-56 min-[1600px]:shrink-0',
          boxClassName,
        )}
      >
        <Search
          aria-hidden="true"
          className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2"
        />
        <Input
          ref={input}
          aria-label={label}
          {...(description === undefined ? {} : { 'aria-describedby': descriptionId })}
          className="h-8 pr-14 pl-8 text-sm [&::-webkit-search-cancel-button]:appearance-none"
          id={id}
          maxLength={SEARCH_MAX_LENGTH}
          onChange={(event) => setDraft(event.target.value)}
          placeholder={placeholder}
          title={hint}
          type="search"
          value={draft}
        />
        <div className="absolute top-1/2 right-1 flex -translate-y-1/2 items-center gap-0.5">
          {busy ? (
            <LoaderCircle
              aria-hidden="true"
              className="text-muted-foreground size-3.5 animate-spin"
              data-testid="search-busy"
            />
          ) : null}
          {draft !== '' ? (
            <button
              aria-label="Clear search"
              className="text-muted-foreground hover:text-foreground focus-visible:ring-ring/50 inline-flex size-6 items-center justify-center rounded outline-none focus-visible:ring-[3px]"
              onClick={clear}
              type="button"
            >
              <X aria-hidden="true" className="size-3.5" />
            </button>
          ) : null}
        </div>
      </div>
      {description === undefined ? null : (
        // Polite: the too-short hint is announced as it appears. From 1600 px the
        // line takes no room on the toolbar (D26): the hint is the tooltip, and
        // the too-short hint floats under the box, on one line.
        <p
          aria-live="polite"
          className={cn(
            'text-muted-foreground text-xs',
            tooShort
              ? 'min-[1600px]:bg-popover min-[1600px]:absolute min-[1600px]:top-full min-[1600px]:left-0 min-[1600px]:z-10 min-[1600px]:mt-1 min-[1600px]:rounded-md min-[1600px]:border min-[1600px]:px-2 min-[1600px]:py-0.5 min-[1600px]:whitespace-nowrap min-[1600px]:shadow-sm'
              : 'min-[1600px]:sr-only',
          )}
          id={descriptionId}
        >
          {description}
        </p>
      )}
    </div>
  )
}

/** A marked match: legible on both themes, no extra width. */
export const MARK_CLASS = 'bg-warning/30 text-foreground rounded-[2px]'

/** `text` with every occurrence of the search term in a `<mark>` (D20). */
export function HighlightedText({ text, q }: { text: string; q: string | null | undefined }) {
  return (
    <>
      {highlightMatches(text, q).map((segment, index) =>
        segment.match ? (
          <mark key={index} className={MARK_CLASS}>
            {segment.text}
          </mark>
        ) : (
          segment.text
        ),
      )}
    </>
  )
}
