import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'

export function useConfig() {
  return useQuery({ queryKey: ['config'] as const, queryFn: api.config, staleTime: 5 * 60 * 1000 })
}
