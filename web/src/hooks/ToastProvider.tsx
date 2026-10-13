import { useCallback, useMemo, useRef, useState, type ReactNode } from 'react'
import { ToastContext, type Toast, type ToastContextValue, type ToastKind } from './ToastContext'

const TTL: Record<ToastKind, number> = { success: 3500, info: 4500, error: 7000 }

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])
  const seq = useRef(0)

  const dismiss = useCallback((id: number) => {
    setToasts((ts) => ts.filter((t) => t.id !== id))
  }, [])

  const push = useCallback(
    (kind: ToastKind, message: string) => {
      const id = ++seq.current
      setToasts((ts) => [...ts.slice(-3), { id, kind, message }])
      window.setTimeout(() => dismiss(id), TTL[kind])
    },
    [dismiss],
  )

  const value = useMemo<ToastContextValue>(() => ({ toasts, push, dismiss }), [toasts, push, dismiss])
  return <ToastContext.Provider value={value}>{children}</ToastContext.Provider>
}
