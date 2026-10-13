import { z } from 'zod'
import { bookSchema } from './book'
import { holdSchema, loanSchema } from './loan'

export const bookDetailSchema = z.object({
  book: bookSchema,
  loans: z.array(loanSchema).nullable().optional(),
  holds: z.array(holdSchema).nullable().optional(),
})
export type BookDetail = z.infer<typeof bookDetailSchema>

export const dashboardSchema = z.object({
  books: z.number().nullable().optional(),
  copies: z.number().nullable().optional(),
  openLoans: z.number().nullable().optional(),
  overdue: z.number().nullable().optional(),
  holdsWaiting: z.number().nullable().optional(),
  members: z.number().nullable().optional(),
  recentLoans: z.array(loanSchema).nullable().optional(),
  overdueLoans: z.array(loanSchema).nullable().optional(),
  readyHolds: z.array(holdSchema).nullable().optional(),
})
export type Dashboard = z.infer<typeof dashboardSchema>
