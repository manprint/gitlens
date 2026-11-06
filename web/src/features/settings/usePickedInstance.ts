import { useSearchParams } from 'react-router-dom'

export interface PickableInstance {
  instance_id: string
}

/**
 * One instance at a time (GAP-9): the URL param names it, the first instance
 * is the default and is never written, and an id the list no longer holds
 * falls back to the first.
 */
export function usePickedInstance<T extends PickableInstance>(
  param: string,
  instances: readonly T[],
): [T | undefined, (id: string) => void] {
  const [searchParams, setSearchParams] = useSearchParams()
  const wanted = searchParams.get(param)
  const picked = instances.find((instance) => instance.instance_id === wanted) ?? instances[0]

  function pick(id: string) {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current)
        if (id === instances[0]?.instance_id) next.delete(param)
        else next.set(param, id)
        return next
      },
      { replace: true },
    )
  }
  return [picked, pick]
}
