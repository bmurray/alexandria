import { useMemo } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api, errorMessage } from '../api/client'
import type { Hold } from '../schemas/loan'
import { useToast } from '../hooks/useToast'
import { useLibraryInvalidation } from '../hooks/useLibraryInvalidation'
import { formatDate } from '../lib/format'
import { Cover } from '../components/Cover'
import { Empty, ErrorState, Loading } from '../components/States'

export function Holds() {
  const holds = useQuery({ queryKey: ['holds', ''] as const, queryFn: api.holds })
  const toast = useToast()
  const invalidate = useLibraryInvalidation()
  const cancel = useMutation({
    mutationFn: (h: Hold) => api.cancelHold(h.id),
    onSuccess: async (_r, h) => {
      toast.push('success', 'Hold cancelled')
      await invalidate(h.bookId)
    },
    onError: (err) => toast.push('error', errorMessage(err, 'Could not cancel')),
  })

  const groups = useMemo(() => {
    const active = (holds.data?.holds ?? []).filter((h) => h.status === 'waiting' || h.status === 'ready')
    const byBook = new Map<string, Hold[]>()
    for (const h of active) {
      const list = byBook.get(h.bookId) ?? []
      list.push(h)
      byBook.set(h.bookId, list)
    }
    return [...byBook.entries()]
      .map(([bookId, list]) => ({ bookId, book: list[0]?.book, holds: list.sort((a, b) => (a.position ?? 0) - (b.position ?? 0)) }))
      .sort((a, b) => (a.book?.title ?? '').localeCompare(b.book?.title ?? ''))
  }, [holds.data])

  if (holds.isPending) return <Loading label="Checking the hold shelf…" />
  if (holds.isError) return <ErrorState error={holds.error} onRetry={() => void holds.refetch()} />

  return (
    <section className="page-section">
      <h1 className="display">Holds</h1>
      {groups.length === 0 ? (
        <Empty title="No active holds">When a member waits for a book, it shows up here.</Empty>
      ) : (
        <div className="hold-groups" data-testid="hold-groups">
          {groups.map((g) => (
            <section key={g.bookId} className="hold-group panel">
              <header className="hold-group__head">
                <Cover title={g.book?.title ?? 'Untitled'} author={g.book?.author} coverUrl={g.book?.coverUrl} size="thumb" />
                <div>
                  <h2>
                    <Link to={`/books/${encodeURIComponent(g.bookId)}`}>{g.book?.title ?? g.bookId}</Link>
                  </h2>
                  {g.book?.author && <p className="muted">{g.book.author}</p>}
                </div>
                <span className="badge">{g.holds.length} waiting</span>
              </header>
              <ol className="mini-list">
                {g.holds.map((h) => (
                  <li key={h.id} className="mini-list__item">
                    <span className="mini-list__main">
                      <strong>
                        {h.position ? `#${h.position} ` : ''}
                        {h.user?.name || h.user?.email || h.userId}
                      </strong>
                      <span className="small muted"> · placed {formatDate(h.placedAt)}</span>
                    </span>
                    <span className={`badge ${h.status === 'ready' ? 'badge--ok' : ''}`}>{h.status}</span>
                    <button type="button" className="btn btn--sm" disabled={cancel.isPending} onClick={() => cancel.mutate(h)}>
                      Cancel
                    </button>
                  </li>
                ))}
              </ol>
            </section>
          ))}
        </div>
      )}
    </section>
  )
}
