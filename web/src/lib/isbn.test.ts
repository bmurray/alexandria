import { describe, expect, test } from 'bun:test'
import { formatIsbn, isValidIsbn, normalizeIsbn } from './isbn'

describe('isbn', () => {
  test('normalize strips separators and upcases X', () => {
    expect(normalizeIsbn('0-306-40615-2')).toBe('0306406152')
    expect(normalizeIsbn('0 8044 2957 x')).toBe('080442957X')
  })
  test('valid ISBN-10', () => {
    expect(isValidIsbn('0-306-40615-2')).toBe(true)
    expect(isValidIsbn('080442957X')).toBe(true)
    expect(isValidIsbn('0306406153')).toBe(false)
  })
  test('valid ISBN-13', () => {
    expect(isValidIsbn('978-0-306-40615-7')).toBe(true)
    expect(isValidIsbn('9780441013593')).toBe(true)
    expect(isValidIsbn('9780441013594')).toBe(false)
  })
  test('garbage', () => {
    expect(isValidIsbn('')).toBe(false)
    expect(isValidIsbn('dune')).toBe(false)
  })
  test('format', () => {
    expect(formatIsbn('9780441013593')).toBe('978-0-44101-359-3')
    expect(formatIsbn('0306406152')).toBe('0-30640-615-2')
    expect(formatIsbn('abc')).toBe('abc')
    expect(formatIsbn(null)).toBe('')
  })
})
