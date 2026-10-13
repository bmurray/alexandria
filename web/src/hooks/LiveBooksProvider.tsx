import type { ReactNode } from 'react'
import { LiveContext } from './LiveContext'
import { useLiveBooks } from './useLiveBooks'

/** Mounts the single catalog socket for the whole app and shares its state. */
export function LiveBooksProvider({ children }: { children: ReactNode }) {
  const live = useLiveBooks()
  return <LiveContext.Provider value={live}>{children}</LiveContext.Provider>
}
