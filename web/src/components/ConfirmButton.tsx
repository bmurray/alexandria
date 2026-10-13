import { useCallback, useState, type ReactNode } from 'react'

type Props = {
  children: ReactNode
  confirmLabel?: string
  question?: string
  onConfirm: () => void
  disabled?: boolean
  className?: string
}

/** A button that asks once, inline, before doing something destructive. */
export function ConfirmButton({ children, confirmLabel = 'Yes, do it', question = 'Are you sure?', onConfirm, disabled, className }: Props) {
  const [arming, setArming] = useState(false)
  const arm = useCallback(() => setArming(true), [])
  const cancel = useCallback(() => setArming(false), [])
  const go = useCallback(() => {
    setArming(false)
    onConfirm()
  }, [onConfirm])
  if (arming) {
    return (
      <span className="confirm" role="group" aria-label={question}>
        <span className="confirm__q">{question}</span>
        <button type="button" className="btn btn--sm btn--danger" onClick={go} autoFocus>
          {confirmLabel}
        </button>
        <button type="button" className="btn btn--sm btn--ghost" onClick={cancel}>
          Cancel
        </button>
      </span>
    )
  }
  return (
    <button type="button" className={className ?? 'btn btn--sm btn--danger'} onClick={arm} disabled={disabled}>
      {children}
    </button>
  )
}
