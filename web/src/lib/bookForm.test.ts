import { describe, expect, test } from 'bun:test'
import type { Book } from '../schemas/book'
import { EMPTY_FORM, applySuggestion, diffInput, formFromBook, inputFromForm, suggestionToForm, validateForm } from './bookForm'

const book: Book = {
  id: 'b1',
  title: 'Dune',
  author: 'Frank Herbert',
  isbn: '9780441013593',
  year: 1965,
  genre: 'Science fiction',
  publisher: 'Chilton',
  description: 'Spice.',
  coverUrl: '',
  copies: 2,
  tags: ['classic', 'desert'],
}

describe('bookForm', () => {
  test('round trips a book', () => {
    const f = formFromBook(book)
    expect(f.year).toBe('1965')
    expect(f.tags).toBe('classic, desert')
    expect(inputFromForm(f)).toMatchObject({ title: 'Dune', year: 1965, copies: 2, tags: ['classic', 'desert'] })
  })
  test('diff only sends changes', () => {
    const f = { ...formFromBook(book), copies: '3', tags: 'classic, desert, ecology' }
    expect(diffInput(book, inputFromForm(f))).toEqual({ copies: 3, tags: ['classic', 'desert', 'ecology'] })
    expect(diffInput(book, inputFromForm(formFromBook(book)))).toEqual({})
  })
  test('validation', () => {
    expect(validateForm(EMPTY_FORM)).toEqual(['Title is required'])
    expect(validateForm({ ...EMPTY_FORM, title: 'x', year: 'abc', copies: '0' })).toEqual([
      'Year must be a whole number',
      'Copies must be at least 1',
    ])
    expect(validateForm({ ...EMPTY_FORM, title: 'x', coverUrl: 'ftp://x' })).toEqual([
      'Cover URL must start with http:// or https://',
    ])
  })
  test('suggestion apply respects onlyEmpty', () => {
    const sugg = suggestionToForm({ author: 'F. Herbert', year: 1965, tags: ['sf', 'sf', 'epic'], description: '' })
    expect(sugg).toEqual({ author: 'F. Herbert', year: '1965', tags: 'sf, epic' })
    const form = { ...EMPTY_FORM, author: 'Frank Herbert' }
    expect(applySuggestion(form, sugg, true).author).toBe('Frank Herbert')
    expect(applySuggestion(form, sugg, false).author).toBe('F. Herbert')
    expect(applySuggestion(form, sugg, true).year).toBe('1965')
  })
})
