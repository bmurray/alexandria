/** Strip hyphens, spaces and other separators; keep digits and a trailing X. */
export function normalizeIsbn(raw: string): string {
  return raw.toUpperCase().replace(/[^0-9X]/g, '')
}

function validIsbn10(s: string): boolean {
  if (!/^\d{9}[\dX]$/.test(s)) return false
  let sum = 0
  for (let i = 0; i < 10; i++) {
    const ch = s[i] ?? '0'
    const v = ch === 'X' ? 10 : Number(ch)
    sum += v * (10 - i)
  }
  return sum % 11 === 0
}

function validIsbn13(s: string): boolean {
  if (!/^\d{13}$/.test(s)) return false
  let sum = 0
  for (let i = 0; i < 13; i++) sum += Number(s[i]) * (i % 2 === 0 ? 1 : 3)
  return sum % 10 === 0
}

/** True for a checksum-valid ISBN-10 or ISBN-13 (separators allowed). */
export function isValidIsbn(raw: string): boolean {
  const s = normalizeIsbn(raw)
  return validIsbn10(s) || validIsbn13(s)
}

/** "9780441013593" -> "978-0-441-01359-3"-ish grouping for display (best effort). */
export function formatIsbn(raw: string | null | undefined): string {
  if (!raw) return ''
  const s = normalizeIsbn(raw)
  if (s.length === 13) return `${s.slice(0, 3)}-${s.slice(3, 4)}-${s.slice(4, 9)}-${s.slice(9, 12)}-${s.slice(12)}`
  if (s.length === 10) return `${s.slice(0, 1)}-${s.slice(1, 6)}-${s.slice(6, 9)}-${s.slice(9)}`
  return raw
}
