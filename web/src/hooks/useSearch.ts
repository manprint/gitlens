import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

/** The text in a search box and how the box reports what it passed on (D20). */
export interface SearchDraft {
  /** The text in the box, which can be ahead of the URL while the box waits. */
  draft: string
  setDraft: (draft: string) => void
  /** The box passed `value` on: when the URL takes it, the draft is not replaced. */
  emitted: (value: string) => void
}

interface DraftState {
  draft: string
  /** The URL value the draft last followed. */
  seen: string
  /** The values the box passed on that the URL has not taken yet, oldest first. */
  pending: readonly string[]
}

/**
 * The draft once the URL holds `value`. A value the box passed on keeps the
 * draft (the user may have typed on) and settles it and every older one; any
 * other value replaces the draft.
 */
function followUrl(state: DraftState, value: string): DraftState {
  const at = state.pending.indexOf(value)
  return at < 0
    ? { draft: value, seen: value, pending: [] }
    : { draft: state.draft, seen: value, pending: state.pending.slice(at + 1) }
}

/**
 * The draft of a search box, owned by the page so that it survives the box
 * moving between the table toolbar and a bar of its own (a remount). A new URL
 * value replaces the draft only when it is not the one the box itself passed
 * on: typing on while the URL takes the previous term keeps the newer text.
 */
export function useSearchDraft(value: string): SearchDraft {
  const [state, setState] = useState<DraftState>({ draft: value, seen: value, pending: [] })
  // Adjusting state while rendering (https://react.dev/learn/you-might-not-need-an-effect).
  const current = state.seen === value ? state : followUrl(state, value)
  if (current !== state) setState(current)
  return {
    draft: current.draft,
    setDraft: (draft) => setState((previous) => ({ ...previous, draft })),
    emitted: (next) =>
      setState((previous) => ({ ...previous, pending: [...previous.pending, next] })),
  }
}

/**
 * The term the shown results answer. While a new search loads, the page
 * keeps the results of the last search that succeeded (placeholder data):
 * their marks, counts and labels keep that search's term rather than claim
 * the new one, or one that failed (I-9).
 */
export function useShownTerm(term: string, placeholder: boolean, success: boolean): string {
  const [shown, setShown] = useState(term)
  if (!placeholder && success && shown !== term) {
    setShown(term)
    return term
  }
  return placeholder ? shown : term
}

/**
 * Moves an older search parameter of the page (`<table>.q`, the table search
 * the full search replaced) to `q`, so an older link still searches.
 */
export function useLegacySearchParam(legacyKey: string): void {
  const [searchParams, setSearchParams] = useSearchParams()
  const legacy = searchParams.get(legacyKey)
  useEffect(() => {
    if (legacy === null) return
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current)
        if (!next.has('q') && legacy.trim() !== '') next.set('q', legacy)
        next.delete(legacyKey)
        return next
      },
      { replace: true },
    )
  }, [legacy, legacyKey, setSearchParams])
}

/**
 * Reports a search hit's row as not in the list (D20) when the table that
 * would hold it is not drawn, e.g. the tab has no rows: the page then drops
 * `focus` from the URL and says so.
 */
export function useFocusMissing(
  focusKey: string | undefined,
  onFocusDone: ((found: boolean) => void) | undefined,
  noTable: boolean,
): void {
  useEffect(() => {
    if (focusKey !== undefined && noTable) onFocusDone?.(false)
  }, [focusKey, noTable, onFocusDone])
}
