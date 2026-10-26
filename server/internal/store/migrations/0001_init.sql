CREATE TABLE signs (
    id        TEXT PRIMARY KEY,
    parent_id TEXT REFERENCES signs(id),
    title     TEXT NOT NULL,
    position  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE entries (
    id             TEXT PRIMARY KEY,
    sign_id        TEXT NOT NULL REFERENCES signs(id),
    kind           TEXT NOT NULL,
    title          TEXT NOT NULL,
    read_when      TEXT NOT NULL,
    body_json      TEXT NOT NULL,
    body_schema    INTEGER NOT NULL,
    tags_json      TEXT NOT NULL DEFAULT '[]',
    tier           TEXT NOT NULL,
    superseded_by  TEXT,
    distinct_reason TEXT,
    decided_by     TEXT,
    decided_at     TEXT,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);

CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,
    actor      TEXT NOT NULL,
    task       TEXT NOT NULL,
    task_type  TEXT NOT NULL,
    started_at TEXT NOT NULL,
    ended_at   TEXT,
    ended_by   TEXT
);

-- No foreign key on session_id: the recorder logs a call before its session row exists or when the id is unknown.
CREATE TABLE events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    at         TEXT NOT NULL,
    tool       TEXT NOT NULL,
    target     TEXT NOT NULL DEFAULT '',
    depth      TEXT NOT NULL DEFAULT '',
    result     TEXT NOT NULL DEFAULT '',
    shown_json TEXT NOT NULL DEFAULT '[]'
);

CREATE INDEX events_session ON events(session_id, id);
