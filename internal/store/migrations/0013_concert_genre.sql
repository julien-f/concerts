-- The concert's genre as free text (« Jazz », « Électro, DJ set »), NULL when unknown. Revisions
-- keep it, so undoing an edit restores it; venue events too, so « Relire la billetterie » offers it.
ALTER TABLE concerts ADD COLUMN genre TEXT;
ALTER TABLE concert_revisions ADD COLUMN genre TEXT;
ALTER TABLE venue_events ADD COLUMN genre TEXT;
