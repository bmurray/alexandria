import { useCallback, useMemo, useState, type FormEvent } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery } from '@tanstack/react-query'
import { ApiError, api, errorMessage } from '../api/client'
import type { Hold, Loan } from '../schemas/loan'
import { useAuth } from '../hooks/useAuth'
import { useConfig } from '../hooks/useConfig'
import { useDrawer } from '../hooks/useDrawer'
import { useToast } from '../hooks/useToast'
import { useLibraryInvalidation } from '../hooks/useLibraryInvalidation'
import { dueLabel, formatDate, isOverdue, plural } from '../lib/format'
import { formatIsbn } from '../lib/isbn'
import { AvailabilityPill } from '../components/AvailabilityPill'
import { ConfirmButton } from '../components/ConfirmButton'
import { Cover } from '../components/Cover'
import { ErrorState, Loading } from '../components/States'
import { SparkIcon } from '../components/icons'

export function Book() {
  const { id = '' } = useParams()
  const { user, can } = useAuth()
  const config = useConfig()
  const toast = useToast()
  const drawer = useDrawer()
  const navigate = useNavigate()
  const location = useLocation()
  const invalidate = useLibraryInvalidation()
  const [memberId, setMemberId] = useState('')

  const detail = useQuery({ queryKey: ['book', id] as const, queryFn: api.book, enabled: !!id })

  const onError = useCallback((err: unknown, fallback: string) => toast.push('error', errorMessage(err, fallback)), [toast])
  const done = useCallback(
    async (msg: string) => {
      toast.push('success', msg)
      await invalidate(id)
    },
    [toast, invalidate, id],
  )

  const checkout = useMutation({
    mutationFn: (userId?: string) => api.checkout(id, userId),
    onSuccess: (res, userId) => {
      void done(userId ? `Checked out for ${res.loan.user?.name || userId} · ${dueLabel(res.loan.dueAt)}` : `Checked out · ${dueLabel(res.loan.dueAt)}`)
      setMemberId('')
    },
    onError: (err) => onError(err, 'Could not check out'),
  })
  const returnLoan = useMutation({
    mutationFn: (loanId: string) => api.returnLoan(loanId),
    onSuccess: () => void done('Returned. Thank you!'),
    onError: (err) => onError(err, 'Could not return'),
  })
  const placeHold = useMutation({
    mutationFn: () => api.placeHold(id),
    onSuccess: (res) => void done(res.hold.position ? `Hold placed · you are #${res.hold.position} in the queue` : 'Hold placed'),
    onError: (err) => onError(err, 'Could not place a hold'),
  })
  const cancelHold = useMutation({
    mutationFn: (holdId: string) => api.cancelHold(holdId),
    onSuccess: () => void done('Hold cancelled'),
    onError: (err) => onError(err, 'Could not cancel the hold'),
  })
  const remove = useMutation({
    mutationFn: () => api.deleteBook(id),
    onSuccess: async () => {
      toast.push('success', 'Book deleted')
      await invalidate()
      navigate('/')
    },
    onError: (err) => {
      if (err instanceof ApiError && err.status === 409) toast.push('error', 'This book is on loan; return it before deleting')
      else onError(err, 'Could not delete')
    },
  })

  const book = detail.data?.book
  const loans = useMemo(() => (detail.data?.loans ?? []).filter((l) => !l.returnedAt), [detail.data])
  const holds = useMemo(() => (detail.data?.holds ?? []).filter((h) => h.status === 'waiting' || h.status === 'ready'), [detail.data])
  const myLoan = useMemo(() => (user ? loans.find((l) => l.userId === user.id) : undefined), [loans, user])
  const myHold = useMemo(() => (user ? holds.find((h) => h.userId === user.id) : undefined), [holds, user])

  const checkoutForMember = useCallback(
    (e: FormEvent) => {
      e.preventDefault()
      const uid = memberId.trim()
      if (uid) checkout.mutate(uid)
    },
    [memberId, checkout],
  )

  if (!id) return <ErrorState error={new Error('No book id')} />
  if (detail.isPending) return <Loading label="Opening the book…" />
  if (detail.isError) {
    if (detail.error instanceof ApiError && detail.error.status === 404) {
      return (
        <section className="page-section narrow">
          <h1 className="display">That book is not on our shelves</h1>
          <p className="muted">It may have been removed. </p>
          <Link to="/" className="btn">
            Back to the catalog
          </Link>
        </section>
      )
    }
    return <ErrorState error={detail.error} onRetry={() => void detail.refetch()} />
  }
  if (!book) return null

  const available = book.available ?? 0
  const busy = checkout.isPending || returnLoan.isPending || placeHold.isPending || cancelHold.isPending
  const isLibrarian = can('librarian')
  const here = location.pathname

  return (
    <article className="book-page" data-testid="book-page">
      <nav className="crumbs" aria-label="Breadcrumb">
        <Link to="/">Catalog</Link>
        <span aria-hidden="true">/</span>
        <span>{book.title}</span>
      </nav>

      <div className="book-page__layout">
        <div className="book-page__cover">
          <Cover title={book.title} author={book.author} coverUrl={book.coverUrl} size="large" />
        </div>

        <div className="book-page__main">
          <header className="book-page__head">
            <h1 className="display" data-testid="book-title">{book.title}</h1>
            {book.author && <p className="book-page__author">by {book.author}</p>}
            <div className="row row--wrap">
              <AvailabilityPill available={book.available} copies={book.copies} holdsWaiting={book.holdsWaiting} />
              {book.genre && <span className="badge">{book.genre}</span>}
              {book.year ? <span className="muted">{book.year}</span> : null}
            </div>
          </header>

          <section className="actions" aria-label="Actions">
            {!user && (
              <p className="muted">
                <Link to="/login" state={{ from: here }} className="btn btn--primary">
                  Sign in to check out
                </Link>
              </p>
            )}
            {user && (
              <div className="row row--wrap">
                {myLoan ? (
                  <>
                    <span className={`note ${isOverdue(myLoan.dueAt, myLoan.returnedAt) ? 'text-danger' : ''}`}>
                      You have this one · {dueLabel(myLoan.dueAt)}
                    </span>
                    <button type="button" className="btn btn--primary" disabled={busy} onClick={() => returnLoan.mutate(myLoan.id)}>
                      Return
                    </button>
                  </>
                ) : myHold ? (
                  <>
                    <span className="note">
                      {myHold.status === 'ready' ? 'Your hold is ready for pickup' : `You are #${myHold.position ?? '?'} in the hold queue`}
                    </span>
                    {myHold.status === 'ready' && available > 0 && (
                      <button type="button" className="btn btn--primary" disabled={busy} onClick={() => checkout.mutate(undefined)}>
                        Check out
                      </button>
                    )}
                    <button type="button" className="btn" disabled={busy} onClick={() => cancelHold.mutate(myHold.id)}>
                      Cancel hold
                    </button>
                  </>
                ) : available > 0 ? (
                  <button type="button" className="btn btn--primary" disabled={busy} onClick={() => checkout.mutate(undefined)}>
                    Check out
                  </button>
                ) : (
                  <button type="button" className="btn btn--primary" disabled={busy} onClick={() => placeHold.mutate()}>
                    Place a hold
                  </button>
                )}
                {config.data?.aiEnabled && (
                  <button type="button" className="btn btn--ghost" onClick={() => drawer.ask(`Tell me about "${book.title}"${book.author ? ` by ${book.author}` : ''}. Is it available?`)}>
                    <SparkIcon /> Ask the Librarian
                  </button>
                )}
              </div>
            )}
            {!user && config.data?.aiEnabled && (
              <button type="button" className="btn btn--ghost" onClick={() => drawer.ask(`Tell me about "${book.title}"${book.author ? ` by ${book.author}` : ''}.`)}>
                <SparkIcon /> Ask the Librarian
              </button>
            )}

            {isLibrarian && (
              <div className="librarian-tools">
                <div className="row row--wrap">
                  <Link to={`/books/${encodeURIComponent(id)}/edit`} className="btn">
                    Edit
                  </Link>
                  <ConfirmButton
                    question="Delete this book?"
                    confirmLabel="Delete"
                    onConfirm={() => remove.mutate()}
                    disabled={remove.isPending}
                  >
                    Delete
                  </ConfirmButton>
                </div>
                <form className="inline-form" onSubmit={checkoutForMember}>
                  <label htmlFor="member-user-id">Member user id</label>
                  <input id="member-user-id" value={memberId} onChange={(e) => setMemberId(e.target.value)} placeholder="e.g. 7k3m…" autoComplete="off" />
                  <button type="submit" className="btn btn--sm" disabled={!memberId.trim() || busy || available <= 0}>
                    Check out for member
                  </button>
                </form>
              </div>
            )}
          </section>

          {book.description && (
            <section className="book-page__desc">
              <h2>About this book</h2>
              {book.description.split(/\n{2,}/).map((p, i) => (
                <p key={i}>{p}</p>
              ))}
            </section>
          )}

          <section className="book-page__meta">
            <h2>Details</h2>
            <dl className="kv">
              <dt>Author</dt>
              <dd>{book.author || '—'}</dd>
              <dt>Year</dt>
              <dd>{book.year ?? '—'}</dd>
              <dt>Genre</dt>
              <dd>{book.genre || '—'}</dd>
              <dt>Publisher</dt>
              <dd>{book.publisher || '—'}</dd>
              <dt>ISBN</dt>
              <dd>{book.isbn ? <code>{formatIsbn(book.isbn)}</code> : '—'}</dd>
              <dt>Copies</dt>
              <dd>
                {book.copies ?? 0} total · {available} available · {book.onLoan ?? 0} on loan
                {(book.holdsWaiting ?? 0) > 0 ? ` · ${plural(book.holdsWaiting ?? 0, 'hold')} waiting` : ''}
              </dd>
              <dt>Added</dt>
              <dd>{formatDate(book.createdAt) || '—'}</dd>
            </dl>
            {book.tags && book.tags.length > 0 && (
              <ul className="tags" aria-label="Tags">
                {book.tags.map((t) => (
                  <li key={t}>
                    <Link to={`/?q=${encodeURIComponent(t)}`} className="tag">
                      {t}
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </section>

          {user && (loans.length > 0 || isLibrarian) && (
            <section className="book-page__loans">
              <h2>{isLibrarian ? 'Open loans' : 'Your loans'}</h2>
              {loans.length === 0 ? (
                <p className="muted">No open loans.</p>
              ) : (
                <ul className="mini-list" data-testid="book-loans">
                  {loans.map((l: Loan) => (
                    <li key={l.id} className={`mini-list__item ${isOverdue(l.dueAt, l.returnedAt) ? 'is-overdue' : ''}`}>
                      <span className="mini-list__main">
                        {isLibrarian ? <strong>{l.user?.name || l.user?.email || l.userId}</strong> : <strong>You</strong>}
                        <span className="small muted"> · out {formatDate(l.checkedOutAt)}</span>
                      </span>
                      <span className={isOverdue(l.dueAt, l.returnedAt) ? 'text-danger' : ''}>{dueLabel(l.dueAt)}</span>
                      {(isLibrarian || l.userId === user.id) && (
                        <button type="button" className="btn btn--sm" disabled={busy} onClick={() => returnLoan.mutate(l.id)}>
                          Return
                        </button>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </section>
          )}

          {user && (holds.length > 0 || isLibrarian) && (
            <section className="book-page__holds">
              <h2>Hold queue</h2>
              {holds.length === 0 ? (
                <p className="muted">Nobody is waiting.</p>
              ) : (
                <ol className="mini-list" data-testid="book-holds">
                  {holds.map((h: Hold) => (
                    <li key={h.id} className="mini-list__item">
                      <span className="mini-list__main">
                        <strong>
                          {h.position ? `#${h.position} ` : ''}
                          {h.userId === user.id ? 'You' : h.user?.name || h.user?.email || h.userId}
                        </strong>
                        <span className="small muted"> · placed {formatDate(h.placedAt)}</span>
                      </span>
                      <span className={`badge ${h.status === 'ready' ? 'badge--ok' : ''}`}>{h.status}</span>
                      {(isLibrarian || h.userId === user.id) && (
                        <button type="button" className="btn btn--sm" disabled={busy} onClick={() => cancelHold.mutate(h.id)}>
                          Cancel
                        </button>
                      )}
                    </li>
                  ))}
                </ol>
              )}
            </section>
          )}
        </div>
      </div>
    </article>
  )
}
