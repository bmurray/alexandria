import { createContext } from 'react'
import type { Role, Session, User } from '../schemas/auth'

export type AuthContextValue = {
  user: User | null
  isLoading: boolean
  /** True when the signed-in user has at least this role. */
  can: (min: Role) => boolean
  /** Store a freshly created session (after dev login). */
  setSession: (s: Session) => void
  refresh: () => Promise<void>
  logout: () => Promise<void>
}

export const AuthContext = createContext<AuthContextValue | null>(null)
