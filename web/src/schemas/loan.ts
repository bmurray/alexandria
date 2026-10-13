import { z } from 'zod'

export const loanBookSchema = z.object({
  id: z.string(),
  title: z.string().nullable().optional(),
  author: z.string().nullable().optional(),
  coverUrl: z.string().nullable().optional(),
})

export const loanUserSchema = z.object({
  id: z.string(),
  name: z.string().nullable().optional(),
  email: z.string().nullable().optional(),
})

export const loanSchema = z.object({
  id: z.string(),
  bookId: z.string(),
  userId: z.string(),
  checkedOutAt: z.string(),
  dueAt: z.string(),
  returnedAt: z.string().nullable().optional(),
  overdue: z.boolean().nullable().optional(),
  book: loanBookSchema.nullable().optional(),
  user: loanUserSchema.nullable().optional(),
})
export type Loan = z.infer<typeof loanSchema>

export const loanResponseSchema = z.object({ loan: loanSchema })
export const loanListSchema = z.object({
  loans: z.array(loanSchema),
  total: z.number().nullable().optional(),
})

export const holdStatusSchema = z.enum(['waiting', 'ready', 'fulfilled', 'cancelled', 'lapsed'])
export type HoldStatus = z.infer<typeof holdStatusSchema>

export const holdSchema = z.object({
  id: z.string(),
  bookId: z.string(),
  userId: z.string(),
  placedAt: z.string().nullable().optional(),
  readyAt: z.string().nullable().optional(),
  status: holdStatusSchema.catch('waiting'),
  position: z.number().nullable().optional(),
  book: loanBookSchema.nullable().optional(),
  user: loanUserSchema.nullable().optional(),
})
export type Hold = z.infer<typeof holdSchema>

export const holdResponseSchema = z.object({ hold: holdSchema })
export const holdListSchema = z.object({ holds: z.array(holdSchema) })

export const loanStatusSchema = z.enum(['open', 'overdue', 'returned'])
export type LoanStatus = z.infer<typeof loanStatusSchema>

export type LoanListParams = {
  status: LoanStatus | ''
  userId: string
  page: number
}
