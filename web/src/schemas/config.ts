import { z } from 'zod'

export const providerSchema = z.object({
  name: z.string(),
  displayName: z.string().nullable().optional(),
  type: z.string(),
})
export type AuthProvider = z.infer<typeof providerSchema>

export const appConfigSchema = z.object({
  libraryName: z.string().nullable().optional(),
  providers: z.array(providerSchema).nullable().optional(),
  aiEnabled: z.boolean().nullable().optional(),
  loanDays: z.number().nullable().optional(),
})
export type AppConfig = z.infer<typeof appConfigSchema>
