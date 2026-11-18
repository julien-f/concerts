-- What a venue's agenda last said of each upcoming event, kept once for every group (the
-- validated values the sync imports), so « Relire la billetterie » can show what changed on
-- an import members edited. Pruned once past or unseen for 30 days.
CREATE TABLE venue_events (
    source TEXT NOT NULL,
    source_id TEXT NOT NULL,
    artist TEXT NOT NULL,
    date TEXT NOT NULL,
    time TEXT,
    venue TEXT NOT NULL,
    address TEXT,
    ticket_url TEXT,
    price TEXT,
    sold_out INTEGER NOT NULL CHECK (sold_out IN (0, 1)),
    seen_at TEXT NOT NULL,
    PRIMARY KEY (source, source_id)
) STRICT, WITHOUT ROWID;
