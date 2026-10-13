import { useContext } from 'react'
import { LiveContext } from './LiveContext'

export function useLive(): boolean {
  return useContext(LiveContext)
}
