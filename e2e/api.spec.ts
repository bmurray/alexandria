import { expect, test, type APIRequestContext } from '@playwright/test'
import { apiAs, bookPayload, createBook, loginAs, uniqueEmail, type Book } from './helpers'

// Pure request-context coverage of docs/api.md. The server runs with
// configs/e2e.pkl: Dev sign-in, seeded catalog, no AI.

test.describe('config and catalog (anonymous)', () => {
  test('GET /api/config exposes the library name and the dev provider', async ({ request }) => {
    const res = await request.get('/api/config')
    expect(res.status()).toBe(200)
    const cfg = await res.json()
    expect(typeof cfg.libraryName).toBe('string')
    expect(cfg.libraryName.length).toBeGreaterThan(0)
    expect(cfg.providers.map((p: { name: string }) => p.name)).toContain('dev')
    expect(typeof cfg.loanDays).toBe('number')
  })

  test('GET /api/books works anonymously and the seeded catalog has books', async ({ request }) => {
    const res = await request.get('/api/books')
    expect(res.status()).toBe(200)
    const body = (await res.json()) as { books: Book[]; total: number }
    expect(Array.isArray(body.books)).toBe(true)
    expect(body.total).toBeGreaterThan(0)
    expect(body.books.length).toBeGreaterThan(0)
    const first = body.books[0]!
    expect(typeof first.id).toBe('string')
    expect(typeof first.title).toBe('string')
    expect(typeof first.available).toBe('number')
  })

  test('GET /api/genres lists genres with counts', async ({ request }) => {
    const res = await request.get('/api/genres')
    expect(res.status()).toBe(200)
    const { genres } = (await res.json()) as { genres: { name: string; count: number }[] }
    expect(genres.length).toBeGreaterThan(0)
    expect(typeof genres[0]!.name).toBe('string')
    expect(typeof genres[0]!.count).toBe('number')
  })

  test('health endpoints', async ({ request }) => {
    const h = await request.get('/api/healthz')
    expect(h.status()).toBe(200)
    expect((await h.json()).ok).toBe(true)
    const r = await request.get('/api/readyz')
    expect(r.status()).toBe(200)
  })
})

test.describe('auth and roles', () => {
  test('anonymous is 401 on POST /api/books, member is 403', async ({ request }) => {
    const anon = await request.post('/api/books', { data: bookPayload() })
    expect(anon.status()).toBe(401)

    await loginAs(request, { email: uniqueEmail('member'), name: 'Member', role: 'member' })
    const member = await request.post('/api/books', { data: bookPayload() })
    expect(member.status()).toBe(403)
  })

  test('dev login, me, logout', async ({ request }) => {
    const email = uniqueEmail('session')
    const user = await loginAs(request, { email, name: 'Session Person', role: 'librarian' })
    expect(user.role).toBe('librarian')

    const me = await request.get('/api/auth/me')
    expect(me.status()).toBe(200)
    expect((await me.json()).user.email).toBe(email)

    const out = await request.post('/api/auth/logout', { data: {} })
    expect(out.status()).toBe(204)

    const after = await request.get('/api/auth/me')
    expect(after.status()).toBe(401)
  })

  test('CSRF guard: a state-changing request without a JSON content type is 415', async ({ request }) => {
    await loginAs(request, { email: uniqueEmail('csrf'), name: 'CSRF', role: 'librarian' })
    const res = await request.post('/api/books', {
      headers: { 'content-type': 'text/plain' },
      data: JSON.stringify(bookPayload()),
    })
    expect(res.status()).toBe(415)
  })

  test('admin lists users and changes a role; non-admin is 403', async ({ request, baseURL }) => {
    const memberEmail = uniqueEmail('promote')
    const member = await apiAs(baseURL!, { email: memberEmail, name: 'Promote Me', role: 'member' })
    const memberId = (await (await member.get('/api/auth/me')).json()).user.id as string

    const forbidden = await member.get('/api/users')
    expect(forbidden.status()).toBe(403)
    await member.dispose()

    await loginAs(request, { email: uniqueEmail('admin'), name: 'Admin', role: 'admin' })
    const list = await request.get('/api/users')
    expect(list.status()).toBe(200)
    const { users } = (await list.json()) as { users: { id: string; email: string; role: string }[] }
    expect(users.some((u) => u.email === memberEmail)).toBe(true)

    const patch = await request.patch(`/api/users/${memberId}`, { data: { role: 'librarian' } })
    expect(patch.status(), await patch.text()).toBe(200)
    expect((await patch.json()).user.role).toBe('librarian')
  })
})

