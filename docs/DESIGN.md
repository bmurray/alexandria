# Alexandria — design

The Library of Alexandria: a small, polished library management system.
Spec: `docs/CHALLENGE.md`. Conventions: `CLAUDE.md`, `CHAD.md`.

## Roles

| Role | May |
|---|---|
| anonymous | browse and search the catalog, see availability, use **Ask the Librarian** (read-only tools) |
| `member` | everything above, plus check out / return their own loans, place and cancel their own holds, see their loan history |
| `librarian` | everything above, plus add / edit / delete books, check out and return **for any member**, see all loans, the overdue dashboard, enrich books with AI, manage holds |
| `admin` | everything above, plus list users and change roles, see AI spend and provider state, toggle providers |

Roles are ordered: `admin > librarian > member`. `RequireRole(min)` in middleware.
A user's role is stored on the `users` row. On first sign-in, an email listed in
`auth.admins` (Pkl) gets `admin`; everyone else gets `member`.

## Data model (SQLite)

```
users          id TEXT PK, email TEXT UNIQUE, name TEXT, picture TEXT, role TEXT,
               created_at, last_login_at
identities     id PK, user_id FK, provider TEXT, subject TEXT, UNIQUE(provider, subject)
sessions       id PK (sha256 of token), user_id FK, user_agent, ip, created_at, last_seen_at, expires_at
challenges     id PK, kind TEXT, payload TEXT(json), expires_at         -- OIDC state + PKCE
books          id TEXT PK, title, author, isbn, year INT NULL, genre, publisher,
               description, cover_url, copies INT (>=1), tags TEXT (json array),
               created_at, updated_at, created_by FK users
books_fts      FTS5 virtual table (title, author, isbn, genre, tags, description), content=books
loans          id PK, book_id FK, user_id FK, checked_out_at, due_at, returned_at NULL,
               checked_out_by FK users (who performed it), returned_by FK users NULL
holds          id PK, book_id FK, user_id FK, placed_at, status TEXT (waiting|ready|fulfilled|cancelled),
               UNIQUE(book_id, user_id) WHERE status IN ('waiting','ready')
ai_providers   name TEXT PK, enabled INT, priority INT, spend_limit_usd REAL NULL, spent_usd REAL
ai_usage       id PK, provider, model, purpose TEXT, input_tokens, output_tokens, cache_read_tokens,
               cost_usd REAL, user_id NULL, created_at
ai_conversations id PK, user_id NULL (anonymous allowed, keyed by cookie), title, created_at, updated_at
ai_messages    id PK, conversation_id FK, role TEXT (user|assistant|tool), content TEXT (json blocks), created_at
schema_migrations version PK, applied_at
```

IDs are 20-char base32 random strings (`internal/id`). Times are RFC3339 UTC strings
(`TEXT`) so they round-trip cleanly through `database/sql` and JSON.

Derived values (never stored): `available = copies - count(open loans)`,
`overdue = returned_at IS NULL AND due_at < now`.

Rules:
- Check-out fails with 409 when `available == 0`, or when the member already has
  an open loan on that book, or when a `ready` hold exists for another member.
- Loan period is `library.loanDays` (default 21). Return sets `returned_at`; if a
  `waiting` hold exists, the oldest becomes `ready` (the book is reserved for them).
- A hold may only be placed when `available == 0`. Placing a hold on an available
  book answers 409 with "book is available; check it out instead".
- Deleting a book with open loans answers 409.

## API

All JSON under `/api`. Errors are `{"error": "message"}` and, for validation,
`{"error": "...", "problems": ["title is required", ...]}`. Cookie session
(`alexandria_session`, HttpOnly, SameSite=Lax, Secure in prod). State-changing
requests must be `application/json` (CSRF guard). The full reference with
request/response shapes is `docs/api.md`; this is the index.

### Auth
```
GET  /api/config                        { providers: ["google"|"dev"...], aiEnabled: bool, loanDays: int }
GET  /api/auth/me                       { user } | 401
POST /api/auth/logout                   204
GET  /api/auth/oidc/{provider}/start?returnTo=/path   302 to the IdP
GET  /api/auth/oidc/{provider}/callback 302 to /auth/callback?returnTo=...  (or ?error=)
POST /api/auth/dev/login                { email, name, role? }  -> { user }   (Dev provider only; 404 otherwise)
```
### Users (admin)
```
GET   /api/users                        { users: [ user ] }
PATCH /api/users/{id}                   { role } -> { user }
```
### Books
```
GET    /api/books?q=&genre=&available=true&sort=title|author|year|added&limit=&offset=
                                        { books: [ bookWithAvailability ], total }
                                        Upgrade: websocket -> frames {type:"snapshot",data:{...}} then {type:"event",topic:"books",data:{action,id}}
GET    /api/books/{id}                  { book, loans: [ open loans (librarian) | mine ], holds: [...] }
POST   /api/books                       (librarian) book fields -> 201 { book }
PATCH  /api/books/{id}                  (librarian) partial fields -> { book }
DELETE /api/books/{id}                  (librarian) 204 | 409
GET    /api/genres                      { genres: [ {name, count} ] }
POST   /api/books/{id}/enrich           (librarian, AI) -> { suggestion: {author?, year?, genre?, publisher?, description?, coverUrl?, tags?}, source: "openlibrary"|"model"|"both" }
POST   /api/books/lookup                (librarian) { isbn } | { title, author? } -> { suggestion } (Open Library only, no model)
```
### Loans and holds
```
POST   /api/books/{id}/checkout         (member) { userId? (librarian may set) } -> 201 { loan }
POST   /api/loans/{id}/return           (member: own; librarian: any) -> { loan }
GET    /api/loans?status=open|overdue|returned&userId=&limit=&offset=  (librarian; members get their own)
                                        { loans: [ loanWithBook ], total }
GET    /api/me/loans                    { loans: [ loanWithBook ] }   (open + history)
POST   /api/books/{id}/holds            (member) -> 201 { hold }
DELETE /api/holds/{id}                  (member: own; librarian: any) 204
GET    /api/holds?bookId=               (librarian) { holds: [ holdWithBookAndUser ] }
GET    /api/dashboard                   (librarian) { books, copies, openLoans, overdue, holdsWaiting, recentLoans: [...] , overdueLoans: [...] }
```
### AI
```
POST   /api/ai/chat                     { conversationId?, message } -> { conversationId, reply, toolCalls: [{name,input,output}], usage: {inputTokens,outputTokens,costUsd,provider} }
GET    /api/ai/conversations            { conversations: [ {id,title,updatedAt} ] }  (own)
GET    /api/ai/conversations/{id}       { conversation, messages: [...] }
DELETE /api/ai/conversations/{id}       204
GET    /api/ai/usage                    (admin) { providers: [ providerState ], totals: {today, allTime}, byPurpose: {...} }
PATCH  /api/ai/providers/{name}         (admin) { enabled?, priority?, spendLimitUsd? } -> { provider }
```
### Health
```
GET /api/healthz   { ok: true, version }
GET /api/readyz    200 when the database answers
```

