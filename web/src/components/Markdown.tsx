import { useMemo } from 'react'
import { Link } from 'react-router-dom'
import { parseMarkdown, type Block, type Inline } from '../lib/markdown'

function Inlines({ nodes }: { nodes: Inline[] }) {
  return (
    <>
      {nodes.map((n, i) => {
        switch (n.type) {
          case 'text':
            return <span key={i}>{n.text}</span>
          case 'strong':
            return (
              <strong key={i}>
                <Inlines nodes={n.children} />
              </strong>
            )
          case 'em':
            return (
              <em key={i}>
                <Inlines nodes={n.children} />
              </em>
            )
          case 'code':
            return <code key={i}>{n.text}</code>
          case 'link':
            return n.href.startsWith('/') ? (
              <Link key={i} to={n.href}>
                <Inlines nodes={n.children} />
              </Link>
            ) : (
              <a key={i} href={n.href} target="_blank" rel="noopener noreferrer">
                <Inlines nodes={n.children} />
              </a>
            )
        }
      })}
    </>
  )
}

function BlockView({ block }: { block: Block }) {
  switch (block.type) {
    case 'paragraph':
      return (
        <p>
          <Inlines nodes={block.children} />
        </p>
      )
    case 'heading': {
      const Tag = block.level <= 2 ? 'h4' : 'h5'
      return (
        <Tag>
          <Inlines nodes={block.children} />
        </Tag>
      )
    }
    case 'list': {
      const items = block.items.map((it, i) => (
        <li key={i}>
          <Inlines nodes={it} />
        </li>
      ))
      return block.ordered ? <ol>{items}</ol> : <ul>{items}</ul>
    }
  }
}

/** Renders the markdown subset in lib/markdown.ts. No HTML is injected. */
export function Markdown({ text }: { text: string }) {
  const blocks = useMemo(() => parseMarkdown(text), [text])
  return (
    <div className="md">
      {blocks.map((b, i) => (
        <BlockView key={i} block={b} />
      ))}
    </div>
  )
}
