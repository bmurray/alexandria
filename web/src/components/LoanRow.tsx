import { useMemo } from 'react'
import { Link } from 'react-router-dom'
import type { Loan } from '../schemas/loan'
import { dueLabel, formatDate, isOverdue } from '../lib/format'
import { Cover } from './Cover'

type Props = {
  loan: Loan
  showMember?: boolean
  onReturn?: (loan: Loan) => void
  returning?: boolean
  onMemberClick?: (userId: string) => void
}

/** One loan in a list: cover, title, member (librarians), dates, Return. */
export function LoanRow({ loan, showMember, onReturn, returning, onMemberClick }: Props) {
  const overdue = useMemo(() => loan.overdue ?? isOverdue(loan.dueAt, loan.returnedAt), [loan])
  const open = !loan.returnedAt
  const title = loan.book?.title ?? 'Untitled'
  return (
    <li className={`loan-row ${overdue ? 'is-overdue' : ''} ${open ? '' : 'is-returned'}`} data-testid="loan-row">
      <Cover title={title} author={loan.book?.author} coverUrl={loan.book?.coverUrl} size="thumb" />
      <div className="loan-row__main">
        <Link to={`/books/${encodeURIComponent(loan.bookId)}`} className="loan-row__title">
          {title}
        </Link>
        {loan.book?.author && <span className="loan-row__author">{loan.book.author}</span>}
        {showMember && loan.user && (
          <span className="loan-row__member">
            {onMemberClick ? (
              <button type="button" className="linklike" onClick={() => onMemberClick(loan.userId)}>
                {loan.user.name || loan.user.email || loan.userId}
              </button>
            ) : (
              loan.user.name || loan.user.email || loan.userId
            )}
          </span>
        )}
      </div>
      <div className="loan-row__dates">
        <span className="small muted">Out {formatDate(loan.checkedOutAt)}</span>
        {open ? (
          <span className={`loan-row__due ${overdue ? 'text-danger' : ''}`}>
            {dueLabel(loan.dueAt)}
            <span className="small muted"> · {formatDate(loan.dueAt)}</span>
          </span>
        ) : (
          <span className="small muted">Returned {formatDate(loan.returnedAt)}</span>
        )}
      </div>
      {open && onReturn && (
        <button type="button" className="btn btn--sm" disabled={returning} onClick={() => onReturn(loan)}>
          Return
        </button>
      )}
    </li>
  )
}
