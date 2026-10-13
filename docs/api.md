# API

All endpoints live under `/api` and speak JSON. The e2e suite
(`e2e/api.spec.ts`) exercises everything documented here.

| Area | Reference |
|---|---|
| Sign-in, sessions, users and roles | [api/auth.md](api/auth.md) |
| Books, search, genres, enrich and lookup | [api/books.md](api/books.md) |
| Loans, holds, dashboard | [api/loans.md](api/loans.md) |
| Ask the Librarian, usage and providers | [api/ai.md](api/ai.md) |

## Conventions

**Authentication.** A cookie session (`alexandria_session`; HttpOnly,
SameSite=Lax, Secure in production). `GET /api/auth/me` returns the current
user or `401`. Roles are ordered `admin > librarian > member`; an endpoint
that needs a role answers `401` when anonymous and `403` when signed in
below that role.

**Errors.** Every error is a JSON object:

```json
{ "error": "book is not available" }
```

Validation failures add a list the UI can show per field:

```json
{ "error": "invalid book", "problems": ["title is required", "copies must be at least 1"] }
```

| Status | Meaning |
|---|---|
| `400` | malformed JSON or validation failure (`problems` present) |
| `401` | not signed in |
| `403` | signed in, insufficient role (or not the owner) |
| `404` | no such resource |
| `409` | state conflict: no copies available, hold on an available book, delete with open loans, duplicate open loan |
| `415` | see CSRF below |

**CSRF.** Every state-changing request (`POST`, `PATCH`, `DELETE`) must
carry `Content-Type: application/json`; anything else is rejected with
`415` before the handler runs. Browsers cannot send that content type
cross-site without a CORS preflight, which the API never grants, so the
session cookie alone cannot be replayed from another origin. Send `{}` when
there is no body.

**Pagination.** List endpoints accept `limit` (default 50, max 200) and
`offset`, and return `total` alongside the page.

**Times** are RFC 3339 UTC strings. **IDs** are opaque strings.

**Realtime.** `GET /api/books` with `Upgrade: websocket` keeps the same
URL and query parameters but streams instead of answering once: first a
`{"type":"snapshot","data":{ ...the normal GET body... }}` frame, then a
`{"type":"event","topic":"books","data":{"action":"created|updated|deleted|checkout|return","id":"..."}}`
frame for every change. There are no dedicated websocket paths.

## Health

| Method and path | Answer |
|---|---|
| `GET /api/healthz` | `200 {"ok": true, "version": "v0.1.0"}` — process is up (liveness) |
| `GET /api/readyz` | `200` once the database answers; `503` otherwise (readiness) |
| `GET /api/config` | `200 {"libraryName", "providers": ["google","dev"...], "aiEnabled", "loanDays"}` — what the UI needs before sign-in |
