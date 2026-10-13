import { Link } from 'react-router-dom'

export function NotFound() {
  return (
    <section className="page-section narrow">
      <h1 className="display">This page is not in the stacks</h1>
      <p className="muted">The link may be old, or the page may have moved.</p>
      <Link to="/" className="btn">
        Back to the catalog
      </Link>
    </section>
  )
}
