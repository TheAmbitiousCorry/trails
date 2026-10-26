CREATE TABLE anchors (
    id              INTEGER PRIMARY KEY,
    entry_id        TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    type            TEXT NOT NULL CHECK(type IN ('file','range','dir','commit','url','transcript','produced_by')),
    path_or_url     TEXT,
    range_start     INTEGER,
    range_end       INTEGER,
    hash            TEXT,
    fetched_at      TEXT,
    last_checked_at TEXT,
    ok              INTEGER NOT NULL DEFAULT 1,
    extra           TEXT
);

CREATE INDEX anchors_entry ON anchors(entry_id);

ALTER TABLE entries ADD COLUMN promoted_by TEXT;
ALTER TABLE entries ADD COLUMN tier_before_stale TEXT;
