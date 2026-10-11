CREATE TABLE versions (
    id TEXT PRIMARY KEY,
    parent_id TEXT REFERENCES versions(id),
    status TEXT NOT NULL DEFAULT 'candidate',
    overlay_json TEXT NOT NULL DEFAULT '{}',
    notes TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    activated_at TEXT
);

CREATE TABLE active_version (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    version_id TEXT REFERENCES versions(id)
);

INSERT INTO active_version (id, version_id) VALUES (1, NULL);
