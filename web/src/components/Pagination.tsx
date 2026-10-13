type Props = { page: number; pageSize: number; total: number; onPage: (page: number) => void }

export function Pagination({ page, pageSize, total, onPage }: Props) {
  const pages = Math.max(1, Math.ceil(total / pageSize))
  if (pages <= 1) return null
  const from = page * pageSize + 1
  const to = Math.min(total, (page + 1) * pageSize)
  return (
    <nav className="pagination" aria-label="Pagination">
      <button type="button" className="btn btn--sm" disabled={page <= 0} onClick={() => onPage(page - 1)}>
        Previous
      </button>
      <span className="pagination__status">
        {from}–{to} of {total.toLocaleString('en-US')} · page {page + 1} of {pages}
      </span>
      <button type="button" className="btn btn--sm" disabled={page >= pages - 1} onClick={() => onPage(page + 1)}>
        Next
      </button>
    </nav>
  )
}
