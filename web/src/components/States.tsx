import type { ReactNode } from 'react'
import { errorMessage } from '../api/client'

export function Loading({ label = 'Loading…' }: { label?: string }) {
  return (
    <div className="state state--loading" role="status" aria-live="polite">
      <span className="spinner" aria-hidden="true" />
      <span>{label}</span>
    </div>
  )
}

export function Empty({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="state state--empty">
      <p className="state__title">{title}</p>
      {children && <div className="state__body">{children}</div>}
    </div>
  )
}

export function ErrorState({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  return (
    <div className="state state--error" role="alert">
      <p className="state__title">{errorMessage(error)}</p>
      {onRetry && (
        <button type="button" className="btn btn--sm" onClick={onRetry}>
          Try again
        </button>
      )}
    </div>
  )
}

/** Skeleton placeholders for a card grid. */
export function GridSkeleton({ count = 8 }: { count?: number }) {
  return (
    <div className="book-grid" aria-hidden="true">
      {Array.from({ length: count }, (_, i) => (
        <div key={i} className="book-card book-card--skeleton">
          <div className="cover cover--card skeleton" />
          <div className="book-card__body">
            <div className="skeleton skeleton--line" />
            <div className="skeleton skeleton--line skeleton--short" />
          </div>
        </div>
      ))}
    </div>
  )
}
