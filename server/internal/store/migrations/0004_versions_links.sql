CREATE TABLE versions (
    id           INTEGER PRIMARY KEY,
    entry_id     TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    body_json    TEXT NOT NULL,
    title        TEXT NOT NULL,
    read_when    TEXT NOT NULL,
    anchors_json TEXT NOT NULL,
    actor        TEXT NOT NULL,
    created_at   TEXT NOT NULL
);

CREATE INDEX versions_entry ON versions(entry_id, id);

-- see_also and conflicts_with are symmetric: stored once with from_id < to_id and queried both ways.
CREATE TABLE links (
    from_id  TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    to_id    TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    relation TEXT NOT NULL CHECK(relation IN ('requires','see_also','supersedes','conflicts_with')),
    PRIMARY KEY (from_id, to_id, relation)
);

CREATE INDEX links_to ON links(to_id);
