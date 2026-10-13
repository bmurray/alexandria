import { Link } from 'react-router-dom'
import type { Book } from '../schemas/book'
import { AvailabilityPill } from './AvailabilityPill'
import { Cover } from './Cover'

/**
 * One catalog result. The title is the link (so its accessible name is the
 * title); CSS stretches it over the whole card for pointer users.
 */
export function BookCard({ book }: { book: Book }) {
  return (
    <article className="book-card" data-testid="book-card">
      <Cover title={book.title} author={book.author} coverUrl={book.coverUrl} />
      <div className="book-card__body">
        <h3 className="book-card__title">
          <Link to={`/books/${encodeURIComponent(book.id)}`} className="book-card__link">
            {book.title}
          </Link>
        </h3>
        {book.author && <p className="book-card__author">{book.author}</p>}
        <p className="book-card__meta">
          {book.year ? <span>{book.year}</span> : null}
          {book.year && book.genre ? <span aria-hidden="true"> · </span> : null}
          {book.genre ? <span>{book.genre}</span> : null}
        </p>
        <AvailabilityPill available={book.available} copies={book.copies} holdsWaiting={book.holdsWaiting} />
      </div>
    </article>
  )
}
