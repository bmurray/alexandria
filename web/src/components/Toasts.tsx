import { useToast } from '../hooks/useToast'
import { CloseIcon } from './icons'

export function Toasts() {
  const { toasts, dismiss } = useToast()
  return (
    <div className="toasts" role="status" aria-live="polite">
      {toasts.map((t) => (
        <div key={t.id} className={`toast toast--${t.kind}`} data-testid="toast">
          <span className="toast__text">{t.message}</span>
          <button type="button" className="toast__close" onClick={() => dismiss(t.id)} aria-label="Dismiss">
            <CloseIcon size={14} />
          </button>
        </div>
      ))}
    </div>
  )
}
