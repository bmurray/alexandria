import { z } from 'zod'

/** 204 responses parse against this. */
export const emptySchema = z.undefined()

/** One frame on the live socket: a snapshot first, then change events. */
export const frameSchema = z.object({
  type: z.string(),
  topic: z.string().nullable().optional(),
  data: z.unknown().optional(),
})
export type Frame = z.infer<typeof frameSchema>

export const bookEventSchema = z.object({
  action: z.string().nullable().optional(),
  id: z.string().nullable().optional(),
})
export type BookEvent = z.infer<typeof bookEventSchema>
