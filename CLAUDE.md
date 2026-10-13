# Alexandria — The Library of Alexandria

A library management system built for the Valsoft agentic coding challenge
(`docs/CHALLENGE.md` is the spec, converted from the docx). Public repo
`github.com/bmurray/alexandria`, deployed at **https://alexandria.bmurray.ca** on
the local k3s cluster. Personal project under bmurray.ca; not part of Novega.

Read these before changing anything (gitignored, local only — canonical copies
live in `../chad`):

- [CHAD.md](./CHAD.md) — stack and conventions (Go + Pkl + React/Vite/Bun).
- [CHAD-DEPLOY.md](./CHAD-DEPLOY.md) — Flux/Gateway deployment rules.
- [docs/DESIGN.md](./docs/DESIGN.md) — domain model, API surface, roles, AI features.
- [docs/api.md](./docs/api.md) — every endpoint, inputs and outputs (e2e tests cover it).

## Scope rule

**Build what the challenge asks for, nothing speculative.** CHAD describes a
platform; this app takes its scaffolding and conventions, not its full
feature list. If the spec does not call for it, we do not build it:

| CHAD piece | Alexandria |
|---|---|
| MariaDB outside the cluster | **SQLite** (`modernc.org/sqlite`, pure Go, WAL) on a PVC. One replica, `Recreate`. |
| Redis pub/sub, work queues, GCRA rate limits | **Not built.** In-process only where a hub is needed. |
| S3 file storage | **Not built.** Cover images are URLs (Open Library). |
| Compose dev stack | **Not built.** `make dev` needs nothing but Go, Bun, Pkl. |
| OTel collector sidecar | **Direct OTLP gRPC** to `monitoring.monitoring.svc.cluster.local:4317` (this cluster's otel-lgtm). Off in dev by default. |
| AWS IRSA / Secrets Manager | **Not built.** Credentials: `Static` (dev), `File` (dev), `K8sSecret` (prod). |
| EKS / ECR pull-through | **Local k3s** (context `default`), Flux OCI artifact `oci://ghcr.io/bmurray/alexandria-manifests`, images `ghcr.io/bmurray/alexandria`. |

Everything else in CHAD applies: Pkl config with Go codegen, `CONFIG_FILE` as the
only env var, `context.Context` everywhere, `http.ServeMux` method patterns,
React + TypeScript + Zod + TanStack Query, modern CSS and no styling libraries,
documented API, e2e tests in CI, multi-arch images on `vX.Y.Z` tags only from main.

## What we build (from the spec)

- **Books**: add, edit, delete. Fields: title, author, ISBN, year, genre, publisher,
  description, cover URL, copy count, tags.
- **Check-out / check-in**: a loan has a book, a member, out/due/returned dates.
  Availability is derived (copies minus open loans). Overdue is derived from due date.
- **Search**: one query box over title, author, ISBN, genre, tags (SQLite FTS5),
  plus filters (availability, genre).
- **SSO with roles** (bonus): OIDC (Google by default; any issuer works) with
  roles `admin` > `librarian` > `member`. First sign-in from an email in
  `auth.admins` becomes admin; admins change roles. A `Dev` auth variant exists
  for local work and e2e; it must never be enabled on a public hostname.
- **AI** (bonus): (1) **Ask the Librarian** — a chat with tools over the catalog
  (search, availability, recommend, place a hold). (2) **Enrich** — fill in metadata and
  a description for a book from its title/ISBN (Open Library first, model fills gaps).
  Every model call is priced and written to the `ai_usage` ledger; providers have
  runtime spend caps and priority with fallback (CHAD costing rules).
- **Creativity**: live updates on the catalog over the same GET (websocket on
  `Upgrade`), member loan history, overdue dashboard for librarians, holds queue,
  Open Library cover lookup.

## Quick start

```
make gen       # Pkl -> Go (internal/gen), committed
make dev       # Go on :24380, proxies to Vite, CONFIG_FILE=configs/dev.pkl
make web-dev   # Vite on :24381
make test      # go test + bun test
make e2e       # Playwright against a running server (BASE_URL)
```

Dev secrets go in gitignored `configs/dev.local.pkl` (`amends "dev.pkl"`).
Never a real key in `configs/dev.pkl`. Ports 24380/24381 are unique to this
project on this host.

## Pkl layout

- `config/` is the schema package (embedded in the binary, `config/embed.go`).
  Every module carries `@go.Package`; codegen goes to `internal/gen/<package>`.
- `configs/` holds runtime files that `amends "@schema/AppConfig.pkl"`.
- One-of-many concepts use an abstract module plus one module per variant
  (never classes): `credential/`, `auth/provider/`, `ai/provider/`.
  Blank-import each generated variant package in `internal/config`.
- The discriminator `type` is `fixed` but **not** `hidden` so it survives pklbin.
- `config/deploy/k8s/{App,Manifests}.pkl` render the workload manifests;
  `devops/k3s/alexandria.pkl` is this cluster's environment file.

## Rules

- `make lint`, `make typecheck`, `make test` must pass before a commit.
- `internal/app` is the only package that imports `internal/gen`; everything
  else takes plain Go values.
- Model IDs and prices come from the `claude-api` skill, never from memory.
  Use the official Anthropic Go SDK.
- SQLite: one writer. Open with `_pragma=journal_mode(WAL)`, `busy_timeout`,
  `foreign_keys(ON)`; `SetMaxOpenConns(1)` for writes is acceptable at this scale.
- Websocket: no dedicated paths. `GET /api/books` with `Upgrade: websocket`
  streams a snapshot then change events.
- Deploy: cutting a `vX.Y.Z` tag on main is the deploy. Flux objects in
  `devops/flux/alexandria.yaml`; `make bootstrap` renders the namespace,
  ServiceAccount and Secret that Flux never owns; a human applies them with
  `kubectl --context default`.
