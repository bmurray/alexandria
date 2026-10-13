import { Link } from 'react-router-dom'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api, errorMessage } from '../api/client'
import type { Loan } from '../schemas/loan'
import { useToast } from '../hooks/useToast'
import { useLibraryInvalidation } from '../hooks/useLibraryInvalidation'
import { formatDate, formatInt } from '../lib/format'
import { LoanRow } from '../components/LoanRow'
import { Empty, ErrorState, Loading } from '../components/States'

export function Dashboard() {
  const dash = useQuery({ queryKey: ['dashboard'] as const, queryFn: api.dashboard })
  const toast = useToast()
  const invalidate = useLibraryInvalidation()
  const ret = useMutation({
    mutationFn: (loan: Loan) => api.returnLoan(loan.id),
    onSuccess: async (_r, loan) => {
      toast.push('success', `Returned “${loan.book?.title ?? 'book'}”`)
      await invalidate(loan.bookId)
    },
    onError: (err) => toast.push('error', errorMessage(err, 'Could not return')),
  })

  if (dash.isPending) return <Loading label="Counting the shelves…" />
  if (dash.isError) return <ErrorState error={dash.error} onRetry={() => void dash.refetch()} />
  const d = dash.data

  const tiles: { label: string; value: number | null | undefined; to?: string; tone?: 'danger' | 'gold' }[] = [
    { label: 'Books', value: d.books, to: '/' },
    { label: 'Copies', value: d.copies },
    { label: 'Open loans', value: d.openLoans, to: '/loans?status=open' },
    { label: 'Overdue', value: d.overdue, to: '/loans?status=overdue', tone: 'danger' },
    { label: 'Holds waiting', value: d.holdsWaiting, to: '/holds', tone: 'gold' },
    { label: 'Members', value: d.members },
  ]

  return (
    <section className="page-section">
      <h1 className="display">Dashboard</h1>
      <div className="tiles" data-testid="stat-tiles">
        {tiles.map((t) => {
          const body = (
            <>
              <span className="tile__value">{formatInt(t.value)}</span>
              <span className="tile__label">{t.label}</span>
            </>
          )
          const cls = `tile ${t.tone ? `tile--${t.tone}` : ''} ${(t.value ?? 0) === 0 && t.tone ? 'tile--calm' : ''}`
          return t.to ? (
            <Link key={t.label} to={t.to} className={cls}>
              {body}
            </Link>
          ) : (
            <div key={t.label} className={cls}>
              {body}
            </div>
          )
        })}
      </div>

      <div className="dash-grid">
        <section className="panel">
          <h2>Overdue</h2>
          {(d.overdueLoans?.length ?? 0) === 0 ? (
            <Empty title="Nothing overdue. Lovely." />
          ) : (
            <ul className="loan-list" data-testid="overdue-list">
              {d.overdueLoans?.map((l) => (
                <LoanRow key={l.id} loan={l} showMember onReturn={(loan) => ret.mutate(loan)} returning={ret.isPending} />
              ))}
            </ul>
          )}
        </section>

        <section className="panel">
          <h2>Ready for pickup</h2>
          {(d.readyHolds?.length ?? 0) === 0 ? (
            <p className="muted">No holds are waiting at the desk.</p>
          ) : (
            <ul className="mini-list" data-testid="ready-holds">
              {d.readyHolds?.map((h) => (
                <li key={h.id} className="mini-list__item">
                  <span className="mini-list__main">
                    <Link to={`/books/${encodeURIComponent(h.bookId)}`}>
                      <strong>{h.book?.title ?? h.bookId}</strong>
                    </Link>
                    <span className="small muted">
                      {' '}
                      · {h.user?.name || h.user?.email || h.userId}
                      {h.readyAt ? ` · ready since ${formatDate(h.readyAt)}` : ''}
                    </span>
                  </span>
                  <span className="badge badge--ok">ready</span>
                </li>
              ))}
            </ul>
          )}
        </section>

        <section className="panel panel--wide">
          <h2>Recent loans</h2>
          {(d.recentLoans?.length ?? 0) === 0 ? (
            <p className="muted">No loans yet.</p>
          ) : (
            <ul className="loan-list" data-testid="recent-loans">
              {d.recentLoans?.map((l) => (
                <LoanRow key={l.id} loan={l} showMember onReturn={l.returnedAt ? undefined : (loan) => ret.mutate(loan)} returning={ret.isPending} />
              ))}
            </ul>
          )}
        </section>
      </div>
    </section>
  )
}
