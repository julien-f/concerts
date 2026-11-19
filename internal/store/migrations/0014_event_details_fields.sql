-- event_details now keeps what an event's page gave beside its time: price, ticket link, address
-- (NULL if none), and when it was read. Rows are kept: they hold the times of listings without
-- one; read_at NULL marks a row read before this migration, so for its time only.
ALTER TABLE event_details ADD COLUMN price TEXT;
ALTER TABLE event_details ADD COLUMN ticket_url TEXT;
ALTER TABLE event_details ADD COLUMN address TEXT;
ALTER TABLE event_details ADD COLUMN read_at TEXT; -- RFC 3339, UTC
