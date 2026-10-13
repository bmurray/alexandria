import { z } from 'zod'
import { appConfigSchema } from '../schemas/config'
import { sessionSchema, userListSchema, userSchema, type Role } from '../schemas/auth'
import {
  bookListSchema,
  bookResponseSchema,
  genreListSchema,
  suggestionResponseSchema,
  type BookInput,
  type BookListParams,
  type BookPatch,
} from '../schemas/book'
import { bookDetailSchema, dashboardSchema } from '../schemas/detail'
import { holdListSchema, holdResponseSchema, loanListSchema, loanResponseSchema, type LoanListParams } from '../schemas/loan'
import {
  aiUsageSchema,
  chatResponseSchema,
  conversationDetailSchema,
  conversationListSchema,
  providerResponseSchema,
  type ProviderPatch,
} from '../schemas/ai'
import { emptySchema } from '../schemas/common'

export const PAGE_SIZE = 24
export const LOAN_PAGE_SIZE = 50

export class ApiError extends Error {
  readonly status: number
  /** Validation detail from a 400 (`{error, problems}`), one message per problem. */
  readonly problems: string[]
  constructor(status: number, message: string, problems: string[] = []) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.problems = problems
  }
}

function problemText(p: unknown): string | null {
  if (typeof p === 'string') return p
  if (typeof p === 'object' && p !== null) {
    const o = p as Record<string, unknown>
    const msg = typeof o.message === 'string' ? o.message : typeof o.error === 'string' ? o.error : null
    const field = typeof o.field === 'string' ? o.field : null
    if (msg && field) return `${field}: ${msg}`
    if (msg) return msg
  }
  return null
}

export function errorFromBody(status: number, data: unknown, fallback: string): ApiError {
  let msg = fallback
  let problems: string[] = []
  if (typeof data === 'object' && data !== null) {
    const o = data as Record<string, unknown>
    if (typeof o.error === 'string') msg = o.error
    if (Array.isArray(o.problems)) problems = o.problems.map(problemText).filter((p): p is string => !!p)
  }
  return new ApiError(status, msg, problems)
}

/** The text to show a person for any thrown value. */
export function errorMessage(err: unknown, fallback = 'Something went wrong'): string {
  if (err instanceof ApiError) {
    if (err.status === 401) return 'Please sign in to do that'
    if (err.status === 403) return 'You do not have permission to do that'
    return err.message || fallback
  }
  if (err instanceof Error) return err.message || fallback
  return fallback
}

type Json = Record<string, unknown> | undefined

async function request<S extends z.ZodType>(
  schema: S,
  path: string,
  init: { method?: string; body?: Json } = {},
): Promise<z.infer<S>> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  // JSON bodies pass the server's content-type CSRF guard.
  if (init.body !== undefined) headers['Content-Type'] = 'application/json'
  let res: Response
  try {
    res = await fetch(path, {
      method: init.method ?? 'GET',
      credentials: 'same-origin',
      headers,
      body: init.body !== undefined ? JSON.stringify(init.body) : undefined,
    })
  } catch {
    throw new ApiError(0, 'The library is unreachable. Check your connection.')
  }
  if (res.status === 204) return schema.parse(undefined) as z.infer<S>
  const text = await res.text()
  let data: unknown = undefined
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      throw new ApiError(res.status, res.ok ? `unexpected response from ${path}` : res.statusText || 'request failed')
    }
  }
  if (!res.ok) throw errorFromBody(res.status, data, res.statusText || 'request failed')
  return schema.parse(data) as z.infer<S>
}

function qs(params: Record<string, string | number | boolean | undefined>): string {
  const sp = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === '' || v === false) continue
    sp.set(k, String(v))
  }
  const s = sp.toString()
  return s ? `?${s}` : ''
}

export function booksPath(p: BookListParams): string {
  return `/api/books${qs({
    q: p.q,
    genre: p.genre,
    available: p.available ? 'true' : undefined,
    sort: p.sort,
    limit: PAGE_SIZE,
    offset: p.page * PAGE_SIZE,
  })}`
}

