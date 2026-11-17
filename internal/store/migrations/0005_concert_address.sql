-- Street address of the venue, for the map link and the .ics; NULL when unknown.
ALTER TABLE concerts ADD COLUMN address TEXT;