test.describe('books, loans and holds', () => {
  let librarian: APIRequestContext
  let member: APIRequestContext
  let memberId: string
  let other: APIRequestContext
  let otherId: string

  test.beforeAll(async ({ baseURL }) => {
    librarian = await apiAs(baseURL!, { email: uniqueEmail('librarian'), name: 'Lib Rarian', role: 'librarian' })
    member = await apiAs(baseURL!, { email: uniqueEmail('member'), name: 'Mem Ber', role: 'member' })
    memberId = (await (await member.get('/api/auth/me')).json()).user.id
    other = await apiAs(baseURL!, { email: uniqueEmail('other'), name: 'Other Member', role: 'member' })
    otherId = (await (await other.get('/api/auth/me')).json()).user.id
  })

  test.afterAll(async () => {
    await Promise.all([librarian, member, other].filter(Boolean).map((c) => c.dispose()))
  })

  test('validation: a missing title is 400 with problems', async () => {
    const res = await librarian.post('/api/books', { data: { ...bookPayload(), title: '' } })
    expect(res.status()).toBe(400)
    const body = await res.json()
    expect(typeof body.error).toBe('string')
    expect(Array.isArray(body.problems)).toBe(true)
    expect(body.problems.join(' ')).toMatch(/title/i)
  })

  test('create, read, edit, search, delete', async () => {
    const payload = bookPayload({ copies: 2 })
    const created = await librarian.post('/api/books', { data: payload })
    expect(created.status(), await created.text()).toBe(201)
    const { book } = (await created.json()) as { book: Book }
    expect(book.id).toBeTruthy()
    expect(book.title).toBe(payload.title)
    expect(book.author).toBe(payload.author)
    expect(book.isbn).toBe(payload.isbn)
    expect(book.year).toBe(payload.year)
    expect(book.genre).toBe(payload.genre)
    expect(book.publisher).toBe(payload.publisher)
    expect(book.description).toBe(payload.description)
    expect(book.coverUrl).toBe(payload.coverUrl)
    expect(book.copies).toBe(2)
    expect(book.tags).toEqual(payload.tags)
    expect(book.available).toBe(2)

    const read = await member.get(`/api/books/${book.id}`)
    expect(read.status()).toBe(200)
    expect((await read.json()).book.id).toBe(book.id)

    const edited = await librarian.patch(`/api/books/${book.id}`, { data: { year: 1999, genre: 'Edited' } })
    expect(edited.status(), await edited.text()).toBe(200)
    const after = (await edited.json()).book as Book
    expect(after.year).toBe(1999)
    expect(after.genre).toBe('Edited')
    expect(after.title).toBe(book.title)

    // Search by full title, author prefix, ISBN and tag.
    const tag = (payload.tags as string[])[1]!
    const queries = [book.title, book.author.split(' ').slice(0, 2).join(' '), book.isbn!, tag]
    for (const q of queries) {
      const res = await member.get(`/api/books?q=${encodeURIComponent(q)}`)
      expect(res.status()).toBe(200)
      const { books } = (await res.json()) as { books: Book[] }
      expect(books.map((b) => b.id), `search for ${JSON.stringify(q)}`).toContain(book.id)
    }

    const del = await librarian.delete(`/api/books/${book.id}`)
    expect(del.status()).toBe(204)
    const gone = await member.get(`/api/books/${book.id}`)
    expect(gone.status()).toBe(404)
  })

  test('check-out and return', async () => {
    const book = await createBook(librarian, { copies: 1 })

    const out = await member.post(`/api/books/${book.id}/checkout`, { data: {} })
    expect(out.status(), await out.text()).toBe(201)
    const { loan } = await out.json()
    expect(loan.id).toBeTruthy()
    expect(loan.bookId ?? loan.book?.id).toBe(book.id)
    expect(loan.dueAt).toBeTruthy()
    expect(loan.returnedAt ?? null).toBeNull()

    const read = await member.get(`/api/books/${book.id}`)
    expect((await read.json()).book.available).toBe(0)

    // No copies left: a second checkout (by anyone) is 409.
    const again = await other.post(`/api/books/${book.id}/checkout`, { data: {} })
    expect(again.status()).toBe(409)

    // Delete while on loan is 409.
    const del = await librarian.delete(`/api/books/${book.id}`)
    expect(del.status()).toBe(409)

    const mine = await member.get('/api/me/loans')
    expect(mine.status()).toBe(200)
    expect((await mine.json()).loans.some((l: { id: string }) => l.id === loan.id)).toBe(true)

    const back = await member.post(`/api/loans/${loan.id}/return`, { data: {} })
    expect(back.status(), await back.text()).toBe(200)
    expect((await back.json()).loan.returnedAt).toBeTruthy()

    const after = await member.get(`/api/books/${book.id}`)
    expect((await after.json()).book.available).toBe(1)

    // Librarian checks it out for another member via userId, then returns it.
    const forOther = await librarian.post(`/api/books/${book.id}/checkout`, { data: { userId: otherId } })
    expect(forOther.status(), await forOther.text()).toBe(201)
    const otherLoan = (await forOther.json()).loan
    expect(otherLoan.userId ?? otherLoan.user?.id).toBe(otherId)

    const otherSees = await other.get('/api/me/loans')
    expect((await otherSees.json()).loans.some((l: { id: string }) => l.id === otherLoan.id)).toBe(true)

    const ret = await librarian.post(`/api/loans/${otherLoan.id}/return`, { data: {} })
    expect(ret.status()).toBe(200)

    // Delete after return is 204 and the book is gone.
    const del2 = await librarian.delete(`/api/books/${book.id}`)
    expect(del2.status()).toBe(204)
    expect((await member.get(`/api/books/${book.id}`)).status()).toBe(404)
  })

  test('holds: only on an unavailable book; cancel', async () => {
    const book = await createBook(librarian, { copies: 1 })

    // Available: a hold is 409.
    const early = await other.post(`/api/books/${book.id}/holds`, { data: {} })
    expect(early.status()).toBe(409)

    const out = await member.post(`/api/books/${book.id}/checkout`, { data: {} })
    expect(out.status()).toBe(201)
    const loanId = (await out.json()).loan.id

    const hold = await other.post(`/api/books/${book.id}/holds`, { data: {} })
    expect(hold.status(), await hold.text()).toBe(201)
    const holdId = (await hold.json()).hold.id
    expect(holdId).toBeTruthy()

    const listed = await librarian.get(`/api/holds?bookId=${book.id}`)
    expect(listed.status()).toBe(200)
    expect((await listed.json()).holds.some((h: { id: string }) => h.id === holdId)).toBe(true)

    const cancel = await other.delete(`/api/holds/${holdId}`)
    expect(cancel.status()).toBe(204)

    const after = await librarian.get(`/api/holds?bookId=${book.id}`)
    const remaining = (await after.json()).holds as { id: string; status?: string }[]
    expect(remaining.filter((h) => h.id === holdId && h.status !== 'cancelled')).toHaveLength(0)

    // Tidy up so the catalog is as we found it.
    expect((await member.post(`/api/loans/${loanId}/return`, { data: {} })).status()).toBe(200)
    expect((await librarian.delete(`/api/books/${book.id}`)).status()).toBe(204)
  })

  test('loans listing is scoped by role', async () => {
    const all = await librarian.get('/api/loans?status=open')
    expect(all.status()).toBe(200)
    expect(Array.isArray((await all.json()).loans)).toBe(true)

    const own = await member.get('/api/loans')
    expect(own.status()).toBe(200)
    const loans = (await own.json()).loans as { userId?: string; user?: { id: string } }[]
    for (const l of loans) expect(l.userId ?? l.user?.id).toBe(memberId)
  })

  test('dashboard is for librarians only', async () => {
    const denied = await member.get('/api/dashboard')
    expect(denied.status()).toBe(403)

    const res = await librarian.get('/api/dashboard')
    expect(res.status()).toBe(200)
    const d = await res.json()
    for (const k of ['books', 'copies', 'openLoans', 'overdue', 'holdsWaiting']) {
      expect(typeof d[k], k).toBe('number')
    }
    expect(Array.isArray(d.recentLoans)).toBe(true)
    expect(Array.isArray(d.overdueLoans)).toBe(true)
  })
})

