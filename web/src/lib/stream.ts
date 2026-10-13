import { frameSchema } from '../schemas/common'

export type StreamHandlers = {
  onSnapshot: (data: unknown) => void
  onEvent: (topic: string, data: unknown) => void
  onOpen?: () => void
  onClose?: () => void
}

export type StreamConnection = { close: () => void }

/**
 * Open the live side of a GET endpoint (internal/realtime): the same URL with
 * a WebSocket upgrade sends a snapshot frame, then one event frame per change.
 * Reconnects with exponential backoff until closed.
 */
export function openStream(path: string, handlers: StreamHandlers): StreamConnection {
  let ws: WebSocket | null = null
  let closed = false
  let retry = 500
  let timer: number | undefined

  const connect = () => {
    const url = new URL(path, window.location.href)
    url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
    ws = new WebSocket(url)
    ws.onopen = () => {
      retry = 500
      handlers.onOpen?.()
    }
    ws.onmessage = (e) => {
      let frame
      try {
        frame = frameSchema.parse(JSON.parse(String(e.data)))
      } catch {
        return
      }
      if (frame.type === 'snapshot') handlers.onSnapshot(frame.data)
      else if (frame.type === 'event') handlers.onEvent(frame.topic ?? '', frame.data)
    }
    ws.onclose = () => {
      handlers.onClose?.()
      if (closed) return
      timer = window.setTimeout(connect, retry)
      retry = Math.min(retry * 2, 15_000)
    }
    ws.onerror = () => undefined
  }
  connect()
  return {
    close: () => {
      closed = true
      window.clearTimeout(timer)
      ws?.close()
    },
  }
}
