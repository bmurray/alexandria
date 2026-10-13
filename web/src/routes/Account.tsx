import { useCallback } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { useToast } from '../hooks/useToast'
import { formatDate, initials, timeAgo } from '../lib/format'

export function Account() {
  const { user, logout, can } = useAuth()
  const navigate = useNavigate()
  const toast = useToast()
  const signOut = useCallback(async () => {
    await logout()
    toast.push('info', 'Signed out')
    navigate('/')
  }, [logout, navigate, toast])
  if (!user) return null
  return (
    <section className="page-section narrow">
      <h1 className="display">Your account</h1>
      <div className="panel profile" data-testid="profile">
        {user.picture ? <img className="avatar avatar--lg" src={user.picture} alt="" referrerPolicy="no-referrer" /> : <span className="avatar avatar--lg avatar--initials">{initials(user.name, user.email)}</span>}
        <div className="profile__body">
          <h2>{user.name || user.email}</h2>
          <p className="muted">{user.email}</p>
          <dl className="kv">
            <dt>Role</dt>
            <dd>
              <span className={`role-badge role-badge--${user.role}`}>{user.role}</span>
            </dd>
            <dt>Member since</dt>
            <dd>{formatDate(user.createdAt) || '—'}</dd>
            <dt>Last sign-in</dt>
            <dd>{timeAgo(user.lastLoginAt) || '—'}</dd>
            <dt>User id</dt>
            <dd>
              <code>{user.id}</code>
            </dd>
          </dl>
        </div>
      </div>
      <div className="row row--wrap">
        <Link to="/loans" className="btn">
          {can('librarian') ? 'Loans' : 'My loans'}
        </Link>
        <button type="button" className="btn btn--danger" onClick={() => void signOut()}>
          Sign out
        </button>
      </div>
    </section>
  )
}
