const DAY = 24 * 60 * 60 * 1000

/** Whole days from `now` to `iso`, rounded toward the calendar day boundary. */
export function daysUntil(iso: string, now: Date = new Date()): number {
  const target = new Date(iso)
  if (Number.isNaN(target.getTime())) return 0
  const startOf = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
  return Math.round((startOf(target) - startOf(now)) / DAY)
}

/** "due in 3 days", "due today", "2 days overdue". */
export function dueLabel(dueAt: string, now: Date = new Date()): string {
  const d = daysUntil(dueAt, now)
  if (d === 0) return 'due today'
  if (d === 1) return 'due tomorrow'
  if (d > 1) return `due in ${d} days`
  if (d === -1) return '1 day overdue'
  return `${-d} days overdue`
}

export function isOverdue(dueAt: string, returnedAt: string | null | undefined, now: Date = new Date()): boolean {
  if (returnedAt) return false
  const t = new Date(dueAt).getTime()
  return !Number.isNaN(t) && t < now.getTime()
}

/** "12 Oct 2026" */
export function formatDate(iso: string | null | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short', year: 'numeric' })
}

/** "3 minutes ago", "yesterday", or a date when older than a week. */
export function timeAgo(iso: string | null | undefined, now: Date = new Date()): string {
  if (!iso) return ''
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return iso
  const diff = now.getTime() - t
  const min = Math.floor(diff / 60000)
  if (min < 1) return 'just now'
  if (min < 60) return `${min} min ago`
  const hours = Math.round(min / 60)
  if (hours < 24) return `${hours} hour${hours === 1 ? '' : 's'} ago`
  const days = Math.round(hours / 24)
  if (days === 1) return 'yesterday'
  if (days < 7) return `${days} days ago`
  return formatDate(iso)
}

export function formatUsd(n: number | null | undefined, digits = 2): string {
  if (n === null || n === undefined || Number.isNaN(n)) return '—'
  if (n > 0 && n < 0.01 && digits === 2) return `$${n.toFixed(4)}`
  return `$${n.toFixed(digits)}`
}

export function formatInt(n: number | null | undefined): string {
  if (n === null || n === undefined) return '—'
  return n.toLocaleString('en-US')
}

export function plural(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`
}

/** The availability pill text for a catalog card or detail page. */
export function availabilityLabel(b: {
  available?: number | null
  copies?: number | null
  holdsWaiting?: number | null
}): { text: string; tone: 'ok' | 'busy' | 'queue' } {
  const copies = b.copies ?? 0
  const available = b.available ?? 0
  const waiting = b.holdsWaiting ?? 0
  if (available > 0) return { text: `${available} of ${copies} available`, tone: 'ok' }
  if (waiting > 0) return { text: `On loan · ${plural(waiting, 'hold')} waiting`, tone: 'queue' }
  return { text: 'On loan', tone: 'busy' }
}

/** Initials for an avatar fallback. */
export function initials(name: string | null | undefined, email?: string | null): string {
  const src = (name ?? '').trim() || (email ?? '').split('@')[0] || '?'
  const parts = src.split(/\s+/).filter(Boolean)
  const first = parts[0]?.[0] ?? '?'
  const last = parts.length > 1 ? (parts[parts.length - 1]?.[0] ?? '') : ''
  return (first + last).toUpperCase()
}
