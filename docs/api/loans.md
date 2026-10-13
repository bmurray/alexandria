# Loans and holds API

Check-out, check-in, holds and the librarian dashboard. Conventions (errors,
roles, JSON) are as in [books.md](./books.md).

## Objects

Loan:
```json
{
  "id": "…", "bookId": "…", "userId": "…",
  "checkedOutAt": "2026-10-01T12:00:00Z", "dueAt": "2026-10-22T12:00:00Z",
  "returnedAt": null, "overdue": false,
  "book": { "id": "…", "title": "Dune", "author": "Frank Herbert", "coverUrl": "…" },
  "user": { "id": "…", "name": "Alice", "email": "alice@example.com" }
}
```
`dueAt = checkedOutAt + library.loanDays` (default 21). `overdue` is derived:
not returned and past due.

Hold:
```json
{
  "id": "…", "bookId": "…", "userId": "…",
  "placedAt": "2026-10-01T12:00:00Z", "readyAt": null,
  "status": "waiting", "position": 2,
  "book": { … }, "user": { … }
}
```
`status` is `waiting` (queued), `ready` (a returned copy is reserved for this
member; `readyAt` set), `fulfilled` (checked out), `cancelled` or `lapsed`
(not picked up within `library.holdPickupDays`, default 7; the copy passes to
the next member). `position` is the 1-based place among the book's active
holds, 0 once inactive.

## Rules

- Checkout `409` when no copy is free, when the member already has an open loan
  on the book, or when the remaining free copies are reserved by `ready`
  holds for other members. A member with a `ready` hold may check out; the
  hold becomes `fulfilled`.
- Return records `returnedAt`; if holds are waiting, the oldest becomes
  `ready` (one per returned copy).
- A hold may only be placed when no copy can be checked out right now;
  otherwise `409 {"error":"book is available; check it out instead"}`. One
  active hold per member per book; `409` if the member has the book on loan.
- Cancelling a `ready` hold passes the reserved copy to the next member.

## `POST /api/books/{id}/checkout` — check out

Auth: member. Body optional: `{ "userId": "…" }`. Members may only check out
for themselves (`403` otherwise); librarians may name any member.

`201 { "loan": Loan }`. `404` unknown book or user; `409` per the rules above.

## `POST /api/loans/{id}/return` — check in

Auth: member for their own loan, librarian for any (`403` otherwise). No body.

`200 { "loan": Loan }` with `returnedAt` set. `404` unknown loan; `409` already returned.

## `GET /api/loans` — list loans

Auth: member. Members always receive their own loans; librarians may filter.

| query | meaning |
|---|---|
| `status` | `open`, `overdue`, `returned`; omit for all |
| `userId` | one member (librarian only; ignored for members) |
| `limit` | default 50, max 200 |
| `offset` | default 0 |

`{ "loans": [ Loan, … ], "total": n }`, open loans first, newest first.
`400` on an unknown `status`.

## `GET /api/me/loans` — my loans

Auth: member. `{ "loans": [ Loan, … ] }`: open loans first, then history, newest first.

## `GET /api/me/holds` — my holds

Auth: member. `{ "holds": [ Hold, … ] }`: the caller's `waiting` and `ready` holds, oldest first.

## `POST /api/books/{id}/holds` — place a hold

Auth: member. No body. `201 { "hold": Hold }`. `404` unknown book; `409` per the rules.

## `DELETE /api/holds/{id}` — cancel a hold

Auth: member for their own hold, librarian for any (`403` otherwise).
`204`. `404` unknown hold; `409` when the hold is no longer active.

## `GET /api/holds?bookId=` — holds queue

Auth: librarian. Active holds (`waiting`, `ready`) for one book in queue
order, or for every book when `bookId` is omitted.

`{ "holds": [ Hold, … ] }`

## `GET /api/dashboard` — librarian overview

Auth: librarian.

```json
{
  "books": 60, "copies": 102, "openLoans": 7, "overdue": 2, "holdsWaiting": 3, "members": 12,
  "recentLoans": [ Loan, … ],
  "overdueLoans": [ Loan, … ],
  "readyHolds": [ Hold, … ]
}
```

`recentLoans` is the last ten check-outs; `overdueLoans` every open loan past
due, soonest-due first; `readyHolds` every hold awaiting pickup. `members` is
the number of user accounts.
