import { useCallback, useMemo, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { useMutation } from '@tanstack/react-query'
import { ApiError, api, errorMessage } from '../api/client'
import type { Book, BookInput, SuggestionResponse } from '../schemas/book'
import {
  EMPTY_FORM,
  FIELD_LABELS,
  applySuggestion,
  formFromBook,
  inputFromForm,
  suggestionToForm,
  validateForm,
  type BookFormState,
  type SuggestionField,
} from '../lib/bookForm'
import { isValidIsbn } from '../lib/isbn'
import { useConfig } from '../hooks/useConfig'
import { useToast } from '../hooks/useToast'
import { Cover } from './Cover'
import { SparkIcon } from './icons'

type Props = {
  initial?: Book
  onSubmit: (input: BookInput) => Promise<void>
  submitting: boolean
  error: unknown
  cancelTo: string
}

type Field = { key: SuggestionField; type?: 'text' | 'number' | 'url' | 'textarea'; hint?: string; width?: 'half' | 'full' }

const FIELDS: Field[] = [
  { key: 'title', width: 'full' },
  { key: 'author', width: 'half' },
  { key: 'isbn', width: 'half', hint: 'ISBN-10 or ISBN-13; hyphens are fine' },
  { key: 'year', type: 'number', width: 'half' },
  { key: 'genre', width: 'half' },
  { key: 'publisher', width: 'half' },
  { key: 'copies', type: 'number', width: 'half' },
  { key: 'description', type: 'textarea', width: 'full' },
  { key: 'coverUrl', type: 'url', width: 'full', hint: 'Open Library covers work well: https://covers.openlibrary.org/b/isbn/…-L.jpg' },
  { key: 'tags', width: 'full', hint: 'Comma separated, e.g. classic, space opera' },
]

export function BookForm({ initial, onSubmit, submitting, error, cancelTo }: Props) {
  const [form, setForm] = useState<BookFormState>(() => (initial ? formFromBook(initial) : EMPTY_FORM))
  const [clientProblems, setClientProblems] = useState<string[]>([])
  const [suggestion, setSuggestion] = useState<{ fields: Partial<BookFormState>; source: string; from: 'lookup' | 'enrich' } | null>(null)
  const config = useConfig()
  const toast = useToast()
  const aiEnabled = config.data?.aiEnabled ?? false

  const set = useCallback((key: SuggestionField, value: string) => {
    setForm((f) => ({ ...f, [key]: value }))
  }, [])

  const showSuggestion = useCallback(
    (res: SuggestionResponse, from: 'lookup' | 'enrich') => {
      const fields = suggestionToForm(res.suggestion ?? {})
      if (Object.keys(fields).length === 0) {
        toast.push('info', from === 'lookup' ? 'Open Library had nothing for that' : 'No suggestions came back')
        setSuggestion(null)
        return
      }
      setSuggestion({ fields, source: res.source ?? (from === 'lookup' ? 'openlibrary' : 'model'), from })
    },
    [toast],
  )

  const lookup = useMutation({
    mutationFn: () => {
      const isbn = form.isbn.trim()
      return isbn ? api.lookup({ isbn }) : api.lookup({ title: form.title.trim(), author: form.author.trim() || undefined })
    },
    onSuccess: (res) => showSuggestion(res, 'lookup'),
    onError: (err) => toast.push('error', errorMessage(err, 'Lookup failed')),
  })

  const enrich = useMutation({
    mutationFn: () =>
      api.enrich({ title: form.title.trim(), author: form.author.trim(), isbn: form.isbn.trim(), description: form.description.trim() }),
    onSuccess: (res) => showSuggestion(res, 'enrich'),
    onError: (err) => {
      if (err instanceof ApiError && err.status === 503) toast.push('error', 'No AI provider is available right now')
      else if (err instanceof ApiError && err.status === 429) toast.push('error', "Today's AI budget is used up")
      else toast.push('error', errorMessage(err, 'Enrich failed'))
    },
  })

  const canLookup = !!form.isbn.trim() || !!form.title.trim()
  const canEnrich = !!form.title.trim() || !!form.isbn.trim()
  const isbnWarning = form.isbn.trim() && !isValidIsbn(form.isbn) ? 'That does not look like a valid ISBN checksum' : null

  const applyField = useCallback((key: SuggestionField) => {
    setSuggestion((s) => {
      if (!s) return s
      const v = s.fields[key]
      if (v !== undefined) setForm((f) => ({ ...f, [key]: v }))
      const rest = { ...s.fields }
      delete rest[key]
      return Object.keys(rest).length ? { ...s, fields: rest } : null
    })
  }, [])

  const applyAll = useCallback(
    (onlyEmpty: boolean) => {
      if (!suggestion) return
      setForm((f) => applySuggestion(f, suggestion.fields, onlyEmpty))
      setSuggestion(null)
      toast.push('success', onlyEmpty ? 'Filled the empty fields' : 'Applied all suggestions')
    },
    [suggestion, toast],
  )

  const submit = useCallback(
    async (e: FormEvent) => {
      e.preventDefault()
      const problems = validateForm(form)
      setClientProblems(problems)
      if (problems.length) return
      await onSubmit(inputFromForm(form))
    },
    [form, onSubmit],
  )

  const serverProblems = useMemo(() => {
    if (!error) return []
    if (error instanceof ApiError) return error.problems.length ? error.problems : [error.message]
    return [errorMessage(error)]
  }, [error])
  const problems = clientProblems.length ? clientProblems : serverProblems

  const suggestionEntries = useMemo(
    () => (suggestion ? (Object.entries(suggestion.fields) as [SuggestionField, string][]) : []),
    [suggestion],
  )
  const hasEmptyTargets = suggestionEntries.some(([k]) => !form[k].trim())

  return (
    <form className="book-form" onSubmit={(e) => void submit(e)} noValidate>
      <div className="book-form__layout">
        <div className="book-form__preview">
          <Cover title={form.title || 'Untitled'} author={form.author} coverUrl={form.coverUrl} size="large" />
          <div className="book-form__tools">
            <button type="button" className="btn" onClick={() => lookup.mutate()} disabled={!canLookup || lookup.isPending}>
              {lookup.isPending ? 'Looking up…' : 'Look up'}
            </button>
            {aiEnabled && (
              <button type="button" className="btn btn--olive" onClick={() => enrich.mutate()} disabled={!canEnrich || enrich.isPending}>
                <SparkIcon /> {enrich.isPending ? 'Thinking…' : 'Enrich with AI'}
              </button>
            )}
            <p className="small muted">
              Look up asks Open Library by ISBN (or title and author). Enrich adds a description, genre and tags from the model.
              Nothing is applied until you say so.
            </p>
          </div>
        </div>

        <div className="book-form__fields">
          {FIELDS.map((f) => {
            const id = `book-${f.key}`
            const label = FIELD_LABELS[f.key]
            const hint = f.key === 'isbn' && isbnWarning ? isbnWarning : f.hint
            return (
              <div key={f.key} className={`field field--${f.width ?? 'full'}`}>
                <label htmlFor={id}>{label}</label>
                {f.type === 'textarea' ? (
                  <textarea id={id} name={f.key} rows={5} value={form[f.key]} onChange={(e) => set(f.key, e.target.value)} />
                ) : (
                  <input
                    id={id}
                    name={f.key}
                    type={f.type ?? 'text'}
                    inputMode={f.type === 'number' ? 'numeric' : undefined}
                    min={f.key === 'copies' ? 1 : undefined}
                    value={form[f.key]}
                    onChange={(e) => set(f.key, e.target.value)}
                    required={f.key === 'title'}
                  />
                )}
                {hint && (
                  <span className={`field__hint ${f.key === 'isbn' && isbnWarning ? 'text-warn' : ''}`} id={`${id}-hint`}>
                    {hint}
                  </span>
                )}
              </div>
            )
          })}
        </div>
      </div>

      {suggestion && (
        <section className="suggestion" aria-label="Suggestions" data-testid="suggestion">
          <header className="suggestion__head">
            <h2>
              {suggestion.from === 'lookup' ? 'From Open Library' : 'Suggested by the model'}
              <span className="badge">{suggestion.source}</span>
            </h2>
            <div className="row">
              {hasEmptyTargets && (
                <button type="button" className="btn btn--sm" onClick={() => applyAll(true)}>
                  Fill empty fields
                </button>
              )}
              <button type="button" className="btn btn--sm btn--primary" onClick={() => applyAll(false)}>
                Use all
              </button>
              <button type="button" className="btn btn--sm btn--ghost" onClick={() => setSuggestion(null)}>
                Dismiss
              </button>
            </div>
          </header>
          <ul className="suggestion__list">
            {suggestionEntries.map(([key, value]) => (
              <li key={key} className="suggestion__row">
                <div className="suggestion__field">{FIELD_LABELS[key]}</div>
                <div className="suggestion__values">
                  {form[key].trim() && <div className="suggestion__current muted">{form[key]}</div>}
                  <div className="suggestion__proposed">{value}</div>
                </div>
                <button type="button" className="btn btn--sm" onClick={() => applyField(key)} aria-label={`Use suggested ${FIELD_LABELS[key]}`}>
                  Use
                </button>
              </li>
            ))}
          </ul>
        </section>
      )}

      {problems.length > 0 && (
        <div className="alert alert--error" role="alert">
          <strong>Please fix the following:</strong>
          <ul>
            {problems.map((p) => (
              <li key={p}>{p}</li>
            ))}
          </ul>
        </div>
      )}

      <footer className="book-form__actions">
        <button type="submit" className="btn btn--primary" disabled={submitting}>
          {submitting ? 'Saving…' : 'Save book'}
        </button>
        <Link to={cancelTo} className="btn btn--ghost">
          Cancel
        </Link>
      </footer>
    </form>
  )
}
