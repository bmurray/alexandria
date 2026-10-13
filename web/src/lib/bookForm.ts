import type { Book, BookInput, BookPatch, Suggestion } from '../schemas/book'
import { joinTags, parseTags } from './tags'

/** The form keeps everything as strings; numbers are parsed on submit. */
export type BookFormState = {
  title: string
  author: string
  isbn: string
  year: string
  genre: string
  publisher: string
  copies: string
  description: string
  coverUrl: string
  tags: string
}

export const EMPTY_FORM: BookFormState = {
  title: '',
  author: '',
  isbn: '',
  year: '',
  genre: '',
  publisher: '',
  copies: '1',
  description: '',
  coverUrl: '',
  tags: '',
}

export function formFromBook(b: Book): BookFormState {
  return {
    title: b.title ?? '',
    author: b.author ?? '',
    isbn: b.isbn ?? '',
    year: b.year === null || b.year === undefined ? '' : String(b.year),
    genre: b.genre ?? '',
    publisher: b.publisher ?? '',
    copies: String(b.copies ?? 1),
    description: b.description ?? '',
    coverUrl: b.coverUrl ?? '',
    tags: joinTags(b.tags),
  }
}

/** Client-side checks; the server is the authority, these just save a round trip. */
export function validateForm(f: BookFormState): string[] {
  const problems: string[] = []
  if (!f.title.trim()) problems.push('Title is required')
  if (f.year.trim() && !/^-?\d{1,4}$/.test(f.year.trim())) problems.push('Year must be a whole number')
  const copies = Number(f.copies)
  if (!f.copies.trim() || !Number.isInteger(copies) || copies < 1) problems.push('Copies must be at least 1')
  if (f.coverUrl.trim() && !/^https?:\/\//i.test(f.coverUrl.trim())) problems.push('Cover URL must start with http:// or https://')
  return problems
}

export function inputFromForm(f: BookFormState): BookInput {
  return {
    title: f.title.trim(),
    author: f.author.trim(),
    isbn: f.isbn.trim(),
    year: f.year.trim() ? Number(f.year.trim()) : null,
    genre: f.genre.trim(),
    publisher: f.publisher.trim(),
    description: f.description.trim(),
    coverUrl: f.coverUrl.trim(),
    copies: Number(f.copies),
    tags: parseTags(f.tags),
  }
}

/** Only the fields that differ from the stored book, for a PATCH. */
export function diffInput(book: Book, input: BookInput): BookPatch {
  const patch: BookPatch = {}
  if (input.title !== (book.title ?? '')) patch.title = input.title
  if (input.author !== (book.author ?? '')) patch.author = input.author
  if (input.isbn !== (book.isbn ?? '')) patch.isbn = input.isbn
  if (input.year !== (book.year ?? null)) patch.year = input.year
  if (input.genre !== (book.genre ?? '')) patch.genre = input.genre
  if (input.publisher !== (book.publisher ?? '')) patch.publisher = input.publisher
  if (input.description !== (book.description ?? '')) patch.description = input.description
  if (input.coverUrl !== (book.coverUrl ?? '')) patch.coverUrl = input.coverUrl
  if (input.copies !== (book.copies ?? 1)) patch.copies = input.copies
  if (joinTags(input.tags) !== joinTags(book.tags)) patch.tags = input.tags
  return patch
}

export type SuggestionField = keyof BookFormState

export const FIELD_LABELS: Record<SuggestionField, string> = {
  title: 'Title',
  author: 'Author',
  isbn: 'ISBN',
  year: 'Year',
  genre: 'Genre',
  publisher: 'Publisher',
  copies: 'Copies',
  description: 'Description',
  coverUrl: 'Cover URL',
  tags: 'Tags',
}

/** Suggestion -> form-shaped strings, dropping empty values. */
export function suggestionToForm(s: Suggestion): Partial<BookFormState> {
  const out: Partial<BookFormState> = {}
  const str = (k: 'title' | 'author' | 'isbn' | 'genre' | 'publisher' | 'description' | 'coverUrl') => {
    const v = s[k]
    if (typeof v === 'string' && v.trim()) out[k] = v.trim()
  }
  str('title')
  str('author')
  str('isbn')
  str('genre')
  str('publisher')
  str('description')
  str('coverUrl')
  if (typeof s.year === 'number') out.year = String(s.year)
  if (Array.isArray(s.tags) && s.tags.length) out.tags = joinTags(parseTags(s.tags.join(',')))
  return out
}

/** Apply a suggestion: all fields, or only the ones that are empty now. */
export function applySuggestion(form: BookFormState, sugg: Partial<BookFormState>, onlyEmpty: boolean): BookFormState {
  const next = { ...form }
  for (const [k, v] of Object.entries(sugg) as [SuggestionField, string][]) {
    if (onlyEmpty && form[k].trim()) continue
    next[k] = v
  }
  return next
}
