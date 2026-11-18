-- Concerts imported from a venue's agenda: source is the venue key, source_id the venue's stable
-- id for the event; both NULL when a member added it (the store sets both or neither).
ALTER TABLE concerts ADD COLUMN source TEXT;
ALTER TABLE concerts ADD COLUMN source_id TEXT;
-- One copy per group and event; a deleted one stays as a tombstone so it never comes back.
CREATE UNIQUE INDEX concerts_source ON concerts (group_id, source, source_id) WHERE source IS NOT NULL;

-- Venues whose agenda the group imports; only the operator sets them.
CREATE TABLE group_venues (
    group_id INTEGER NOT NULL REFERENCES groups ON DELETE CASCADE,
    venue TEXT NOT NULL,
    PRIMARY KEY (group_id, venue)
) STRICT, WITHOUT ROWID;

-- What an event's page gave, read once for every group: the time (NULL if none) of an event the
-- listing dates without one; date only to prune past events.
CREATE TABLE event_details (
    source TEXT NOT NULL,
    source_id TEXT NOT NULL,
    date TEXT NOT NULL,
    time TEXT,
    PRIMARY KEY (source, source_id)
) STRICT, WITHOUT ROWID;
