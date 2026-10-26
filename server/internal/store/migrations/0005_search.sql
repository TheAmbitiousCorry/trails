-- entries_fts keeps its own copy of the text and is maintained by search.Refresh from the write path; SQL cannot walk the JSON body, so there is no trigger.
CREATE VIRTUAL TABLE entries_fts USING fts5(entry_id UNINDEXED, title, read_when_all, body_text, tokenize = 'porter unicode61');

CREATE TABLE entry_aliases (
    entry_id  TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    read_when TEXT NOT NULL
);

CREATE INDEX entry_aliases_entry ON entry_aliases(entry_id);

CREATE TABLE gaps (
    id          INTEGER PRIMARY KEY,
    sign_id     TEXT,
    session_id  TEXT NOT NULL,
    query       TEXT,
    paths_json  TEXT,
    state       TEXT NOT NULL DEFAULT 'open',
    resolution  TEXT,
    target_id   TEXT,
    reason      TEXT,
    opened_at   TEXT NOT NULL,
    resolved_at TEXT
);

INSERT INTO settings (key, value) VALUES ('search_threshold', '0.5');
INSERT INTO settings (key, value) VALUES ('duplicate_threshold', '0.8');
INSERT INTO settings (key, value) VALUES ('paraphrase_threshold', '0.5');
