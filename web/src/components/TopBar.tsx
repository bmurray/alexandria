import { useCallback, useState } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { useConfig } from '../hooks/useConfig'
import { useToast } from '../hooks/useToast'
import { initials } from '../lib/format'
import { CloseIcon } from './icons'

export function TopBar() {
  const { user, isLoading, can, logout } = useAuth()
  const config = useConfig()
  const toast = useToast()
  const navigate = useNavigate()
  const location = useLocation()
  const [menuOpen, setMenuOpen] = useState(false)
  const [navOpen, setNavOpen] = useState(false)
  const libraryName = config.data?.libraryName || 'Alexandria'

  const closeAll = useCallback(() => {
    setMenuOpen(false)
    setNavOpen(false)
  }, [])

  const signOut = useCallback(async () => {
    closeAll()
    try {
      await logout()
      toast.push('info', 'Signed out')
      navigate('/')
    } catch {
      toast.push('error', 'Could not sign out')
    }
  }, [closeAll, logout, navigate, toast])

  const here = location.pathname + location.search

  return (
    <header className="topbar">
      <div className="topbar__inner">
        <Link to="/" className="brand" onClick={closeAll}>
          <span className="brand__glyph" aria-hidden="true">
            <svg viewBox="0 0 32 32" width="26" height="26">
              <rect width="32" height="32" rx="7" fill="currentColor" opacity="0.12" />
              <path d="M9 8h14v2.5H9zM9 21.5h14V24H9z" fill="currentColor" />
              <path d="M11 10.5h3v11h-3zM18 10.5h3v11h-3z" fill="currentColor" />
            </svg>
          </span>
          <span className="brand__name">{libraryName}</span>
        </Link>

        <button
          type="button"
          className="topbar__burger"
          aria-expanded={navOpen}
          aria-controls="main-nav"
          aria-label={navOpen ? 'Close menu' : 'Open menu'}
          onClick={() => setNavOpen((o) => !o)}
        >
          {navOpen ? <CloseIcon /> : <span className="burger" aria-hidden="true" />}
        </button>

        <nav id="main-nav" className={`nav ${navOpen ? 'is-open' : ''}`} aria-label="Main">
          <NavLink to="/" end onClick={closeAll}>
            Catalog
          </NavLink>
          {user && !can('librarian') && (
            <NavLink to="/loans" onClick={closeAll}>
              My loans
            </NavLink>
          )}
          {can('librarian') && (
            <NavLink to="/loans" onClick={closeAll}>
              Loans
            </NavLink>
          )}
          {can('librarian') && (
            <NavLink to="/dashboard" onClick={closeAll}>
              Dashboard
            </NavLink>
          )}
          {can('librarian') && (
            <NavLink to="/holds" onClick={closeAll}>
              Holds
            </NavLink>
          )}
          {can('admin') && (
            <NavLink to="/users" onClick={closeAll}>
              Members
            </NavLink>
          )}
          {can('admin') && (
            <NavLink to="/ai" onClick={closeAll}>
              AI spend
            </NavLink>
          )}
        </nav>

        <div className="topbar__user">
          {user ? (
            <div className={`user-menu ${menuOpen ? 'is-open' : ''}`}>
              <button
                type="button"
                className="user-menu__trigger"
                aria-haspopup="menu"
                aria-expanded={menuOpen}
                onClick={() => setMenuOpen((o) => !o)}
                data-testid="user-menu"
              >
                {user.picture ? (
                  <img className="avatar" src={user.picture} alt="" referrerPolicy="no-referrer" />
                ) : (
                  <span className="avatar avatar--initials" aria-hidden="true">
                    {initials(user.name, user.email)}
                  </span>
                )}
                <span className="user-menu__name">{user.name || user.email}</span>
                <span className={`role-badge role-badge--${user.role}`}>{user.role}</span>
              </button>
              {menuOpen && (
                <div className="user-menu__panel" role="menu">
                  <div className="user-menu__who">
                    <strong>{user.name}</strong>
                    <span className="muted small">{user.email}</span>
                  </div>
                  <Link to="/account" role="menuitem" onClick={closeAll}>
                    Account
                  </Link>
                  <Link to="/loans" role="menuitem" onClick={closeAll}>
                    {can('librarian') ? 'Loans' : 'My loans'}
                  </Link>
                  <button type="button" role="menuitem" onClick={() => void signOut()}>
                    Sign out
                  </button>
                </div>
              )}
            </div>
          ) : (
            !isLoading && (
              <Link to="/login" state={{ from: here }} className="btn btn--primary btn--sm">
                Sign in
              </Link>
            )
          )}
        </div>
      </div>
      {(menuOpen || navOpen) && <button type="button" className="scrim scrim--clear" aria-label="Close menu" onClick={closeAll} />}
    </header>
  )
}
