CREATE TABLE groups (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    -- SHA-256 of the link secret; the secret itself is never stored.
    secret_hash BLOB NOT NULL UNIQUE,
    created_at TEXT NOT NULL
) STRICT;
