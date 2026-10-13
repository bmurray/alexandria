-- Alexandria schema. Times are RFC3339 UTC text. See docs/DESIGN.md.

CREATE TABLE users (
  id            TEXT PRIMARY KEY,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  picture       TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('member','librarian','admin')),
  created_at    TEXT NOT NULL,
  last_login_at TEXT NOT NULL
);

CREATE TABLE identities (
  id         TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider   TEXT NOT NULL,
  subject    TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (provider, subject)
);
CREATE INDEX identities_user ON identities(user_id);

CREATE TABLE sessions (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  user_agent   TEXT NOT NULL DEFAULT '',
  ip           TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  expires_at   TEXT NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);
CREATE INDEX sessions_expires ON sessions(expires_at);

CREATE TABLE challenges (
  id         TEXT PRIMARY KEY,
  kind       TEXT NOT NULL,
  payload    TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

CREATE TABLE books (
  id          TEXT PRIMARY KEY,
  title       TEXT NOT NULL,
  author      TEXT NOT NULL,
  isbn        TEXT NOT NULL DEFAULT '',
  year        INTEGER,
  genre       TEXT NOT NULL DEFAULT '',
  publisher   TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  cover_url   TEXT NOT NULL DEFAULT '',
  copies      INTEGER NOT NULL DEFAULT 1 CHECK (copies >= 1),
  tags        TEXT NOT NULL DEFAULT '[]',
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  created_by  TEXT REFERENCES users(id) ON DELETE SET NULL
);
CREATE INDEX books_title ON books(title);
CREATE INDEX books_author ON books(author);
CREATE INDEX books_genre ON books(genre);
CREATE INDEX books_isbn ON books(isbn);

CREATE VIRTUAL TABLE books_fts USING fts5(
  title, author, isbn, genre, tags, description,
  content='books', content_rowid='rowid',
  tokenize='unicode61 remove_diacritics 2'
);
CREATE TRIGGER books_ai AFTER INSERT ON books BEGIN
  INSERT INTO books_fts(rowid, title, author, isbn, genre, tags, description)
  VALUES (new.rowid, new.title, new.author, new.isbn, new.genre, new.tags, new.description);
END;
CREATE TRIGGER books_ad AFTER DELETE ON books BEGIN
  INSERT INTO books_fts(books_fts, rowid, title, author, isbn, genre, tags, description)
  VALUES ('delete', old.rowid, old.title, old.author, old.isbn, old.genre, old.tags, old.description);
END;
CREATE TRIGGER books_au AFTER UPDATE ON books BEGIN
  INSERT INTO books_fts(books_fts, rowid, title, author, isbn, genre, tags, description)
  VALUES ('delete', old.rowid, old.title, old.author, old.isbn, old.genre, old.tags, old.description);
  INSERT INTO books_fts(rowid, title, author, isbn, genre, tags, description)
  VALUES (new.rowid, new.title, new.author, new.isbn, new.genre, new.tags, new.description);
END;

CREATE TABLE loans (
  id             TEXT PRIMARY KEY,
  book_id        TEXT NOT NULL REFERENCES books(id) ON DELETE RESTRICT,
  user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  checked_out_at TEXT NOT NULL,
  due_at         TEXT NOT NULL,
  returned_at    TEXT,
  checked_out_by TEXT REFERENCES users(id) ON DELETE SET NULL,
  returned_by    TEXT REFERENCES users(id) ON DELETE SET NULL
);
CREATE INDEX loans_book_open ON loans(book_id) WHERE returned_at IS NULL;
CREATE INDEX loans_user ON loans(user_id, checked_out_at);
CREATE INDEX loans_due ON loans(due_at) WHERE returned_at IS NULL;

CREATE TABLE holds (
  id        TEXT PRIMARY KEY,
  book_id   TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
  user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  placed_at TEXT NOT NULL,
  ready_at  TEXT,
  status    TEXT NOT NULL DEFAULT 'waiting' CHECK (status IN ('waiting','ready','fulfilled','cancelled','lapsed'))
);
CREATE UNIQUE INDEX holds_active ON holds(book_id, user_id) WHERE status IN ('waiting','ready');
CREATE INDEX holds_book_status ON holds(book_id, status, placed_at);

CREATE TABLE ai_providers (
  name            TEXT PRIMARY KEY,
  enabled         INTEGER NOT NULL DEFAULT 1,
  priority        INTEGER NOT NULL DEFAULT 100,
  spend_limit_usd REAL,
  spent_usd       REAL NOT NULL DEFAULT 0
);

CREATE TABLE ai_usage (
  id                TEXT PRIMARY KEY,
  provider          TEXT NOT NULL,
  model             TEXT NOT NULL,
  purpose           TEXT NOT NULL,
  input_tokens      INTEGER NOT NULL DEFAULT 0,
  output_tokens     INTEGER NOT NULL DEFAULT 0,
  cache_read_tokens INTEGER NOT NULL DEFAULT 0,
  cost_usd          REAL NOT NULL DEFAULT 0,
  user_id           TEXT,
  created_at        TEXT NOT NULL
);
CREATE INDEX ai_usage_created ON ai_usage(created_at);

CREATE TABLE ai_conversations (
  id         TEXT PRIMARY KEY,
  owner      TEXT NOT NULL,
  title      TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX ai_conversations_owner ON ai_conversations(owner, updated_at);

CREATE TABLE ai_messages (
  id              TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL REFERENCES ai_conversations(id) ON DELETE CASCADE,
  role            TEXT NOT NULL CHECK (role IN ('user','assistant')),
  content         TEXT NOT NULL,
  created_at      TEXT NOT NULL
);
CREATE INDEX ai_messages_conv ON ai_messages(conversation_id, created_at);
