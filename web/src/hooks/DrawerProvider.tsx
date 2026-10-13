import { useCallback, useMemo, useState, type ReactNode } from 'react'
import { DrawerContext, type DrawerContextValue } from './DrawerContext'

export function DrawerProvider({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState('')

  const toggle = useCallback(() => setOpen((o) => !o), [])
  const ask = useCallback((p: string) => {
    setDraft(p)
    setOpen(true)
  }, [])

  const value = useMemo<DrawerContextValue>(
    () => ({ open, setOpen, toggle, draft, setDraft, ask }),
    [open, toggle, draft, ask],
  )
  return <DrawerContext.Provider value={value}>{children}</DrawerContext.Provider>
}
