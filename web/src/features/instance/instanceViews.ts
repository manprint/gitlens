import type { ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'

/**
 * The query parameter of a tab's own views (15.3): one table per view. It is
 * distinct from `section`, `db`, `range` and the table prefixes.
 */
export const INSTANCE_VIEW_PARAM = 'view'

export interface InstanceView<Id extends string = string> {
  id: Id
  label: ReactNode
}

/** The view a `?view=` value names; absent, unknown or unavailable values mean the first view. */
export function readInstanceView<Id extends string>(
  views: readonly { id: Id }[],
  value: string | null,
): Id | undefined {
  return views.find((view) => view.id === value)?.id ?? views[0]?.id
}

/**
 * The active view of a tab and its setter. The first view is the default and is
 * never written to the URL; a change replaces the history entry, like `section`.
 */
export function useInstanceView<Id extends string>(
  views: readonly { id: Id }[],
): [Id | undefined, (next: string) => void] {
  const [searchParams, setSearchParams] = useSearchParams()
  const view = readInstanceView(views, searchParams.get(INSTANCE_VIEW_PARAM))

  function showView(next: string) {
    setSearchParams(
      (current) => {
        const params = new URLSearchParams(current)
        if (next === views[0]?.id) params.delete(INSTANCE_VIEW_PARAM)
        else params.set(INSTANCE_VIEW_PARAM, next)
        return params
      },
      { replace: true },
    )
  }

  return [view, showView]
}
