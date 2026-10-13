import { Link, Navigate, Outlet, useLocation } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import type { Role } from '../schemas/auth'
import { Loading } from './States'

/**
 * Route wrapper: anonymous visitors go to /login (remembering where they were);
 * signed-in users below `min` see a friendly 403.
 */
export function RequireRole({ min }: { min: Role }) {
  const { user, isLoading, can } = useAuth()
  const location = useLocation()
  if (isLoading) return <Loading />
  if (!user) return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />
  if (!can(min)) return <Forbidden min={min} />
  return <Outlet />
}

export function Forbidden({ min }: { min: Role }) {
  return (
    <section className="page-section narrow">
      <h1 className="display">Members only, past this shelf</h1>
      <p className="muted">
        You don&apos;t have permission to view this page: it is for{' '}
        {min === 'admin' ? 'administrators' : `${min}s`}. If you think you should have access, ask a librarian to
        update your role.
      </p>
      <p>
        <Link to="/" className="btn">
          Back to the catalog
        </Link>
      </p>
    </section>
  )
}
