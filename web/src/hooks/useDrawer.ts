import { useContext } from 'react'
import { DrawerContext, type DrawerContextValue } from './DrawerContext'

export function useDrawer(): DrawerContextValue {
  const ctx = useContext(DrawerContext)
  if (!ctx) throw new Error('useDrawer must be used inside DrawerProvider')
  return ctx
}
