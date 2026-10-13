import { useCallback, useMemo, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Role, Session } from '../schemas/auth'
import { hasRole } from '../lib/roles'
import { AuthContext, type AuthContextValue } from './AuthContext'

export function AuthProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient()
  const me = useQuery({ queryKey: ['auth', 'me'] as const, queryFn: api.me, staleTime: 60 * 1000, retry: false })

  const setSession = useCallback(
    (s: Session) => {
      qc.setQueryData(['auth', 'me'], s)
      // Everything role-scoped (loans, book detail loans) changes with the user.
      void qc.invalidateQueries({ predicate: (q) => q.queryKey[0] !== 'auth' && q.queryKey[0] !== 'config' })
    },
    [qc],
  )

  const refresh = useCallback(async () => {
    await qc.invalidateQueries({ queryKey: ['auth', 'me'] })
  }, [qc])

  const logoutMutation = useMutation({
    mutationFn: api.logout,
    onSettled: () => {
      qc.setQueryData(['auth', 'me'], null)
      qc.removeQueries({ predicate: (q) => q.queryKey[0] !== 'auth' && q.queryKey[0] !== 'config' })
    },
  })
  const logout = useCallback(async () => {
    await logoutMutation.mutateAsync()
  }, [logoutMutation])

  const user = me.data?.user ?? null
  const can = useCallback((min: Role) => hasRole(user?.role, min), [user?.role])

  const value = useMemo<AuthContextValue>(
    () => ({ user, isLoading: me.isPending, can, setSession, refresh, logout }),
    [user, me.isPending, can, setSession, refresh, logout],
  )
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
