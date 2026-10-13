import { z } from 'zod'

export const roleSchema = z.enum(['member', 'librarian', 'admin'])
export type Role = z.infer<typeof roleSchema>

export const userSchema = z.object({
  id: z.string(),
  email: z.string().nullable().optional(),
  name: z.string().nullable().optional(),
  picture: z.string().nullable().optional(),
  role: roleSchema.catch('member'),
  createdAt: z.string().nullable().optional(),
  lastLoginAt: z.string().nullable().optional(),
})
export type User = z.infer<typeof userSchema>

export const sessionSchema = z.object({ user: userSchema })
export type Session = z.infer<typeof sessionSchema>

export const userListSchema = z.object({ users: z.array(userSchema) })