## Ask the Librarian (tools)

System prompt: a friendly, concise librarian for this specific catalog. Tools
(all read-only for anonymous; the last two need a signed-in member):

| tool | input | does |
|---|---|---|
| `search_books` | `{query, genre?, availableOnly?, limit?}` | FTS search, returns id/title/author/year/genre/available |
| `get_book` | `{id}` | full record plus availability and queue length |
| `list_genres` | `{}` | genres with counts |
| `recommend` | `{likeBookId?, interests?}` | catalog search driven by the model's own query expansion |
| `my_loans` | `{}` | the caller's open loans and due dates |
| `place_hold` | `{bookId}` | places a hold for the caller |

Provider routing: `ai.providers` is a Pkl `Listing<Provider>`; each has pricing,
priority and an initial spend cap. The `ai_providers` table holds runtime state;
the service picks the lowest priority number that is enabled and under its cap,
and falls through on error or cap. Every call writes an `ai_usage` row with
`purpose` (`chat`, `enrich`) and bumps `ai_providers.spent_usd`; OTel counters
`alexandria.ai.tokens` and `alexandria.ai.cost_usd` carry `provider` and `purpose`.

Default providers (OpenAI list prices, 2026-10, chosen cheap): `gpt-6-luna`
($0.10 / $0.50 per MTok, cache read $0.01) priority 10 with a cap, then
`gpt-5-nano` ($0.05 / $0.40) priority 20 as the fallback. Variants:
`ai/provider/OpenAI.pkl` (Chat Completions, also OpenAI-compatible servers)
and `ai/provider/Anthropic.pkl` (Messages API via the official SDK).

## Enrich

`POST /api/books/{id}/enrich`: look the ISBN (or title+author) up on Open Library
(`https://openlibrary.org/api/books?bibkeys=ISBN:…&format=json&jscmd=data` and
`/search.json`), then ask the model for a two-sentence description, genre and
tags given what was found. Returns a **suggestion**; the librarian applies it
with a normal PATCH. Nothing is written silently.

## Realtime

`internal/realtime` has an in-process hub (topic -> subscribers). `GET /api/books`
with `Upgrade: websocket` sends a snapshot frame, then an event frame per change
(`{"action":"created|updated|deleted|checkout|return","id":"…"}`); the web app
invalidates its query on events. No separate websocket paths.

## Web

Routes: `/` (catalog + search), `/books/:id`, `/books/new` and `/books/:id/edit`
(librarian), `/loans` (librarian: all; member: mine), `/dashboard` (librarian),
`/holds` (librarian), `/users` (admin), `/ai` (admin spend), `/login`,
`/auth/callback`, `/account`. The **Ask the Librarian** panel is a drawer on
every page.

Design: warm paper and ink palette, serif display face for headings, clean
system sans for UI, generous whitespace, cover thumbnails, availability pills.
Modern CSS only (custom properties, grid, container queries), no libraries.

## Package layout

```
cmd/alexandria/main.go          signal ctx, config, otel, db, wiring, server
internal/app                    the only importer of internal/gen: builds services from config
internal/config                 CONFIG_FILE loader (pkl-loader) + validation
internal/credential             Static / File / K8sSecret resolver
internal/db                     sqlite open + embedded migrations
internal/id                     random ids
internal/otel                   providers (traces, metrics, logs via otelslog)
internal/oidc                   stdlib OIDC relying party (discovery, PKCE, RS256)
internal/auth                   users, identities, sessions, roles, dev login
internal/library                books, loans, holds, search, dashboard (domain + sqlite store)
internal/openlibrary            Open Library client
internal/ai                     providers (OpenAI HTTP, Anthropic SDK), router/budget, ledger, chat with tools, enrich
internal/realtime               in-process hub + websocket on GET
internal/middleware             session, roles, JSON-only CSRF, logging, recover
internal/handler                HTTP handlers (one file per area) + json helpers
internal/server                 mux assembly, SPA/dev proxy, graceful shutdown
config/                         Pkl schema (embedded)
configs/                        dev.pkl, e2e.pkl, *.local.pkl (gitignored)
web/                            React app (embedded via web/embed_prod.go, -tags dev skips)
e2e/                            Playwright specs against a running server
devops/                         k3s environment pkl, flux objects
deploy/                         secrets.sh (bootstrap YAML), out/ (gitignored)
```
