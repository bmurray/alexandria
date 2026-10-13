import { expect, request as playwrightRequest, type APIRequestContext, type Page } from '@playwright/test'

export type Role = 'member' | 'librarian' | 'admin'

export type DevUser = { email: string; name: string; role?: Role }

export type User = { id: string; email: string; name: string; role: Role }

export type Book = {
  id: string
  title: string
  author: string
  isbn?: string | null
  year?: number | null
  genre?: string | null
  publisher?: string | null
  description?: string | null
  coverUrl?: string | null
  copies: number
  tags?: string[]
  available: number
}

let counter = 0

/** An email nobody else in this run uses. */
export function uniqueEmail(prefix = 'e2e'): string {
  counter += 1
  return `${prefix}-${Date.now()}-${counter}-${Math.floor(Math.random() * 1e6)}@example.com`
}

/**
 * Signs the request context in through the Dev provider
 * (`POST /api/auth/dev/login`, configs/e2e.pkl). The session cookie lands
 * on the context, so later calls through it are authenticated. Works for a
 * `page.request` context too, which shares the page's cookie jar.
 */
export async function loginAs(request: APIRequestContext, user: DevUser): Promise<User> {
  const res = await request.post('/api/auth/dev/login', { data: { role: 'member', ...user } })
  expect(res.status(), `dev login must be enabled (configs/e2e.pkl): ${await res.text()}`).toBe(200)
  const body = (await res.json()) as { user: User }
  expect(body.user.email).toBe(user.email)
  return body.user
}

/** A fresh, isolated API context signed in as `user`. Dispose it when done. */
export async function apiAs(baseURL: string, user: DevUser): Promise<APIRequestContext> {
  const ctx = await playwrightRequest.newContext({ baseURL })
  await loginAs(ctx, user)
  return ctx
}

/** Sign the browser page in via the API (no form), then land on `path`. */
export async function signInPage(page: Page, user: DevUser, path = '/'): Promise<User> {
  const u = await loginAs(page.request, user)
  await page.goto(path)
  return u
}

export function bookPayload(overrides: Partial<Book> = {}): Record<string, unknown> {
  const stamp = `${Date.now()}-${Math.floor(Math.random() * 1e6)}`
  return {
    title: `E2E Book ${stamp}`,
    author: `Author ${stamp}`,
    isbn: `978${String(stamp).replace(/\D/g, '').slice(-10)}`,
    year: 2001,
    genre: 'Testing',
    publisher: 'E2E Press',
    description: 'A book created by the end-to-end suite.',
    coverUrl: 'https://covers.openlibrary.org/b/id/1-M.jpg',
    copies: 1,
    tags: ['e2e', `tag-${stamp}`],
    ...overrides,
  }
}

export async function createBook(request: APIRequestContext, overrides: Partial<Book> = {}): Promise<Book> {
  const payload = bookPayload(overrides)
  const res = await request.post('/api/books', { data: payload })
  expect(res.status(), await res.text()).toBe(201)
  const { book } = (await res.json()) as { book: Book }
  return book
}
