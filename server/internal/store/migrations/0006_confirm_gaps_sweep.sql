CREATE TABLE tags (
    id          TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    defined     INTEGER NOT NULL DEFAULT 0
);

-- Slugs of expired entries: freeID skips these so an id never names two entries.
CREATE TABLE ids (
    id       TEXT PRIMARY KEY,
    freed_at TEXT
);

ALTER TABLE sessions ADD COLUMN runtime_session_id TEXT;
ALTER TABLE sessions ADD COLUMN blocks INTEGER NOT NULL DEFAULT 0;

INSERT INTO settings (key, value) VALUES ('search_min_corpus', '20');
INSERT INTO settings (key, value) VALUES ('open_gap_cap', '20');
INSERT INTO settings (key, value) VALUES ('dismiss_cap', '3');
