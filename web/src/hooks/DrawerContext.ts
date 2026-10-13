import { createContext } from 'react'

export type DrawerContextValue = {
  open: boolean
  setOpen: (open: boolean) => void
  toggle: () => void
  /** The text in the composer; owned here so any page can pre-fill it. */
  draft: string
  setDraft: (draft: string) => void
  /** Open the drawer with a message pre-filled (from a book page, for instance). */
  ask: (prompt: string) => void
}

export const DrawerContext = createContext<DrawerContextValue | null>(null)
