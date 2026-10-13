import { useCallback, useMemo, useState } from 'react'

type Props = {
  title: string
  author?: string | null
  coverUrl?: string | null
  size?: 'card' | 'large' | 'thumb'
  className?: string
}

const SPINES = ['#b5542b', '#5f6b3b', '#8a5a9a', '#2f6f8f', '#a2762a', '#7a3b3b', '#3f7a6a']

function hue(s: string): string {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0
  return SPINES[h % SPINES.length] ?? SPINES[0]!
}

/**
 * A cover image with a tasteful fallback: when there is no URL, or the image
 * fails to load, a coloured "spine" shows the title and author instead.
 */
export function Cover({ title, author, coverUrl, size = 'card', className }: Props) {
  const [failed, setFailed] = useState(false)
  const onError = useCallback(() => setFailed(true), [])
  // Open Library answers a 1×1 blank for unknown covers; treat that as missing.
  const onLoad = useCallback((e: React.SyntheticEvent<HTMLImageElement>) => {
    if (e.currentTarget.naturalWidth <= 1 || e.currentTarget.naturalHeight <= 1) setFailed(true)
  }, [])
  const bg = useMemo(() => hue(title), [title])
  const showImage = !!coverUrl && !failed
  return (
    <div className={`cover cover--${size} ${className ?? ''}`} style={{ '--spine': bg } as React.CSSProperties}>
      {showImage ? (
        <img src={coverUrl} alt="" loading="lazy" onError={onError} onLoad={onLoad} />
      ) : (
        <div className="cover__fallback" aria-hidden="true">
          <span className="cover__fallback-title">{title}</span>
          {author && <span className="cover__fallback-author">{author}</span>}
        </div>
      )}
    </div>
  )
}
