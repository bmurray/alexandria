import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { openStream } from '../lib/stream'
import { bookEventSchema } from '../schemas/common'

/**
 * Owns ONE WebSocket to GET /api/books while mounted. The snapshot frame is
 * ignored (the list queries own their data); each change event invalidates
 * the catalog list queries and the touched book's detail query. This is the
 * one place a subscription effect is justified: it owns a connection.
 */
export function useLiveBooks(): boolean {
  const qc = useQueryClient()
  const [live, setLive] = useState(false)

  useEffect(() => {
    const conn = openStream('/api/books', {
      onOpen: () => setLive(true),
      onClose: () => setLive(false),
      onSnapshot: () => undefined,
      onEvent: (topic, data) => {
        if (topic && topic !== 'books') return
        void qc.invalidateQueries({ queryKey: ['books'] })
        void qc.invalidateQueries({ queryKey: ['genres'] })
        const ev = bookEventSchema.safeParse(data)
        if (ev.success && ev.data.id) void qc.invalidateQueries({ queryKey: ['book', ev.data.id] })
        // Loan and hold activity shows up on these pages too.
        void qc.invalidateQueries({ queryKey: ['dashboard'] })
        void qc.invalidateQueries({ queryKey: ['loans'] })
        void qc.invalidateQueries({ queryKey: ['holds'] })
        void qc.invalidateQueries({ queryKey: ['me'] })
      },
    })
    return () => {
      conn.close()
      setLive(false)
    }
  }, [qc])

  return live
}
