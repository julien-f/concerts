-- Dates and times are Europe/Paris wall clock (ADR 0005); empty optional fields are NULL.
CREATE TABLE concerts (
    -- AUTOINCREMENT: ids in ?deleted= banners and restore links never point at a newer concert.
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    artist TEXT NOT NULL,
    date TEXT NOT NULL,       -- YYYY-MM-DD
    time TEXT,                -- HH:MM
    venue TEXT NOT NULL,
    ticket_url TEXT,
    on_sale_date TEXT,        -- YYYY-MM-DD
    created_by INTEGER REFERENCES members (id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    deleted_at TEXT           -- soft delete, undoable
) STRICT;

CREATE INDEX concerts_group_date ON concerts (group_id, date);
