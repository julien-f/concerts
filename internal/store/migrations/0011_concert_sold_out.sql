-- « Complet »: no tickets left on sale. Set by a member or a venue's agenda; revisions keep it
-- too, so undoing an edit restores it.
ALTER TABLE concerts ADD COLUMN sold_out INTEGER NOT NULL DEFAULT 0 CHECK (sold_out IN (0, 1));
ALTER TABLE concert_revisions ADD COLUMN sold_out INTEGER NOT NULL DEFAULT 0 CHECK (sold_out IN (0, 1));
