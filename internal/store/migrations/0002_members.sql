CREATE TABLE members (
    -- AUTOINCREMENT: a deleted member's id is never reused, so their cookie cannot name a newcomer.
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    -- strings.ToLower(name): the one rule for "same first name" and for sorting.
    name_key TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (group_id, name_key)
) STRICT;
