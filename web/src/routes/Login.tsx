import { useCallback, useState, type FormEvent } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { useMutation } from '@tanstack/react-query'
import { api, errorMessage } from '../api/client'
import type { Role } from '../schemas/auth'
import { useAuth } from '../hooks/useAuth'
import { useConfig } from '../hooks/useConfig'
import { Loading } from '../components/States'
import { ROLES } from '../lib/roles'

function safeReturnTo(s: unknown): string {
  return typeof s === 'string' && s.startsWith('/') && !s.startsWith('//') ? s : '/'
}

export function Login() {
  const { user, isLoading, setSession } = useAuth()
  const config = useConfig()
  const location = useLocation()
  const returnTo = safeReturnTo((location.state as { from?: string } | null)?.from ?? new URLSearchParams(location.search).get('returnTo'))

  const [email, setEmail] = useState('')
  const [name, setName] = useState('')
  const [role, setRole] = useState<Role>('member')

  const dev = useMutation({
    mutationFn: () => api.devLogin({ email: email.trim(), name: name.trim(), role }),
    onSuccess: (s) => setSession(s),
  })
  const submitDev = useCallback(
    (e: FormEvent) => {
      e.preventDefault()
      if (!email.trim()) return
      dev.mutate()
    },
    [email, dev],
  )

  if (isLoading) return <Loading />
  if (user) return <Navigate to={returnTo} replace />

  const providers = config.data?.providers ?? []
  const oidc = providers.filter((p) => p.type !== 'dev')
  const hasDev = providers.some((p) => p.type === 'dev')

  return (
    <section className="auth">
      <div className="auth__card">
        <h1 className="display">Welcome back</h1>
        <p className="muted">Sign in to check out books, place holds and see your loans.</p>

        {config.isPending && <Loading label="Loading sign-in options…" />}
        {config.isError && <div className="alert alert--error" role="alert">{errorMessage(config.error)}</div>}

        {oidc.length > 0 && (
          <div className="stack">
            {oidc.map((p) => (
              <a key={p.name} className="btn btn--primary btn--block" href={api.oidcStartUrl(p.name, returnTo)} data-testid={`oidc-${p.name}`}>
                Continue with {p.displayName || p.name}
              </a>
            ))}
          </div>
        )}

        {oidc.length > 0 && hasDev && <div className="divider">or</div>}

        {hasDev && (
          <form className="stack" onSubmit={submitDev} data-testid="dev-login">
            <p className="small muted">Development sign-in. Any email works; the role you pick is the role you get.</p>
            <div className="field">
              <label htmlFor="dev-email">Email</label>
              <input id="dev-email" type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
            </div>
            <div className="field">
              <label htmlFor="dev-name">Name</label>
              <input id="dev-name" type="text" autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} />
            </div>
            <div className="field">
              <label htmlFor="dev-role">Role</label>
              <select id="dev-role" value={role} onChange={(e) => setRole(e.target.value as Role)}>
                {ROLES.map((r) => (
                  <option key={r} value={r}>
                    {r}
                  </option>
                ))}
              </select>
            </div>
            {dev.isError && (
              <div className="alert alert--error" role="alert">
                {errorMessage(dev.error, 'Sign-in failed')}
              </div>
            )}
            <button type="submit" className="btn btn--block" disabled={dev.isPending || !email.trim()}>
              {dev.isPending ? 'Signing in…' : 'Sign in (dev)'}
            </button>
          </form>
        )}

        {config.isSuccess && providers.length === 0 && (
          <div className="alert" role="alert">
            No sign-in method is configured. Browsing and the librarian chat still work without an account.
          </div>
        )}
      </div>
    </section>
  )
}