test.describe('realtime', () => {
  test('GET /api/books with Upgrade streams a snapshot, then an event after an edit', async ({ page, baseURL }) => {
    const librarian = await apiAs(baseURL!, { email: uniqueEmail('ws-lib'), name: 'WS Librarian', role: 'librarian' })
    const book = await createBook(librarian)

    await page.goto('/') // the WebSocket below resolves relative to the app origin

    // Open the socket in the page; resolve once an event frame follows the
    // snapshot. The evaluate is NOT awaited yet: the edit that produces the
    // event is sent from the test while the page is listening.
    const framesPromise = page.evaluate(
      ({ wait }) =>
        new Promise<{ first: { type: string; data: unknown }; event: { type: string; topic?: string; data: unknown } }>(
          (resolve, reject) => {
            const w = window as unknown as { __wsReady?: boolean }
            const s = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/books`)
            let first: { type: string; data: unknown } | undefined
            const timer = setTimeout(() => reject(new Error('no event frame within the wait')), wait)
            s.onerror = () => reject(new Error('websocket error'))
            s.onmessage = (e) => {
              const msg = JSON.parse(String(e.data))
              if (first === undefined) {
                first = msg
                w.__wsReady = true
                return
              }
              if (msg.type === 'event') {
                clearTimeout(timer)
                s.close()
                resolve({ first, event: msg })
              }
            }
          },
        ),
      { wait: 15_000 },
    )

    await page.waitForFunction(() => (window as unknown as { __wsReady?: boolean }).__wsReady === true, null, {
      timeout: 10_000,
    })

    const edit = await librarian.patch(`/api/books/${book.id}`, { data: { genre: 'Realtime' } })
    expect(edit.status(), await edit.text()).toBe(200)

    const frames = await framesPromise
    expect(frames.first.type).toBe('snapshot')
    expect(frames.first.data).toBeTruthy()
    expect(frames.event.type).toBe('event')
    expect(frames.event.topic ?? 'books').toBe('books')
    const data = frames.event.data as { action?: string; id?: string }
    expect(data.id).toBe(book.id)
    expect(data.action).toMatch(/updated|created|deleted|checkout|return/)

    expect((await librarian.delete(`/api/books/${book.id}`)).status()).toBe(204)
    await librarian.dispose()
  })
})
