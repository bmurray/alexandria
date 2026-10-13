import { Link, Outlet } from 'react-router-dom'
import { useConfig } from '../hooks/useConfig'
import { TopBar } from './TopBar'
import { Toasts } from './Toasts'
import { LibrarianDrawer } from './LibrarianDrawer'

export function Layout() {
  const config = useConfig()
  const aiEnabled = config.data?.aiEnabled ?? false
  return (
    <div className="app">
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <TopBar />
      <main id="main" className="main">
        <Outlet />
      </main>
      <footer className="footer">
        <span>{config.data?.libraryName || 'The Library of Alexandria'}</span>
        <span className="muted">Loans run {config.data?.loanDays ?? 21} days. Return early, the next reader thanks you.</span>
        <nav className="footer__links" aria-label="Legal">
          <Link to="/tos">Terms</Link>
          <Link to="/privacy">Privacy</Link>
        </nav>
      </footer>
      <Toasts />
      {aiEnabled && <LibrarianDrawer />}
    </div>
  )
}
