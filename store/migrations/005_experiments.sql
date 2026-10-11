CREATE TABLE experiments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    version_id TEXT NOT NULL REFERENCES versions(id),
    baseline_version_id TEXT REFERENCES versions(id),
    status TEXT NOT NULL DEFAULT 'pending',
    scorecard_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    finished_at TEXT
);

CREATE INDEX idx_experiments_version ON experiments(version_id);
