import { useEffect, useRef, type FocusEvent } from 'react'

/**
 * Keeps keyboard focus on a control that moves to another place in the page
 * (15.3): page controls sit in a table's toolbar line while it has rows and in a
 * bar of their own otherwise, so the select that emptied or refilled the table
 * is unmounted and mounted again. Spread the handlers on a wrapper around the
 * controls; when `placement` changes and focus fell back to the page body, the
 * control last focused (by id) gets it back.
 */
export function useKeepFocus(placement: string) {
  const lastFocused = useRef<string | null>(null)

  useEffect(() => {
    const id = lastFocused.current
    if (id !== null && document.activeElement === document.body) {
      document.getElementById(id)?.focus()
    }
  }, [placement])

  return {
    onFocus: (event: FocusEvent<HTMLElement>) => {
      lastFocused.current = event.target.id || null
    },
    // Focus moved on to another element: nothing to restore.
    onBlur: (event: FocusEvent<HTMLElement>) => {
      if (event.relatedTarget !== null) lastFocused.current = null
    },
  }
}
