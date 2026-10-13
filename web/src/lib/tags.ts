/** "sci-fi, Classic ,  , space opera" -> ["sci-fi", "Classic", "space opera"]; de-duplicated case-insensitively. */
export function parseTags(raw: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const part of raw.split(/[,\n;]/)) {
    const t = part.trim().replace(/\s+/g, ' ')
    if (!t) continue
    const key = t.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    out.push(t)
  }
  return out
}

export function joinTags(tags: string[] | null | undefined): string {
  return (tags ?? []).join(', ')
}
