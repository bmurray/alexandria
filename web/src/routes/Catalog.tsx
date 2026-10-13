import { useCallback, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { PAGE_SIZE, api } from '../api/client'
import { sortSchema, type BookListParams, type Sort } from '../schemas/book'
import { useAuth } from '../hooks/useAuth'
import { useLive } from '../hooks/useLive'
import { useDebouncedCallback } from '../hooks/useDebouncedCallback'
import { BookCard } from '../components/BookCard'
import { Pagination } from '../components/Pagination'
import { Empty, ErrorState, GridSkeleton } from '../components/States'
import { PlusIcon, SearchIcon } from '../components/icons'

function readParams(sp: URLSearchParams): BookListParams {
  const sort = sortSchema.safeParse(sp.get('sort'))
  const page = Number(sp.get('page') ?? '0')
  return {
    q: sp.get('q') ?? '',
    genre: sp.get('genre') ?? '',
    available: sp.get('available') === 'true',
    sort: sort.success ? sort.data : 'title',
    page: Number.isInteger(page) && page > 0 ? page : 0,
  }
}

export function Catalog() {
  const [sp, setSp] = useSearchParams()
  const params = useMemo(() => readParams(sp), [sp])
  const { can } = useAuth()
  const live = useLive()

  // The input is local (typing must not lag); the URL follows after a pause.
  const [draft, setDraft] = useState(params.q)
  const [urlQ, setUrlQ] = useState(params.q)
  if (params.q !== urlQ) {
    // The URL changed under us (back button, a nav link): adopt it.
    setUrlQ(params.q)
    setDraft(params.q)
  }

  const update = useCallback(
    (patch: Partial<BookListParams>) => {
      const next = { ...readParams(sp), ...patch }
      const out = new URLSearchParams()
      if (next.q) out.set('q', next.q)
      if (next.genre) out.set('genre', next.genre)
      if (next.available) out.set('available', 'true')
      if (next.sort !== 'title') out.set('sort', next.sort)
      if (next.page > 0) out.set('page', String(next.page))
      setSp(out, { replace: true })
    },
    [sp, setSp],
  )
  const debounce = useDebouncedCallback(300)
  const onType = useCallback(
    (q: string) => {
      setDraft(q)
      debounce(() => update({ q, page: 0 }))
    },
    [debounce, update],
  )

  const books = useQuery({ queryKey: ['books', params] as const, queryFn: api.books, placeholderData: keepPreviousData })
  const genres = useQuery({ queryKey: ['genres'] as const, queryFn: api.genres, staleTime: 60 * 1000 })

  const total = books.data?.total ?? books.data?.books.length ?? 0
  const hasFilters = !!params.q || !!params.genre || params.available

  return (
    <div className="catalog">
      <section className="hero">
        <div className="hero__text">
          <h1 className="display">Find your next book</h1>
          <p className="hero__lede">Search the shelves by title, author, ISBN or tag. Live availability, no card catalog required.</p>
        </div>
        <form
          className="search"
          role="search"
          onSubmit={(e) => {
            e.preventDefault()
            update({ q: draft, page: 0 })
          }}
        >
          <SearchIcon className="search__icon" size={20} />
          <input
            type="search"
            className="search__input"
            aria-label="Search the catalog"
            placeholder="Search by title, author, ISBN or tag"
            value={draft}
            onChange={(e) => onType(e.target.value)}
            autoComplete="off"
            enterKeyHint="search"
          />
          {draft && (
            <button type="button" className="search__clear" aria-label="Clear search" onClick={() => onType('')}>
              ×
            </button>
          )}
        </form>
      </section>

      <section className="filters" aria-label="Filters">
        <div className="chips" role="group" aria-label="Genre">
          <button type="button" className={`chip ${!params.genre ? 'is-active' : ''}`} onClick={() => update({ genre: '', page: 0 })} aria-pressed={!params.genre}>
            All genres
          </button>
          {genres.data?.genres.map((g) => (
            <button
              key={g.name}
              type="button"
              className={`chip ${params.genre === g.name ? 'is-active' : ''}`}
              aria-pressed={params.genre === g.name}
              onClick={() => update({ genre: params.genre === g.name ? '' : g.name, page: 0 })}
            >
              {g.name}
              {typeof g.count === 'number' && <span className="chip__count">{g.count}</span>}
            </button>
          ))}
        </div>
        <div className="filters__right">
          <label className="switch">
            <input type="checkbox" checked={params.available} onChange={(e) => update({ available: e.target.checked, page: 0 })} />
            <span className="switch__track" aria-hidden="true" />
            <span>Available now</span>
          </label>
          <label className="select-wrap">
            <span className="visually-hidden">Sort by</span>
            <select value={params.sort} onChange={(e) => update({ sort: e.target.value as Sort, page: 0 })} aria-label="Sort by">
              <option value="title">Title A–Z</option>
              <option value="author">Author A–Z</option>
              <option value="year">Year</option>
              <option value="added">Recently added</option>
            </select>
          </label>
          {can('librarian') && (
            <Link to="/books/new" className="btn btn--primary btn--sm">
              <PlusIcon /> Add book
            </Link>
          )}
        </div>
      </section>

      <div className="results-bar">
        <p className="results-bar__count" aria-live="polite" data-testid="result-count">
          {books.isPending ? 'Searching…' : `${total.toLocaleString('en-US')} ${total === 1 ? 'book' : 'books'}`}
          {params.q && !books.isPending ? <span className="muted"> for “{params.q}”</span> : null}
          {books.isFetching && !books.isPending ? <span className="muted"> · updating</span> : null}
        </p>
        <span className={`live ${live ? 'is-live' : ''}`} title={live ? 'Live updates connected' : 'Live updates reconnecting'} data-testid="live-indicator">
          <span className="live__dot" aria-hidden="true" />
          {live ? 'Live' : 'Offline'}
        </span>
      </div>

      {books.isPending ? (
        <GridSkeleton />
      ) : books.isError ? (
        <ErrorState error={books.error} onRetry={() => void books.refetch()} />
      ) : books.data.books.length === 0 ? (
        <Empty title={hasFilters ? 'Nothing on the shelves matches that' : 'The catalog is empty'}>
          {hasFilters ? (
            <button type="button" className="btn btn--sm" onClick={() => { setDraft(''); setSp(new URLSearchParams(), { replace: true }) }}>
              Clear search and filters
            </button>
          ) : can('librarian') ? (
            <Link to="/books/new" className="btn btn--sm btn--primary">
              Add the first book
            </Link>
          ) : null}
        </Empty>
      ) : (
        <>
          <div className="book-grid" data-testid="book-grid">
            {books.data.books.map((b) => (
              <BookCard key={b.id} book={b} />
            ))}
          </div>
          <Pagination page={params.page} pageSize={PAGE_SIZE} total={total} onPage={(page) => { update({ page }); window.scrollTo({ top: 0, behavior: 'smooth' }) }} />
        </>
      )}
    </div>
  )
}
