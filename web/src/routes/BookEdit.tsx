import { useCallback, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { BookInput } from '../schemas/book'
import { diffInput } from '../lib/bookForm'
import { useToast } from '../hooks/useToast'
import { BookForm } from '../components/BookForm'
import { ErrorState, Loading } from '../components/States'

/** /books/new and /books/:id/edit share one form. */
export function BookEdit() {
  const { id } = useParams()
  const isNew = !id
  const navigate = useNavigate()
  const qc = useQueryClient()
  const toast = useToast()
  const [error, setError] = useState<unknown>(null)

  const detail = useQuery({ queryKey: ['book', id ?? ''] as const, queryFn: api.book, enabled: !isNew })

  const save = useMutation({
    mutationFn: async (input: BookInput) => {
      if (isNew) return (await api.createBook(input)).book
      const current = detail.data?.book
      const patch = current ? diffInput(current, input) : input
      if (Object.keys(patch).length === 0) return current!
      return (await api.updateBook(id, patch)).book
    },
    onSuccess: async (book) => {
      setError(null)
      await Promise.all([qc.invalidateQueries({ queryKey: ['books'] }), qc.invalidateQueries({ queryKey: ['genres'] }), qc.invalidateQueries({ queryKey: ['book', book.id] })])
      toast.push('success', isNew ? `Added “${book.title}”` : 'Saved')
      navigate(`/books/${encodeURIComponent(book.id)}`)
    },
    onError: (err) => setError(err),
  })

  const onSubmit = useCallback(
    async (input: BookInput) => {
      try {
        await save.mutateAsync(input)
      } catch {
        /* shown under the form */
      }
    },
    [save],
  )

  if (!isNew && detail.isPending) return <Loading label="Loading the book…" />
  if (!isNew && detail.isError) return <ErrorState error={detail.error} onRetry={() => void detail.refetch()} />

  const cancelTo = isNew ? '/' : `/books/${encodeURIComponent(id)}`
  return (
    <section className="page-section">
      <nav className="crumbs" aria-label="Breadcrumb">
        <Link to="/">Catalog</Link>
        <span aria-hidden="true">/</span>
        {!isNew && detail.data && (
          <>
            <Link to={cancelTo}>{detail.data.book.title}</Link>
            <span aria-hidden="true">/</span>
          </>
        )}
        <span>{isNew ? 'New book' : 'Edit'}</span>
      </nav>
      <h1 className="display">{isNew ? 'Add a book' : `Edit “${detail.data?.book.title ?? ''}”`}</h1>
      <BookForm key={id ?? 'new'} initial={detail.data?.book} onSubmit={onSubmit} submitting={save.isPending} error={error} cancelTo={cancelTo} />
    </section>
  )
}
