import { describe, expect, test } from 'bun:test'
import { inlineText, parseInline, parseMarkdown, safeHref } from './markdown'

describe('parseInline', () => {
  test('bold, italic, code', () => {
    const nodes = parseInline('A **bold** and *soft* `code` line')
    expect(nodes.map((n) => n.type)).toEqual(['text', 'strong', 'text', 'em', 'text', 'code', 'text'])
    expect(inlineText(nodes)).toBe('A bold and soft code line')
  })
  test('links keep safe schemes only', () => {
    const ok = parseInline('[Dune](/books/abc)')
    expect(ok[0]).toMatchObject({ type: 'link', href: '/books/abc' })
    const bad = parseInline('[x](javascript:alert(1))')
    expect(bad[0]).toEqual({ type: 'text', text: 'x' })
  })
  test('bare urls become links', () => {
    const nodes = parseInline('see https://openlibrary.org/x.')
    expect(nodes[1]).toMatchObject({ type: 'link', href: 'https://openlibrary.org/x' })
  })
  test('underscores inside words are not emphasis', () => {
    expect(parseInline('snake_case_name')).toEqual([{ type: 'text', text: 'snake_case_name' }])
  })
})

describe('parseMarkdown', () => {
  test('paragraphs and lists', () => {
    const blocks = parseMarkdown('Hello there.\nSecond line.\n\n- one\n- **two**\n\n1. first\n2. second')
    expect(blocks.map((b) => b.type)).toEqual(['paragraph', 'list', 'list'])
    expect(blocks[0]).toMatchObject({ type: 'paragraph' })
    expect(blocks[1]).toMatchObject({ type: 'list', ordered: false })
    expect(blocks[2]).toMatchObject({ type: 'list', ordered: true })
    const list = blocks[1]
    if (list?.type === 'list') expect(list.items.length).toBe(2)
  })
  test('headings', () => {
    const blocks = parseMarkdown('## Picks\ntext')
    expect(blocks[0]).toMatchObject({ type: 'heading', level: 2 })
  })
  test('empty input', () => {
    expect(parseMarkdown('')).toEqual([])
  })
})

describe('safeHref', () => {
  test('rejects protocol-relative and scripts', () => {
    expect(safeHref('//evil')).toBeNull()
    expect(safeHref('javascript:x')).toBeNull()
    expect(safeHref('mailto:a@b.c')).toBe('mailto:a@b.c')
  })
})
