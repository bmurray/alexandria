import { useEffect, useRef } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { Loading } from '../components/States'

/** Landing page after the OIDC redirect: the cookie is set; refresh and go back. */
export function AuthCallback() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const { refresh } = useAuth()
  const error = params.get('error')
  const returnTo = params.get('returnTo') ?? '/'
  const done = useRef(false)

  useEffect(() => {
    if (error || done.current) return
    done.current = true
    void refresh().then(() => navigate(returnTo.startsWith('/') && !returnTo.startsWith('//') ? returnTo : '/', { replace: true }))
  }, [error, navigate, refresh, returnTo])

  return (
    <section className="auth">
      <div className="auth__card">
        {error ? (
          <>
            <h1 className="display">Sign-in failed</h1>
            <div className="alert alert--error" role="alert">
              {error}
            </div>
            <p>
              <Link to="/login" className="btn">
                Try again
              </Link>
            </p>
          </>
        ) : (
          <>
            <h1 className="display">Signing you in…</h1>
            <Loading label="One moment." />
          </>
        )}
      </div>
    </section>
  )
}
