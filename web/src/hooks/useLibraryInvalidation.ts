import { useCallback } from 'react'
import { useQueryClient } from '@tanstack/react-query'

/** After a loan or hold changes, every view that derives from it refetches. */
export function useLibraryInvalidation() {
  const qc = useQueryClient()
  return useCallback(
    async (bookId?: string) => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ['books'] }),
        bookId ? qc.invalidateQueries({ queryKey: ['book', bookId] }) : Promise.resolve(),
        qc.invalidateQueries({ queryKey: ['loans'] }),
        qc.invalidateQueries({ queryKey: ['holds'] }),
        qc.invalidateQueries({ queryKey: ['me'] }),
        qc.invalidateQueries({ queryKey: ['dashboard'] }),
        qc.invalidateQueries({ queryKey: ['genres'] }),
      ])
    },
    [qc],
  )
}
