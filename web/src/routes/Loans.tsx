import { useCallback, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { keepPreviousData, useMutation, useQuery } from '@tanstack/react-query'
import { LOAN_PAGE_SIZE, api, errorMessage } from '../api/client'
import { loanStatusSchema, type Loan, type LoanListParams, type LoanStatus } from '../schemas/loan'
import { useAuth } from '../hooks/useAuth'
import { useToast } from '../hooks/useToast'
import { useLibraryInvalidation } from '../hooks/useLibraryInvalidation'
import { LoanRow } from '../components/LoanRow'
import { Pagination } from '../components/Pagination'
import { Empty, ErrorState, Loading } from '../components/States'

const STATUS_CHIPS: { value: LoanStatus | ''; label: string }[] = [
  { value: 'open', label: 'Open' },
  { value: 'overdue', label: 'Overdue' },
  { value: 'returned', label: 'Returned' },
  { value: '', label: 'All' },
]

export function Loans() {
  const { can } = useAuth()
  return can('librarian') ? <AllLoans /> : <MyLoans />
}

function useReturn() {
  const toast = useToast()
  const invalidate = useLibraryInvalidation()
  return useMutation({
    mutationFn: (loan: Loan) => api.returnLoan(loan.id),
    onSuccess: async (_res, loan) => {
      toast.push('success', `Returned “${loan.book?.title ?? 'book'}”`)
      await invalidate(loan.bookId)
    },
    onError: (err) => toast.push('error', errorMessage(err, 'Could not return')),
  })
}

function MyLoans() {
  const loans = useQuery({ queryKey: ['me', 'loans'] as const, queryFn: api.myLoans })
  const holds = useQuery({ queryKey: ['me', 'holds'] as const, queryFn: api.myHolds })
  const ret = useReturn()
  const toast = useToast()
  const invalidate = useLibraryInvalidation()
  const cancel = useMutation({
    mutationFn: (id: string) => api.cancelHold(id),
    onSuccess: async () => {
      toast.push('success', 'Hold cancelled')
      await invalidate()
    },
    onError: (err) => toast.push('error', errorMessage(err, 'Could not cancel')),
  })

  const { open, history } = useMemo(() => {
    const all = loans.data?.loans ?? []
    const open = all.filter((l) => !l.returnedAt).sort((a, b) => a.dueAt.localeCompare(b.dueAt))
    const history = all.filter((l) => !!l.returnedAt).sort((a, b) => (b.returnedAt ?? '').localeCompare(a.returnedAt ?? ''))
    return { open, history }
  }, [loans.data])
  const activeHolds = useMemo(() => (holds.data?.holds ?? []).filter((h) => h.status === 'waiting' || h.status === 'ready'), [holds.data])

  if (loans.isPending) return <Loading label="Fetching your loans…" />
  if (loans.isError) return <ErrorState error={loans.error} onRetry={() => void loans.refetch()} />

  return (
    <section className="page-section">
      <h1 className="display">My loans</h1>
      <h2>Out now</h2>
      {open.length === 0 ? (
        <Empty title="Nothing checked out">Browse the catalog and take something home.</Empty>
      ) : (
        <ul className="loan-list" data-testid="open-loans">
          {open.map((l) => (
            <LoanRow key={l.id} loan={l} onReturn={(loan) => ret.mutate(loan)} returning={ret.isPending} />
          ))}
        </ul>
      )}

      {activeHolds.length > 0 && (
        <>
          <h2>Holds</h2>
          <ul className="mini-list" data-testid="my-holds">
            {activeHolds.map((h) => (
              <li key={h.id} className="mini-list__item">
                <span className="mini-list__main">
                  <strong>{h.book?.title ?? h.bookId}</strong>
                  <span className="small muted"> · {h.status === 'ready' ? 'ready for pickup' : `#${h.position ?? '?'} in queue`}</span>
                </span>
                <span className={`badge ${h.status === 'ready' ? 'badge--ok' : ''}`}>{h.status}</span>
                <button type="button" className="btn btn--sm" disabled={cancel.isPending} onClick={() => cancel.mutate(h.id)}>
                  Cancel
                </button>
              </li>
            ))}
          </ul>
        </>
      )}

      <h2>History</h2>
      {history.length === 0 ? (
        <p className="muted">No returned loans yet.</p>
      ) : (
        <ul className="loan-list" data-testid="loan-history">
          {history.map((l) => (
            <LoanRow key={l.id} loan={l} />
          ))}
        </ul>
      )}
    </section>
  )
}

function readParams(sp: URLSearchParams): LoanListParams {
  const status = loanStatusSchema.safeParse(sp.get('status'))
  const page = Number(sp.get('page') ?? '0')
  return {
    status: sp.has('status') ? (status.success ? status.data : '') : 'open',
    userId: sp.get('userId') ?? '',
    page: Number.isInteger(page) && page > 0 ? page : 0,
  }
}

function AllLoans() {
  const [sp, setSp] = useSearchParams()
  const params = useMemo(() => readParams(sp), [sp])
  const [memberDraft, setMemberDraft] = useState(params.userId)
  const ret = useReturn()

  const update = useCallback(
    (patch: Partial<LoanListParams>) => {
      const next = { ...readParams(sp), ...patch }
      const out = new URLSearchParams()
      out.set('status', next.status)
      if (next.userId) out.set('userId', next.userId)
      if (next.page > 0) out.set('page', String(next.page))
      setSp(out, { replace: true })
    },
    [sp, setSp],
  )

  const loans = useQuery({ queryKey: ['loans', params] as const, queryFn: api.loans, placeholderData: keepPreviousData })
  const total = loans.data?.total ?? loans.data?.loans.length ?? 0

  return (
    <section className="page-section">
      <h1 className="display">Loans</h1>
      <div className="filters">
        <div className="chips" role="group" aria-label="Status">
          {STATUS_CHIPS.map((c) => (
            <button key={c.label} type="button" className={`chip ${params.status === c.value ? 'is-active' : ''}`} aria-pressed={params.status === c.value} onClick={() => update({ status: c.value, page: 0 })}>
              {c.label}
            </button>
          ))}
        </div>
        <form
          className="inline-form"
          onSubmit={(e) => {
            e.preventDefault()
            update({ userId: memberDraft.trim(), page: 0 })
          }}
        >
          <label htmlFor="loans-member">Member user id</label>
          <input id="loans-member" value={memberDraft} onChange={(e) => setMemberDraft(e.target.value)} placeholder="filter by member" autoComplete="off" />
          <button type="submit" className="btn btn--sm">
            Filter
          </button>
          {params.userId && (
            <button type="button" className="btn btn--sm btn--ghost" onClick={() => { setMemberDraft(''); update({ userId: '', page: 0 }) }}>
              Clear
            </button>
          )}
        </form>
      </div>

      <p className="muted" aria-live="polite">
        {loans.isPending ? 'Loading…' : `${total.toLocaleString('en-US')} ${total === 1 ? 'loan' : 'loans'}`}
      </p>

      {loans.isPending ? (
        <Loading />
      ) : loans.isError ? (
        <ErrorState error={loans.error} onRetry={() => void loans.refetch()} />
      ) : loans.data.loans.length === 0 ? (
        <Empty title="No loans match" />
      ) : (
        <>
          <ul className="loan-list" data-testid="loan-list">
            {loans.data.loans.map((l) => (
              <LoanRow
                key={l.id}
                loan={l}
                showMember
                onReturn={(loan) => ret.mutate(loan)}
                returning={ret.isPending}
                onMemberClick={(uid) => { setMemberDraft(uid); update({ userId: uid, page: 0 }) }}
              />
            ))}
          </ul>
          <Pagination page={params.page} pageSize={LOAN_PAGE_SIZE} total={total} onPage={(page) => update({ page })} />
        </>
      )}
    </section>
  )
}
