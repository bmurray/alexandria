import { describe, expect, test } from 'bun:test'
import { hasRole } from './roles'

describe('hasRole', () => {
  test('ordering', () => {
    expect(hasRole('admin', 'member')).toBe(true)
    expect(hasRole('librarian', 'admin')).toBe(false)
    expect(hasRole('member', 'member')).toBe(true)
    expect(hasRole(null, 'member')).toBe(false)
  })
})
