# AI API — Ask the Librarian and Enrich

Everything here is served by `internal/handler/assistant` over `internal/ai`.
Errors are `{"error": "message"}`; validation errors add `"problems": [...]`.
State-changing requests must be `application/json` (CSRF guard).

Ownership of chat threads: a signed-in user owns threads under their user id.
Anonymous visitors get a cookie `alexandria_anon` (random token, 30 days,
`HttpOnly`, `SameSite=Lax`, `Secure` behind TLS) on their first `POST
/api/ai/chat`; their threads are keyed `anon:<token>`. Signing in does not
migrate anonymous threads.

## Endpoints

### `POST /api/ai/chat` — anyone

Send one message to the librarian. Starts a thread when `conversationId` is
omitted; the thread's title is the first 60 characters of that message.

Request:
```json
{ "conversationId": "optional thread id", "message": "Do you have Dune?" }
```
`message` is required and at most 4000 characters (400 otherwise).

Response `200`:
```json
{
  "conversationId": "k3j…",
  "reply": "We have Dune by Frank Herbert, but both copies are out. Want me to place a hold?",
  "toolCalls": [
    { "name": "search_books", "input": {"query": "dune"}, "output": "{\"books\":[…],\"count\":1}" }
  ],
  "usage": { "inputTokens": 812, "outputTokens": 96, "costUsd": 0.005168, "provider": "anthropic-opus", "model": "claude-opus-5-5" }
}
```
`usage` sums every model round of this turn; `provider`/`model` are the ones
that produced the final reply (a turn can fall through mid-way).

Errors: `404` unknown or foreign `conversationId`; `429` `daily AI budget
reached`; `503` `the librarian is away (no AI provider available)` when AI is
disabled or every provider is off, capped or failing.

### `GET /api/ai/conversations` — own threads

`200 { "conversations": [ { "id", "title", "createdAt", "updatedAt" } ] }`,
most recently updated first. Without a session or anonymous cookie the list
is empty.

### `GET /api/ai/conversations/{id}` — own thread

```json
{
  "conversation": { "id", "title", "createdAt", "updatedAt" },
  "messages": [
    { "id", "role": "user", "content": "Do you have Dune?", "createdAt" },
    { "id", "role": "assistant", "content": "We have…", "toolCalls": [ {name,input,output} ], "createdAt" }
  ]
}
```
`404` when the thread does not exist or belongs to someone else (never
distinguished).

### `DELETE /api/ai/conversations/{id}` — own thread

`204`; `404` as above. Messages cascade.

### `GET /api/ai/usage` — admin

```json
{
  "providers": [
    { "name": "anthropic-opus", "model": "claude-opus-5-5", "enabled": true, "priority": 10,
      "spendLimitUsd": 10, "spentUsd": 3.21, "exceeded": false }
  ],
  "totals": { "todayUsd": 0.42, "allTimeUsd": 3.21, "calls": 57 },
  "byPurpose": [
    { "purpose": "chat", "calls": 50, "costUsd": 3.0, "inputTokens": 400000, "outputTokens": 25000 },
    { "purpose": "enrich", "calls": 7, "costUsd": 0.21, "inputTokens": 9000, "outputTokens": 1800 }
  ]
}
```
`todayUsd` is the UTC calendar day.

### `PATCH /api/ai/providers/{name}` — admin

Runtime control over one provider. Every field is optional; at least one is
required.
```json
{ "enabled": false, "priority": 5, "spendLimitUsd": 25 }
{ "spendLimitUsd": null }
```
`spendLimitUsd: null` removes the cap. Response `200 { "provider": providerState }`
(same shape as in `/api/ai/usage`). `404` unknown provider, `400` negative cap.
Edits persist in `ai_providers` across restarts; the Pkl values are only the
initial seed.

### `POST /api/ai/enrich` — librarian

`docs/DESIGN.md` sketches this as `POST /api/books/{id}/enrich`. It is
implemented as `POST /api/ai/enrich` taking the current form values instead,
so it works for a book that has not been saved yet and for edits in
progress. Nothing is written: the web app shows the suggestion and the
librarian applies what they like with a normal `PATCH /api/books/{id}`.

Request (`title` or `isbn` required):
```json
{ "title": "Dune", "author": "Frank Herbert", "isbn": "9780441013593", "description": "" }
```
Response `200`:
```json
{
  "suggestion": {
    "author": "Frank Herbert", "year": 1965, "genre": "Science fiction", "publisher": "Chilton",
    "description": "Two sentences…", "coverUrl": "https://covers.openlibrary.org/b/id/…-L.jpg",
    "isbn": "9780441013593", "tags": ["desert", "politics", "ecology"]
  },
  "source": "both"
}
```
Only fields that were found are present. `source` is `openlibrary` (no
provider was eligible, or the model failed; Open Library subjects stand in as
tags), `model` (Open Library had nothing) or `both`. `404` when neither had
anything, `429` daily budget (only when Open Library also had nothing).

### `POST /api/books/lookup` — librarian

Open Library only; never calls a model, so it is free.
```json
{ "isbn": "9780441013593" }          or          { "title": "Emma", "author": "Austen" }
```
`200 { "suggestion": {…as above, without genre/description…}, "source": "openlibrary" }`;
`404` when nothing was found; `400` when both `isbn` and `title` are blank.
ISBN is tried first and falls back to the title search.

