import { createContext } from 'react'

export type ToastKind = 'success' | 'error' | 'info'
export type Toast = { id: number; kind: ToastKind; message: string }

export type ToastContextValue = {
  toasts: Toast[]
  push: (kind: ToastKind, message: string) => void
  dismiss: (id: number) => void
}

export const ToastContext = createContext<ToastContextValue | null>(null)
