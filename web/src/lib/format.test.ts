import { describe, expect, test } from 'bun:test'
import { availabilityLabel, daysUntil, dueLabel, formatUsd, initials, isOverdue, plural, timeAgo } from './format'

const now = new Date(2026, 9, 13, 12, 0, 0) // 13 Oct 2026 noon, local time

describe('dueLabel', () => {
  test('future days', () => {
    expect(dueLabel(new Date(2026, 9, 16).toISOString(), now)).toBe('due in 3 days')
    expect(dueLabel(new Date(2026, 9, 14).toISOString(), now)).toBe('due tomorrow')
  })
  test('today regardless of time of day', () => {
    expect(dueLabel(new Date(2026, 9, 13, 23, 59).toISOString(), now)).toBe('due today')
  })
  test('overdue', () => {
    expect(dueLabel(new Date(2026, 9, 12).toISOString(), now)).toBe('1 day overdue')
    expect(dueLabel(new Date(2026, 9, 11).toISOString(), now)).toBe('2 days overdue')
  })
  test('invalid dates do not throw', () => {
    expect(daysUntil('nonsense', now)).toBe(0)
    expect(dueLabel('nonsense', now)).toBe('due today')
  })
})

describe('isOverdue', () => {
  test('returned loans are never overdue', () => {
    expect(isOverdue('2020-01-01T00:00:00Z', '2020-01-02T00:00:00Z', now)).toBe(false)
  })
  test('open loan past due', () => {
    expect(isOverdue('2020-01-01T00:00:00Z', null, now)).toBe(true)
    expect(isOverdue('2030-01-01T00:00:00Z', null, now)).toBe(false)
  })
})

describe('availabilityLabel', () => {
  test('copies available', () => {
    expect(availabilityLabel({ available: 3, copies: 3 })).toEqual({ text: '3 of 3 available', tone: 'ok' })
  })
  test('on loan with a queue', () => {
    expect(availabilityLabel({ available: 0, copies: 1, holdsWaiting: 2 })).toEqual({
      text: 'On loan · 2 holds waiting',
      tone: 'queue',
    })
  })
  test('on loan', () => {
    expect(availabilityLabel({ available: 0, copies: 1, holdsWaiting: 0 }).text).toBe('On loan')
  })
  test('tolerates missing fields', () => {
    expect(availabilityLabel({}).text).toBe('On loan')
  })
})

describe('formatUsd', () => {
  test('cents and sub-cent amounts', () => {
    expect(formatUsd(1.5)).toBe('$1.50')
    expect(formatUsd(0.0012)).toBe('$0.0012')
    expect(formatUsd(0)).toBe('$0.00')
    expect(formatUsd(null)).toBe('—')
  })
})

describe('timeAgo', () => {
  test('ranges', () => {
    expect(timeAgo(new Date(now.getTime() - 30_000).toISOString(), now)).toBe('just now')
    expect(timeAgo(new Date(now.getTime() - 5 * 60_000).toISOString(), now)).toBe('5 min ago')
    expect(timeAgo(new Date(now.getTime() - 3 * 3_600_000).toISOString(), now)).toBe('3 hours ago')
    expect(timeAgo(new Date(now.getTime() - 26 * 3_600_000).toISOString(), now)).toBe('yesterday')
  })
})

describe('plural + initials', () => {
  test('plural', () => {
    expect(plural(1, 'hold')).toBe('1 hold')
    expect(plural(2, 'copy', 'copies')).toBe('2 copies')
  })
  test('initials', () => {
    expect(initials('Ursula K. Le Guin')).toBe('UG')
    expect(initials('', 'brian@example.com')).toBe('B')
    expect(initials(null, null)).toBe('?')
  })
})
