CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    started_at TEXT NOT NULL DEFAULT (datetime('now')),
    ended_at TEXT,
    upstream TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT ''
);
