package store

import (
	"context"
	"database/sql"
	"errors"
)

// Follow makes the group import venue's agenda; following it again changes nothing.
// ErrNotFound if there is no such group.
func (s *Store) Follow(ctx context.Context, groupID int64, venue string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM groups WHERE id = ?", groupID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO group_venues (group_id, venue) VALUES (?, ?) ON CONFLICT DO NOTHING",
		groupID, venue); err != nil {
		return err
	}
	return tx.Commit()
}

// Unfollow stops the group importing venue's agenda and deletes, past ones included, the
// venue's live rows nobody answered or edited; it returns how many. Tombstones and the other
// rows stay. ErrNotFound if the group does not follow venue.
func (s *Store) Unfollow(ctx context.Context, groupID int64, venue string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, "DELETE FROM group_venues WHERE group_id = ? AND venue = ?", groupID, venue)
	if err != nil {
		return 0, err
	}
	if err := mustAffect(res); err != nil {
		return 0, err
	}
	res, err = tx.ExecContext(ctx, `DELETE FROM concerts WHERE group_id = ? AND source = ? AND deleted_at IS NULL
		AND NOT EXISTS (SELECT 1 FROM statuses s WHERE s.concert_id = concerts.id)
		AND NOT EXISTS (SELECT 1 FROM concert_revisions r WHERE r.concert_id = concerts.id)`, groupID, venue)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

// Followers returns the ids of the groups following venue, in id order.
func (s *Store) Followers(ctx context.Context, venue string) ([]int64, error) {
	return queryAll(ctx, s.db, func(id *int64) []any { return []any{id} },
		"SELECT group_id FROM group_venues WHERE venue = ? ORDER BY group_id", venue)
}

// Followed returns the keys of the venues the group follows, sorted.
func (s *Store) Followed(ctx context.Context, groupID int64) ([]string, error) {
	return queryAll(ctx, s.db, func(v *string) []any { return []any{v} },
		"SELECT venue FROM group_venues WHERE group_id = ? ORDER BY venue", groupID)
}

// EventDetail is what an event's page gave once: its date and time, "" if it had none.
type EventDetail struct{ Date, Time string }

// EventDetail returns what was kept of the page of source's event sourceID, or ErrNotFound.
func (s *Store) EventDetail(ctx context.Context, source, sourceID string) (EventDetail, error) {
	id, ok := CleanSourceID(sourceID)
	if !ok {
		return EventDetail{}, ErrSourceID
	}
	var d EventDetail
	err := s.db.QueryRowContext(ctx, "SELECT date, coalesce(time, '') FROM event_details WHERE source = ? AND source_id = ?",
		source, id).Scan(&d.Date, &d.Time)
	if errors.Is(err, sql.ErrNoRows) {
		return EventDetail{}, ErrNotFound
	}
	return d, err
}

// PutEventDetail keeps what the page of source's event sourceID gave, replacing what was kept.
func (s *Store) PutEventDetail(ctx context.Context, source, sourceID string, d EventDetail) error {
	id, ok := CleanSourceID(sourceID)
	if !ok {
		return ErrSourceID
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO event_details (source, source_id, date, time) VALUES (?, ?, ?, nullif(?, ''))
		ON CONFLICT DO UPDATE SET date = excluded.date, time = excluded.time`, source, id, d.Date, d.Time)
	return err
}

// PruneEventDetails deletes what was kept of events dated before today; it returns how many.
func (s *Store) PruneEventDetails(ctx context.Context, today string) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM event_details WHERE date < ?", today)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
