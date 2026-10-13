# Alexandria

A small, polished library management system: catalog, loans, search, and a
librarian you can talk to. Built for the Valsoft agentic coding challenge
(`docs/CHALLENGE.md`).

**Live:** https://alexandria.bmurray.ca

## Features

Core (the spec):

- **Books** — add, edit, delete; title, author, ISBN, year, genre, publisher,
  description, cover image, copy count, tags.
- **Check-out / check-in** — a loan has a book, a member, out/due/returned
  dates. Availability is derived from copies minus open loans; overdue is
  derived from the due date.
- **Search** — one box over title, author, ISBN, genre and tags (SQLite
  FTS5), with availability and genre filters.

Bonus:

- **Sign-in with roles** — Google (any OpenID Connect issuer works) with
  `admin > librarian > member`. Admins promote users; the first sign-in from a
  configured email is an admin.
- **Ask the Librarian** — a chat over the catalog with tools (search,
  availability, recommend, place a hold).
- **Enrich** — fill in metadata and a description for a book from its
  title/ISBN: Open Library first, the model fills the gaps; the librarian
  reviews before anything is saved.
- **Cost caps** — every model call is priced into a ledger; providers have
  spend caps and priority with fallback (GPT-6 Luna until its cap, then GPT-5 nano).
- **Live updates** — `GET /api/books` with `Upgrade: websocket` streams the
  catalog and change events; the UI never polls.
- **Holds** — queue for an unavailable book; a return hands it to the next
  person.
- **Dashboard** — open and overdue loans, holds waiting, recent activity.

## Stack, and why

| Piece | Why |
|---|---|
| **Go** (`net/http` with method patterns, no framework) | One static binary, stdlib mux is enough for a REST API plus a websocket upgrade. |
| **SQLite** (`modernc.org/sqlite`, pure Go, WAL) + **FTS5** | One replica, one writer, zero operational surface; full-text search comes with the engine. |
| **Pkl** config with Go codegen | Typed, validated config with defaults and one-of-many credential sources; the same schema renders the Kubernetes manifests. `CONFIG_FILE` is the only env var. |
| **React + TypeScript** (Vite, Bun, TanStack Query, Zod, React Router) | Validated data at the boundary, cache-driven UI, no styling library — modern CSS. |
| **OpenAI Chat Completions** (stdlib HTTP; an Anthropic SDK variant too) | Tool use for the librarian; token counts come back priced per provider. |
| **OpenTelemetry** | Traces, metrics (incl. AI cost counters) and `slog` logs over OTLP to the cluster's Grafana stack. |
| **Flux GitOps** on k3s | A `vX.Y.Z` tag is the deploy: image + rendered manifests as an OCI artifact; nothing applied by hand after bootstrap. |

## Run it locally

Prerequisites: Go 1.26, [Bun](https://bun.sh), [Pkl](https://pkl-lang.org) 0.32.

```
make gen        # Pkl -> Go types (committed; only needed after schema edits)
make dev        # Go API on http://localhost:24380, proxying the UI to Vite
make web-dev    # Vite on :24381 (second terminal)
```

Open http://localhost:24380. The dev build offers a **development sign-in**
form (any email, name and role); the catalog is seeded with a starter set
of classics on first start.

Google sign-in and the AI features need keys. Put them in gitignored
`configs/dev.local.pkl` and run `CONFIG_FILE=configs/dev.local.pkl make dev`:

```pkl
amends "dev.pkl"

// Google OAuth client (redirect URI http://localhost:24380/api/auth/oidc/google/callback).
// The Google OAuth client lives in ~/.config/secrets/valsoft.json as {"client_id": ..., "client_secret": ...}
auth {
  providers {
    [1] { enabled = true; clientId = "1234-abcd.apps.googleusercontent.com" }
  }
}

// OpenAI key in ~/.openai/auth.json as {"key": "sk-..."}; Google client in ~/.config/secrets/valsoft.json
ai {
  enabled = true
}
```

## Test

```
make lint typecheck   # go vet + eslint, go build + tsc
make test             # go test -race, bun test
make e2e              # Playwright against a running server (BASE_URL, default :24380)
```

The e2e suite covers everything in `docs/api.md` plus a browser smoke
pass. CI runs it against the production binary with `configs/e2e.pkl`:

```
make build && CONFIG_FILE=configs/e2e.pkl ./bin/alexandria &
make e2e-install && make e2e
```

## Deploy

Pushing a `vX.Y.Z` tag on `main` is the deploy (`.github/workflows/release.yml`):

1. the full suite (Go, web, e2e) runs;
2. a multi-arch image (`linux/amd64`, `linux/arm64`, no QEMU) is pushed as
   `ghcr.io/bmurray/alexandria:X.Y.Z`;
3. the Kubernetes manifests are rendered from Pkl against that image and
   pushed as a Flux OCI artifact, `oci://ghcr.io/bmurray/alexandria-manifests:X.Y.Z`;
4. Flux on the cluster picks up the newest semver and reconciles.

Secrets never enter git or the artifact; `make bootstrap` renders the
Namespace, ServiceAccount and Secret for a human to apply once. The one-time
setup (Google OAuth client, secrets files, data directory) is in
[devops/README.md](devops/README.md).

## Layout

```
cmd/alexandria/      entrypoint
internal/            app wiring, config loader, auth, library domain, AI, realtime hub, handlers, server
internal/gen/        Pkl -> Go codegen (committed)
config/              Pkl schema (embedded in the binary); config/deploy/k8s renders manifests
configs/             runtime configs: dev.pkl, e2e.pkl, *.local.pkl (gitignored)
web/                 React app (Vite + Bun); embedded in the production binary
e2e/                 Playwright suite
devops/              k3s environment file, Flux objects, deploy notes
deploy/              secrets.sh (bootstrap YAML); out/ is gitignored
docs/                DESIGN.md, api.md, CHALLENGE.md
```

Design notes: [docs/DESIGN.md](docs/DESIGN.md). API reference: [docs/api.md](docs/api.md).
