package store

import (
	"context"
	"database/sql"
	"errors"
)

// MaxConcerts bounds a group's live concerts, so a leaked link cannot flood the list.
const MaxConcerts = 500

// Concert is a gig shared in a group. Optional fields are "" when unset.
type Concert struct {
	ID         int64
	Artist     string
	Date       string // YYYY-MM-DD, Europe/Paris
	Time       string // HH:MM or ""
	Venue      string
	TicketURL  string
	OnSaleDate string // YYYY-MM-DD or ""
	AddedBy    string // first name of the member who added it, "" if deleted
}

// concertColumns matches scanConcert; c is concerts, m the creator joined on created_by.
const concertColumns = `c.id, c.artist, c.date, coalesce(c.time, ''), c.venue,
	coalesce(c.ticket_url, ''), coalesce(c.on_sale_date, ''), coalesce(m.name, '')`

func scanConcert(row interface{ Scan(...any) error }) (Concert, error) {
	var c Concert
	err := row.Scan(&c.ID, &c.Artist, &c.Date, &c.Time, &c.Venue, &c.TicketURL, &c.OnSaleDate, &c.AddedBy)
	return c, err
}

// Concerts returns the group's live concerts in chronological order (no time first within a day).
func (s *Store) Concerts(ctx context.Context, groupID int64) ([]Concert, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+concertColumns+`
		FROM concerts c LEFT JOIN members m ON m.id = c.created_by
		WHERE c.group_id = ? AND c.deleted_at IS NULL
		ORDER BY c.date, c.time, c.id`, groupID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var cs []Concert
	for rows.Next() {
		c, err := scanConcert(rows)
		if err != nil {
			return nil, err
		}
		cs = append(cs, c)
	}
	return cs, rows.Err()
}

// Concert returns the group's live concert id, or ErrNotFound (also for a deleted one).
func (s *Store) Concert(ctx context.Context, groupID, id int64) (Concert, error) {
	return s.concert(ctx, groupID, id, "IS NULL")
}

// DeletedConcert returns the group's concert id only if it is soft-deleted, or ErrNotFound.
func (s *Store) DeletedConcert(ctx context.Context, groupID, id int64) (Concert, error) {
	return s.concert(ctx, groupID, id, "IS NOT NULL")
}

func (s *Store) concert(ctx context.Context, groupID, id int64, deleted string) (Concert, error) {
	c, err := scanConcert(s.db.QueryRowContext(ctx, `SELECT `+concertColumns+`
		FROM concerts c LEFT JOIN members m ON m.id = c.created_by
		WHERE c.id = ? AND c.group_id = ? AND c.deleted_at `+deleted, id, groupID))
	if errors.Is(err, sql.ErrNoRows) {
		return Concert{}, ErrNotFound
	}
	return c, err
}

// CreateConcert adds c to the group on behalf of memberID and returns its id,
// or ErrLimit past MaxConcerts live concerts.
func (s *Store) CreateConcert(ctx context.Context, groupID, memberID int64, c Concert) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var n int
	if err := tx.QueryRowContext(ctx,
		"SELECT count(*) FROM concerts WHERE group_id = ? AND deleted_at IS NULL", groupID).Scan(&n); err != nil {
		return 0, err
	}
	if n >= MaxConcerts {
		return 0, ErrLimit
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO concerts
		(group_id, artist, date, time, venue, ticket_url, on_sale_date, created_by, created_at)
		VALUES (?, ?, ?, nullif(?, ''), ?, nullif(?, ''), nullif(?, ''), ?, ?)`,
		groupID, c.Artist, c.Date, c.Time, c.Venue, c.TicketURL, c.OnSaleDate, memberID, s.timestamp())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// UpdateConcert replaces the editable fields of the group's live concert id, or returns ErrNotFound.
func (s *Store) UpdateConcert(ctx context.Context, groupID, id int64, c Concert) error {
	res, err := s.db.ExecContext(ctx, `UPDATE concerts SET artist = ?, date = ?, time = nullif(?, ''),
		venue = ?, ticket_url = nullif(?, ''), on_sale_date = nullif(?, '')
		WHERE id = ? AND group_id = ? AND deleted_at IS NULL`,
		c.Artist, c.Date, c.Time, c.Venue, c.TicketURL, c.OnSaleDate, id, groupID)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

// DeleteConcert soft-deletes the group's live concert id, or returns ErrNotFound.
func (s *Store) DeleteConcert(ctx context.Context, groupID, id int64) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE concerts SET deleted_at = ? WHERE id = ? AND group_id = ? AND deleted_at IS NULL",
		s.timestamp(), id, groupID)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

// RestoreConcert undoes DeleteConcert, or returns ErrNotFound if the concert is not deleted.
func (s *Store) RestoreConcert(ctx context.Context, groupID, id int64) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE concerts SET deleted_at = NULL WHERE id = ? AND group_id = ? AND deleted_at IS NOT NULL",
		id, groupID)
	if err != nil {
		return err
	}
	return mustAffect(res)
}