## The librarian's tools

The model sees this tool set on every chat call. All are read-only except
`place_hold`. The two member tools answer "You need to sign in for that."
when the chat is anonymous (as a tool result, so the model relays it).

| tool | input | returns |
|---|---|---|
| `search_books` | `{query, genre?, availableOnly?, limit?}` (limit 1–20, default 8) | `{books: [{id,title,author,year,genre,available,copies,tags}], count}` |
| `get_book` | `{id}` | the full record plus `holdsWaiting`, or `{error}` |
| `list_genres` | `{}` | `{genres: [{name,count}]}` |
| `recommend` | `{likeBookId?, interests?}` | `{seed?, candidates: [...]}` — the liked book's genre, tags and author plus the interests each drive a search; results are merged, deduped and capped at 8. The model writes the reasons. |
| `my_loans` | `{}` | `{loans: [{loanId,bookId,title,author,dueAt,overdue}]}` for the signed-in member |
| `place_hold` | `{bookId}` | `{placed: true, hold: {holdId,bookId,position}}` or `{placed: false, error}` (book available, duplicate hold, …) |

Tool schemas are JSON Schema objects with `additionalProperties: false` and
are sent with `strict: true`, so inputs always match. Tool outputs are
single-line JSON. The system prompt adds today's date and whether (and as
whom) the person is signed in.

## How a chat turn runs

1. Reject an empty or >4000 character message; refuse with `503` if no
   provider is eligible and `429` if the daily budget is spent.
2. Load the thread (or create it), replay the last `ai.historyTurns`
   messages as plain text turns, store the user message.
3. Loop: call the model with the tools; when it asks for tools, run them all,
   send every result back in one user turn, repeat. At most
   `ai.maxToolRounds` tool rounds; one final call then goes out without
   tools so the model must answer with what it gathered. A `refusal` stop
   reason yields "I can't help with that one."
4. Store the assistant message with its tool calls, bump the thread's
   `updatedAt`, and return the reply with the summed usage.

Each model call (not each turn) is routed and priced independently, so a
single turn can cross a cap and finish on the fallback provider.

## Routing, caps and the ledger

- `ai.providers` (Pkl) is a list. On start-up each one is inserted into
  `ai_providers` with `INSERT OR IGNORE`, so `enabled`, `priority` and
  `spendLimitUsd` from Pkl are only the initial values; admin edits win and
  survive restarts.
- A provider is **eligible** when enabled and (no cap or `spentUsd < cap`).
  Eligible providers are tried in ascending `priority`; on an API error the
  next one is tried; the first success is used. The call that crosses a cap
  still completes (we only know the cost afterwards) and logs a warning;
  the next call skips that provider.
- `ai.dailySpendLimitUsd` (optional) caps the whole system per UTC day across
  all providers; once today's ledger sum reaches it, chat answers `429` and
  enrich falls back to Open Library only.
- Every successful call writes one `ai_usage` row: provider, model, purpose
  (`chat` or `enrich`), input/output/cache-read tokens, `cost_usd`, the user
  (nullable), and adds `cost_usd` to `ai_providers.spent_usd` in the same
  transaction. Cost is `tokens / 1e6 × price-per-million` per kind, rounded
  to a micro-dollar.
- OpenTelemetry counters: `alexandria.ai.tokens` (attributes `provider`,
  `purpose`, `kind` = `input|output|cache_read`), `alexandria.ai.cost_usd`
  (`provider`, `purpose`) and `alexandria.ai.calls` (`provider`, `purpose`,
  `result` = `ok|refusal|error`).

## Configuring providers in Pkl

```pkl
import "@schema/ai/provider/Anthropic.pkl"
import "@schema/credential/File.pkl"

ai {
  enabled = true
  // systemPrompt, maxToolRounds (default 6), historyTurns (default 20),
  // dailySpendLimitUsd (optional) are also here.
  providers {
    new Anthropic {
      name = "anthropic-opus"
      model = "claude-opus-5-5"
      apiKey = new File { path = "~/.config/secrets/alexandria/anthropic.json"; jsonKey = "api_key" }
      inputCostPerMillionUsd = 4.0
      outputCostPerMillionUsd = 20.0
      cacheReadCostPerMillionUsd = 0.20
      priority = 10          // tried first
      spendLimitUsd = 10.0   // initial cap; admins change it at runtime
      // effort = "medium", maxTokens, timeout, baseUrl, enabled also exist
    }
    new Anthropic {
      name = "anthropic-haiku"
      model = "claude-haiku-5-5"
      apiKey = new File { path = "~/.config/secrets/alexandria/anthropic.json"; jsonKey = "api_key" }
      inputCostPerMillionUsd = 0.10
      outputCostPerMillionUsd = 0.50
      priority = 20          // fallback once Opus is capped or failing
    }
  }
}
```

Prices are the Anthropic list prices from the `claude-api` skill, never from
memory. `dev.pkl` ships with `ai { enabled = false }`; turn it on in the
gitignored `configs/dev.local.pkl`. Model ids are plain strings; `effort`
(`low|medium|high`) steers Claude's adaptive thinking through
`output_config.effort` — the `thinking` parameter is never sent because Claude
Opus 5.5 is always adaptive and rejects `disabled`. Tool choice is left to
the model (`auto`); forced tool use is not supported on current models.
