# Auth API

Sign-in, sessions, roles and the public configuration endpoint. Served by
`internal/handler/authn` on top of `internal/auth`. All bodies are JSON;
errors are `{"error": "message"}`.

## The session cookie

Signing in sets one cookie, `alexandria_session` by default (`auth.cookieName`
in Pkl):

| Attribute | Value |
|---|---|
| `HttpOnly` | always |
| `Path` | `/` |
| `SameSite` | `Lax` |
| `Secure` | when `auth.secureCookie` is on (production, `https://` public URL) |
| `Max-Age` | `auth.sessionDuration` (default 30 days) |

The cookie value is a random 256-bit token. The server stores only its SHA-256
in `sessions.id`, so a database copy cannot be replayed as a cookie. A session
expires `sessionDuration` after it was created (no sliding renewal);
`last_seen_at` is refreshed at most once per five minutes. Expired sessions and
sign-in challenges are deleted by a background sweeper.

Every request runs through the session middleware: with a valid cookie the
request carries the user, otherwise it is anonymous. Endpoints that need a role
answer `401 {"error":"not signed in"}` for anonymous callers and
`403 {"error":"<role> role required"}` when the role is too low. State-changing
requests must send `Content-Type: application/json` (the CSRF guard).

## Roles

`admin` > `librarian` > `member`. See `docs/DESIGN.md` for what each may do.

How a role is assigned:

1. A new user starts as `member`.
2. On **every** sign-in, if the email is listed in `auth.admins` (case-insensitive)
   the user becomes `admin`. Removing an email from the list does not demote
   anyone; an admin does that with `PATCH /api/users/{id}`.
3. An admin can set any user's role with `PATCH /api/users/{id}`, except their
   own (an admin may not demote themselves, so there is always one left).
4. The Dev sign-in may pass `role` to create or update a user with that role
   (handy in e2e). The admin list still wins.

Email rules on sign-in: trimmed and lowercased; must contain one `@` with
non-empty parts; when `auth.allowedEmailDomains` is set the domain must match
one of them exactly (`403 {"error":"that email domain may not sign in"}`).
The user's display name falls back to the local part of the email.

## Account linking

An identity is `(provider, subject)` — `("google", "1091…")` or `("dev", email)`.
On sign-in the server looks for the identity; if none exists, for a user with
the same email, and links the identity to them; otherwise it creates a user.
So someone who used the Dev sign-in locally and then Google gets one account.

## User object

```json
{
  "id": "k3m9x2c7h4w8p1q6r5t0",
  "email": "ann@example.com",
  "name": "Ann Example",
  "picture": "https://lh3.googleusercontent.com/…",
  "role": "member",
  "createdAt": "2026-10-13T12:00:00Z",
  "lastLoginAt": "2026-10-13T12:00:00Z"
}
```

---

## `GET /api/config`

What the web app needs before anyone signs in. Public.

**Response** `200`

```json
{
  "libraryName": "The Library of Alexandria",
  "providers": [
    { "name": "google", "displayName": "Google", "type": "oidc" },
    { "name": "dev", "displayName": "Development", "type": "dev" }
  ],
  "aiEnabled": true,
  "loanDays": 21
}
```

`providers` lists OIDC providers in configuration order, then `dev` only when
the Dev sign-in is enabled. The login page renders one button per entry:
`oidc` entries link to `/api/auth/oidc/{name}/start`, `dev` shows the email form.

## `GET /api/auth/me`

The signed-in user.

**Response** `200 {"user": User}` or `401 {"error":"not signed in"}`.

## `POST /api/auth/logout`

Deletes the current session and clears the cookie. No body.

**Response** `204`, always (also when not signed in).

## `GET /api/auth/oidc/{provider}/start?returnTo=/path`

Begins an OpenID Connect sign-in (authorization code + PKCE S256 + nonce).
The server stores the state, nonce, code verifier and `returnTo` as a
challenge valid for 10 minutes, then redirects the browser to the identity
provider. Navigate to it with a top-level link, not `fetch`.

**Inputs**

| Name | In | Rules |
|---|---|---|
| `provider` | path | a `name` from `/api/config` with type `oidc` |
| `returnTo` | query | optional; a site-relative path (`/…`, not `//…`); default `/` |

**Responses**

| Status | When |
|---|---|
| `302` | `Location` is the provider's authorization URL |
| `400` | `returnTo` is not a path on this site |
| `404` | unknown provider (or `dev`, which has no OIDC flow) |
| `502` | the provider's discovery document could not be fetched |

The redirect URI registered at the IdP must be
`<publicUrl>/api/auth/oidc/<provider>/callback`.

## `GET /api/auth/oidc/{provider}/callback?state=&code=`

Where the identity provider sends the browser back. The server consumes the
challenge for `state` (single use), redeems `code`, verifies the ID token
(issuer, audience, expiry, nonce, RS256 signature), signs the user in, sets
the session cookie and redirects to the web app.

**Responses** (always `302`; the browser came from the IdP, so a JSON body
would be a dead end)

| `Location` | When |
|---|---|
| `/auth/callback?returnTo=<path>` | success; the cookie is set on this response |
| `/auth/callback?error=<message>` | anything else |

Error messages are the sentinel texts below, or what the IdP sent in
`error_description` when it declined (for example the user cancelled):

| Message | Cause |
|---|---|
| `sign-in attempt expired; try again` | unknown, reused or expired `state`, or missing `code` |
| `sign-in with the identity provider failed` | the token exchange or ID-token verification failed |
| `that email domain may not sign in` | `allowedEmailDomains` |
| `invalid email address` | the ID token carried no usable email |
| `unknown sign-in provider` | no such provider |
| `sign-in failed` | an internal error; detail is in the server log |

## `POST /api/auth/dev/login`

Password-less sign-in for local development and e2e. Only when the `Dev` auth
variant is enabled in Pkl; it must never be on for a public hostname.

**Request**

```json
{ "email": "ann@example.com", "name": "Ann", "role": "librarian" }
```

| Field | Rules |
|---|---|
| `email` | required; same rules as any sign-in |
| `name` | optional; defaults to the email's local part for new users, left alone when blank for existing users |
| `role` | optional; `member`, `librarian` or `admin`; written to the user when given |

**Responses**

| Status | Body |
|---|---|
| `200` | `{"user": User}`, and the session cookie is set |
| `400` | invalid JSON, unknown field, invalid email or role |
| `403` | email domain not allowed |
| `404` | Dev sign-in is disabled (indistinguishable from an unknown route) |

## `GET /api/users` (admin)

All users ordered by creation time.

**Response** `200 {"users": [User, …]}`; `401` / `403` as above.

## `PATCH /api/users/{id}` (admin)

Changes a user's role.

**Request** `{"role": "member" | "librarian" | "admin"}`

**Responses**

| Status | When |
|---|---|
| `200` | `{"user": User}` with the new role |
| `400` | invalid role or JSON |
| `404` | no such user |
| `409` | an admin trying to change their own role (`an admin may not change their own role`) |
| `401` / `403` | not signed in / not an admin |

The change is live immediately: every request re-reads the user row through
the session, so the affected person's next request sees the new role. It does
not end their sessions.

## Observability

OTel counters `alexandria.auth.logins` and `alexandria.auth.failures`, both
with a `provider` attribute (`google`, `dev`, …). A span `auth.signIn` wraps
every sign-in and `auth.OIDCCallback` the callback leg. Sign-ins and role
changes are logged at info with the user id, never the email.
