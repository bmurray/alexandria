/**
 * A tiny, safe markdown subset for assistant replies: paragraphs, headings,
 * bullet and numbered lists, **bold**, *italic*, `code` and [links](https://…).
 * It produces a tree; components/Markdown.tsx renders it with React, so no
 * HTML is ever injected. Links keep only http(s), mailto and same-site paths.
 */

export type Inline =
  | { type: 'text'; text: string }
  | { type: 'strong'; children: Inline[] }
  | { type: 'em'; children: Inline[] }
  | { type: 'code'; text: string }
  | { type: 'link'; href: string; children: Inline[] }

export type Block =
  | { type: 'paragraph'; children: Inline[] }
  | { type: 'heading'; level: number; children: Inline[] }
  | { type: 'list'; ordered: boolean; items: Inline[][] }

export function safeHref(href: string): string | null {
  const h = href.trim()
  if (/^(https?:|mailto:)/i.test(h)) return h
  if (h.startsWith('/') && !h.startsWith('//')) return h
  return null
}

export function parseInline(src: string): Inline[] {
  const out: Inline[] = []
  let text = ''
  const flush = () => {
    if (text) out.push({ type: 'text', text })
    text = ''
  }
  let i = 0
  while (i < src.length) {
    const rest = src.slice(i)
    let m: RegExpMatchArray | null
    if ((m = /^`([^`]+)`/.exec(rest))) {
      flush()
      out.push({ type: 'code', text: m[1] ?? '' })
      i += m[0].length
      continue
    }
    if ((m = /^\*\*(.+?)\*\*/.exec(rest))) {
      flush()
      out.push({ type: 'strong', children: parseInline(m[1] ?? '') })
      i += m[0].length
      continue
    }
    if ((m = /^(?:\*([^*\s][^*]*?)\*|_([^_\s][^_]*?)_)(?![\w])/.exec(rest))) {
      flush()
      out.push({ type: 'em', children: parseInline(m[1] ?? m[2] ?? '') })
      i += m[0].length
      continue
    }
    if ((m = /^\[([^\]]+)\]\(([^)\s]+)\)/.exec(rest))) {
      const href = safeHref(m[2] ?? '')
      flush()
      if (href) out.push({ type: 'link', href, children: parseInline(m[1] ?? '') })
      else out.push({ type: 'text', text: m[1] ?? '' })
      i += m[0].length
      continue
    }
    if ((m = /^https?:\/\/[^\s<>)\]]+/.exec(rest))) {
      // Trailing sentence punctuation belongs to the prose, not the URL.
      const href = m[0].replace(/[.,;:!?]+$/, '')
      flush()
      out.push({ type: 'link', href, children: [{ type: 'text', text: href }] })
      i += href.length
      continue
    }
    text += src[i]
    i++
  }
  flush()
  return out
}

export function parseMarkdown(src: string): Block[] {
  const blocks: Block[] = []
  const lines = src.replace(/\r\n?/g, '\n').split('\n')
  let para: string[] = []
  let list: { ordered: boolean; items: string[] } | null = null

  const flushPara = () => {
    if (para.length) blocks.push({ type: 'paragraph', children: parseInline(para.join(' ')) })
    para = []
  }
  const flushList = () => {
    if (list) blocks.push({ type: 'list', ordered: list.ordered, items: list.items.map(parseInline) })
    list = null
  }

  for (const raw of lines) {
    const line = raw.trimEnd()
    if (!line.trim()) {
      flushPara()
      flushList()
      continue
    }
    const heading = /^(#{1,6})\s+(.*)$/.exec(line)
    if (heading) {
      flushPara()
      flushList()
      blocks.push({ type: 'heading', level: heading[1]?.length ?? 1, children: parseInline(heading[2] ?? '') })
      continue
    }
    const bullet = /^\s*[-*•]\s+(.*)$/.exec(line)
    const numbered = /^\s*\d+[.)]\s+(.*)$/.exec(line)
    if (bullet || numbered) {
      flushPara()
      const ordered = !!numbered
      const item = (bullet?.[1] ?? numbered?.[1] ?? '').trim()
      if (!list || list.ordered !== ordered) {
        flushList()
        list = { ordered, items: [] }
      }
      list.items.push(item)
      continue
    }
    if (list && /^\s{2,}/.test(raw)) {
      // continuation of the previous list item
      const last = list.items.length - 1
      list.items[last] = `${list.items[last] ?? ''} ${line.trim()}`
      continue
    }
    flushList()
    para.push(line.trim())
  }
  flushPara()
  flushList()
  return blocks
}

/** Plain text of a tree (used for tests and titles). */
export function inlineText(nodes: Inline[]): string {
  return nodes
    .map((n) => (n.type === 'text' || n.type === 'code' ? n.text : inlineText(n.children)))
    .join('')
}