// Query functions enforce their keys so useQuery callers cannot drift.
export const api = {
  config: ({ queryKey }: { queryKey: ['config'] }) => request(appConfigSchema, `/api/${queryKey[0]}`),

  me: async ({ queryKey }: { queryKey: ['auth', 'me'] }) => {
    try {
      return await request(sessionSchema, `/api/${queryKey[0]}/${queryKey[1]}`)
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) return null
      throw err
    }
  },
  logout: () => request(emptySchema, '/api/auth/logout', { method: 'POST', body: {} }),
  devLogin: (body: { email: string; name: string; role?: Role }) =>
    request(sessionSchema, '/api/auth/dev/login', { method: 'POST', body }),
  oidcStartUrl: (provider: string, returnTo: string) =>
    `/api/auth/oidc/${encodeURIComponent(provider)}/start?returnTo=${encodeURIComponent(returnTo)}`,

  users: ({ queryKey }: { queryKey: ['users'] }) => request(userListSchema, `/api/${queryKey[0]}`),
  setUserRole: (id: string, role: Role) =>
    request(z.object({ user: userSchema }), `/api/users/${encodeURIComponent(id)}`, { method: 'PATCH', body: { role } }),

  books: ({ queryKey }: { queryKey: ['books', BookListParams] }) => request(bookListSchema, booksPath(queryKey[1])),
  book: ({ queryKey }: { queryKey: ['book', string] }) =>
    request(bookDetailSchema, `/api/books/${encodeURIComponent(queryKey[1])}`),
  genres: ({ queryKey }: { queryKey: ['genres'] }) => request(genreListSchema, `/api/${queryKey[0]}`),
  createBook: (input: BookInput) => request(bookResponseSchema, '/api/books', { method: 'POST', body: input }),
  updateBook: (id: string, patch: BookPatch) =>
    request(bookResponseSchema, `/api/books/${encodeURIComponent(id)}`, { method: 'PATCH', body: patch }),
  deleteBook: (id: string) => request(emptySchema, `/api/books/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  lookup: (body: { isbn?: string; title?: string; author?: string }) =>
    request(suggestionResponseSchema, '/api/books/lookup', { method: 'POST', body }),
  enrich: (body: { title: string; author: string; isbn: string; description: string }) =>
    request(suggestionResponseSchema, '/api/ai/enrich', { method: 'POST', body }),

  checkout: (bookId: string, userId?: string) =>
    request(loanResponseSchema, `/api/books/${encodeURIComponent(bookId)}/checkout`, {
      method: 'POST',
      body: userId ? { userId } : {},
    }),
  returnLoan: (loanId: string) =>
    request(loanResponseSchema, `/api/loans/${encodeURIComponent(loanId)}/return`, { method: 'POST', body: {} }),
  loans: ({ queryKey }: { queryKey: ['loans', LoanListParams] }) => {
    const p = queryKey[1]
    return request(
      loanListSchema,
      `/api/loans${qs({ status: p.status, userId: p.userId, limit: LOAN_PAGE_SIZE, offset: p.page * LOAN_PAGE_SIZE })}`,
    )
  },
  myLoans: ({ queryKey }: { queryKey: ['me', 'loans'] }) => request(loanListSchema, `/api/${queryKey[0]}/${queryKey[1]}`),
  myHolds: ({ queryKey }: { queryKey: ['me', 'holds'] }) => request(holdListSchema, `/api/${queryKey[0]}/${queryKey[1]}`),

  placeHold: (bookId: string) =>
    request(holdResponseSchema, `/api/books/${encodeURIComponent(bookId)}/holds`, { method: 'POST', body: {} }),
  cancelHold: (holdId: string) => request(emptySchema, `/api/holds/${encodeURIComponent(holdId)}`, { method: 'DELETE' }),
  holds: ({ queryKey }: { queryKey: ['holds', string] }) => request(holdListSchema, `/api/holds${qs({ bookId: queryKey[1] })}`),

  dashboard: ({ queryKey }: { queryKey: ['dashboard'] }) => request(dashboardSchema, `/api/${queryKey[0]}`),

  chat: (body: { conversationId?: string; message: string }) =>
    request(chatResponseSchema, '/api/ai/chat', { method: 'POST', body }),
  conversations: async ({ queryKey }: { queryKey: ['ai', 'conversations'] }) => {
    try {
      return await request(conversationListSchema, `/api/${queryKey[0]}/${queryKey[1]}`)
    } catch (err) {
      // Anonymous visitors may have no history endpoint; treat as empty.
      if (err instanceof ApiError && (err.status === 401 || err.status === 404)) return { conversations: [] }
      throw err
    }
  },
  conversation: ({ queryKey }: { queryKey: ['ai', 'conversation', string] }) =>
    request(conversationDetailSchema, `/api/ai/conversations/${encodeURIComponent(queryKey[2])}`),
  deleteConversation: (id: string) =>
    request(emptySchema, `/api/ai/conversations/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  aiUsage: ({ queryKey }: { queryKey: ['ai', 'usage'] }) => request(aiUsageSchema, `/api/${queryKey[0]}/${queryKey[1]}`),
  patchProvider: (name: string, patch: ProviderPatch) =>
    request(providerResponseSchema, `/api/ai/providers/${encodeURIComponent(name)}`, { method: 'PATCH', body: patch }),
}
