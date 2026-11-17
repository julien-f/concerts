-- What the ticketing page says about the concert (paragraphs separated by line feeds) and its
-- price as shown (« 29–32 € », « Prix libre »); NULL when unknown. Revisions keep them too,
-- so undoing an edit restores them.
ALTER TABLE concerts ADD COLUMN description TEXT;
ALTER TABLE concerts ADD COLUMN price TEXT;
ALTER TABLE concert_revisions ADD COLUMN description TEXT;
ALTER TABLE concert_revisions ADD COLUMN price TEXT;
