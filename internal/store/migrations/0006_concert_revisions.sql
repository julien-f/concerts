-- A concert's editable fields as they were before each edit, so any edit can be traced and undone.
CREATE TABLE concert_revisions (
    -- AUTOINCREMENT: ids in ?saved= banners never point at a newer revision.
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    concert_id INTEGER NOT NULL REFERENCES concerts (id) ON DELETE CASCADE,
    artist TEXT NOT NULL,
    date TEXT NOT NULL,
    time TEXT,
    venue TEXT NOT NULL,
    address TEXT,
    ticket_url TEXT,
    on_sale_date TEXT,
    -- No index: few rows per group, like concerts.created_by.
    edited_by INTEGER REFERENCES members (id) ON DELETE SET NULL,
    edited_at TEXT NOT NULL
) STRICT;

CREATE INDEX concert_revisions_concert ON concert_revisions (concert_id, id);
