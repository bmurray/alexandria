# Books API

Catalog endpoints. All responses are JSON. Errors are `{"error": "..."}`;
validation failures are `400 {"error": "validation failed", "problems": [...]}`.
State-changing requests must send `Content-Type: application/json`.

Roles: `anonymous < member < librarian < admin`. "librarian" below means
librarian or admin. Missing sign-in is `401`, insufficient role is `403`.

## Book object

```json
{
  "id": "k7f2m9q4bx1n8p3rwz5c",
  "title": "Dune", "author": "Frank Herbert", "isbn": "9780441013593",
  "year": 1965, "genre": "Science Fiction", "publisher": "Ace",
  "description": "…", "coverUrl": "https://covers.openlibrary.org/b/isbn/9780441013593-M.jpg",
  "copies": 3, "tags": ["space opera", "ecology"],
  "createdAt": "2026-10-01T12:00:00Z", "updatedAt": "2026-10-01T12:00:00Z",
  "available": 2, "onLoan": 1, "holdsWaiting": 0
}
```

`available`, `onLoan` and `holdsWaiting` are derived on every read:
`available = copies − open loans`. `year` is `null` when unknown; `tags` is
always an array. Times are RFC3339 UTC.

Validation (POST and PATCH): `title` and `author` required (trimmed);
`copies` 1–1000 and not below the number currently on loan; `year` 0–2100 or
null; `isbn` empty, 10 characters (digits, final `X` allowed) or 13 digits
after dashes and spaces are stripped (stored stripped); `coverUrl` empty or
an http(s) URL; `tags` are trimmed, lower-cased and de-duplicated (max 20 of
50 chars); `title` ≤ 300, `author`/`publisher` ≤ 200, `genre` ≤ 100,
`description` ≤ 5000 characters.

## `GET /api/books` — list and search

Auth: none.

| query | meaning |
|---|---|
| `q` | free text over title, author, ISBN, genre, tags and description. Each word is a prefix match (`herb` finds Herbert); words are ANDed. Results are ranked by relevance (bm25) and `sort` is ignored. |
| `genre` | exact genre, case-insensitive |
| `available=true` | only books with at least one free copy |
| `sort` | `title` (default), `author`, `year` (newest first, unknown last), `added` (newest first) |
| `limit` | page size, default 24, max 100 |
| `offset` | default 0 |

```json
{ "books": [ Book, … ], "total": 60 }
```

`total` is the size of the whole result set, not the page.

**Live updates.** The same URL with `Upgrade: websocket` opens a stream: the
first frame is `{"type":"snapshot","data":{"books":[…],"total":n}}`, then one
`{"type":"event","topic":"books","data":{"action":"created|updated|deleted|checkout|return|hold","id":"<bookId>"}}`
per change anywhere in the catalog. Clients re-fetch on events. Sending
`{"type":"ping"}` gets `{"type":"pong"}`.

## `GET /api/books/{id}` — one book

Auth: none, but the response depends on the caller.

```json
{ "book": Book, "loans": [ Loan, … ], "holds": [ Hold, … ] }
```

- librarian: `loans` are the book's open loans, `holds` its active queue in position order.
- member: only the caller's own loans (open first, then history) and active holds on this book.
- anonymous: both arrays empty.

`404` when the id is unknown.

## `POST /api/books` — add a book

Auth: librarian. Body: any Book fields except the derived ones and ids/times.

```json
{ "title": "Dune", "author": "Frank Herbert", "isbn": "978-0-441-01359-3",
  "year": 1965, "genre": "Science Fiction", "publisher": "Ace",
  "description": "…", "coverUrl": "https://…", "copies": 3, "tags": ["Space Opera"] }
```

`201 { "book": Book }`. `400` with `problems` on validation; `400 invalid JSON body`
on malformed or unknown fields.

## `PATCH /api/books/{id}` — edit a book

Auth: librarian. Body: any subset of the POST fields; only present fields
change. Send `"year": null` to clear the year (omitting it leaves it alone).
Raising `copies` can promote a waiting hold to ready.

`200 { "book": Book }`. `400` on validation (including `copies` below open
loans), `404` unknown id.

## `DELETE /api/books/{id}` — remove a book

Auth: librarian. `204` on success. `409 {"error":"book has N open loan(s); return them first"}`
while copies are out. Active holds and returned-loan history for the book are
removed with it.

## `GET /api/genres` — genres in use

Auth: none. `{ "genres": [ {"name": "Fiction", "count": 16}, … ] }`, sorted by name.
