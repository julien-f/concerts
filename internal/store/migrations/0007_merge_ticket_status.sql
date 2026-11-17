-- « J'ai mon billet » merges into « J'y vais »: members rarely came back to update it.
-- SQLite cannot alter a CHECK, so the table is rebuilt; no table references it.
CREATE TABLE statuses_new (
    concert_id INTEGER NOT NULL REFERENCES concerts (id) ON DELETE CASCADE,
    member_id INTEGER NOT NULL REFERENCES members (id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('interested', 'going')),
    PRIMARY KEY (concert_id, member_id)
) STRICT, WITHOUT ROWID;

INSERT INTO statuses_new (concert_id, member_id, status)
SELECT concert_id, member_id, CASE status WHEN 'ticket' THEN 'going' ELSE status END
FROM statuses;

DROP TABLE statuses;
ALTER TABLE statuses_new RENAME TO statuses;
