-- One status per member and concert; no row means no answer.
CREATE TABLE statuses (
    concert_id INTEGER NOT NULL REFERENCES concerts (id) ON DELETE CASCADE,
    member_id INTEGER NOT NULL REFERENCES members (id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('interested', 'going', 'ticket')),
    PRIMARY KEY (concert_id, member_id)
) STRICT, WITHOUT ROWID;
