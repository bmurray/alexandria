import { z } from 'zod'

export const toolCallSchema = z.object({
  name: z.string(),
  input: z.unknown().optional(),
  output: z.unknown().optional(),
})
export type ToolCall = z.infer<typeof toolCallSchema>

export const usageSchema = z.object({
  inputTokens: z.number().nullable().optional(),
  outputTokens: z.number().nullable().optional(),
  costUsd: z.number().nullable().optional(),
  provider: z.string().nullable().optional(),
  model: z.string().nullable().optional(),
})
export type Usage = z.infer<typeof usageSchema>

export const chatResponseSchema = z.object({
  conversationId: z.string(),
  reply: z.string(),
  toolCalls: z.array(toolCallSchema).nullable().optional(),
  usage: usageSchema.nullable().optional(),
})
export type ChatResponse = z.infer<typeof chatResponseSchema>

export const conversationSchema = z.object({
  id: z.string(),
  title: z.string().nullable().optional(),
  createdAt: z.string().nullable().optional(),
  updatedAt: z.string().nullable().optional(),
})
export type Conversation = z.infer<typeof conversationSchema>

export const conversationListSchema = z.object({ conversations: z.array(conversationSchema) })

export const chatMessageSchema = z.object({
  id: z.string(),
  role: z.enum(['user', 'assistant']).catch('assistant'),
  content: z.string().nullable().optional(),
  toolCalls: z.array(toolCallSchema).nullable().optional(),
  usage: usageSchema.nullable().optional(),
  createdAt: z.string().nullable().optional(),
})
export type ChatMessage = z.infer<typeof chatMessageSchema>

export const conversationDetailSchema = z.object({
  conversation: conversationSchema.nullable().optional(),
  messages: z.array(chatMessageSchema),
})
export type ConversationDetail = z.infer<typeof conversationDetailSchema>

export const aiProviderSchema = z.object({
  name: z.string(),
  model: z.string().nullable().optional(),
  enabled: z.boolean().nullable().optional(),
  priority: z.number().nullable().optional(),
  spendLimitUsd: z.number().nullable().optional(),
  spentUsd: z.number().nullable().optional(),
  exceeded: z.boolean().nullable().optional(),
})
export type AiProvider = z.infer<typeof aiProviderSchema>

export const aiUsageSchema = z.object({
  providers: z.array(aiProviderSchema).nullable().optional(),
  totals: z
    .object({
      todayUsd: z.number().nullable().optional(),
      allTimeUsd: z.number().nullable().optional(),
      calls: z.number().nullable().optional(),
    })
    .nullable()
    .optional(),
  byPurpose: z
    .array(
      z.object({
        purpose: z.string(),
        calls: z.number().nullable().optional(),
        costUsd: z.number().nullable().optional(),
        inputTokens: z.number().nullable().optional(),
        outputTokens: z.number().nullable().optional(),
      }),
    )
    .nullable()
    .optional(),
})
export type AiUsage = z.infer<typeof aiUsageSchema>

export const providerResponseSchema = z.object({ provider: aiProviderSchema })
export type ProviderPatch = { enabled?: boolean; priority?: number; spendLimitUsd?: number | null }
