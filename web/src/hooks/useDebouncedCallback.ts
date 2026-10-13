import { useCallback, useEffect, useRef } from 'react'

/**
 * Returns a stable `debounce(fn)` that runs `fn` after `ms` of quiet. The
 * closure is passed at call time, so it is never stale; a pending call is
 * dropped on unmount.
 */
export function useDebouncedCallback(ms: number) {
  const timer = useRef<number | undefined>(undefined)
  useEffect(() => () => window.clearTimeout(timer.current), [])
  return useCallback(
    (fn: () => void) => {
      window.clearTimeout(timer.current)
      timer.current = window.setTimeout(fn, ms)
    },
    [ms],
  )
}
