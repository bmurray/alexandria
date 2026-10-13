import { describe, expect, test } from 'bun:test'
import { joinTags, parseTags } from './tags'

describe('tags', () => {
  test('splits, trims, drops empties', () => {
    expect(parseTags('sci-fi, Classic ,  , space opera')).toEqual(['sci-fi', 'Classic', 'space opera'])
  })
  test('dedupes case-insensitively keeping first spelling', () => {
    expect(parseTags('Epic, epic, EPIC, saga')).toEqual(['Epic', 'saga'])
  })
  test('accepts newlines and semicolons', () => {
    expect(parseTags('a;b\nc')).toEqual(['a', 'b', 'c'])
  })
  test('round trip', () => {
    expect(joinTags(parseTags('x, y'))).toBe('x, y')
    expect(joinTags(null)).toBe('')
  })
})
