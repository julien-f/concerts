package store

import (
	"context"
	"database/sql"
	"errors"
)

// MaxConcerts bounds a group's upcoming concerts, so a leaked link cannot flood the list while
// past ones never lock a group; rows, past and deleted ones included, stay under twice that,
// so the disk cannot fill. "Upcoming" means a date on or after today, a Paris YYYY-MM-DD date
// that callers pass, so the store holds no time zone.
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
	Deleted    bool
}

// concertSelect matches scanConcert.
const concertSelect = `SELECT c.id, c.artist, c.date, coalesce(c.time, ''), c.venue,
	coalesce(c.ticket_url, ''), coalesce(c.on_sale_date, ''), coalesce(m.name, ''), c.deleted_at IS NOT NULL
	FROM concerts c LEFT JOIN members m ON m.id = c.created_by`

func scanConcert(row interface{ Scan(...any) error }) (Concert, error) {
	var c Concert
	err := row.Scan(&c.ID, &c.Artist, &c.Date, &c.Time, &c.Venue, &c.TicketURL, &c.OnSaleDate, &c.AddedBy, &c.Deleted)
	return c, err
}

// Concerts returns the group's live concerts in chronological order (no time first within a day).
func (s *Store) Concerts(ctx context.Context, groupID int64) ([]Concert, error) {
	rows, err := s.db.QueryContext(ctx, concertSelect+`
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

// Concert returns the group's concert id, deleted or not, or ErrNotFound.
func (s *Store) Concert(ctx context.Context, groupID, id int64) (Concert, error) {
	c, err := scanConcert(s.db.QueryRowContext(ctx, concertSelect+" WHERE c.id = ? AND c.group_id = ?", id, groupID))
	if errors.Is(err, sql.ErrNoRows) {
		return Concert{}, ErrNotFound
	}
	return c, err
}

// upcoming counts the group's live concerts dated today or later.
func upcoming(ctx context.Context, tx *sql.Tx, groupID int64, today string) (n int, err error) {
	err = tx.QueryRowContext(ctx,
		"SELECT count(*) FROM concerts WHERE group_id = ? AND deleted_at IS NULL AND date >= ?",
		groupID, today).Scan(&n)
	return n, err
}

// checkRoom returns ErrLimit if the group cannot take one more concert dated date; at the row
// cap, it purges the oldest deleted concert, else the oldest past one, instead of locking the group.
func checkRoom(ctx context.Context, tx *sql.Tx, groupID int64, date, today string) error {
	if date >= today {
		n, err := upcoming(ctx, tx, groupID, today)
		if err != nil {
			return err
		}
		if n >= MaxConcerts {
			return ErrLimit
		}
	}
	var all int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM concerts WHERE group_id = ?", groupID).Scan(&all); err != nil {
		return err
	}
	if all >= 2*MaxConcerts {
		// Never an upcoming concert, even if another request already moved today forward.
		_, err := tx.ExecContext(ctx, `DELETE FROM concerts WHERE id = (SELECT id FROM concerts
			WHERE group_id = ? AND (deleted_at IS NOT NULL OR date < ?)
			ORDER BY deleted_at IS NULL, deleted_at, date, time, id LIMIT 1)`, groupID, today)
		return err
	}
	return nil
}

// CreateConcert adds c to the group on behalf of memberID and returns its id, or ErrLimit.
func (s *Store) CreateConcert(ctx context.Context, groupID, memberID int64, c Concert, today string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := checkRoom(ctx, tx, groupID, c.Date, today); err != nil {
		return 0, err
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

// UpdateConcert replaces the editable fields of the group's live concert id; ErrNotFound if
// there is none, ErrLimit if moving it to the future would exceed the limit.
func (s *Store) UpdateConcert(ctx context.Context, groupID, id int64, c Concert, today string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE concerts SET artist = ?, date = ?, time = nullif(?, ''),
		venue = ?, ticket_url = nullif(?, ''), on_sale_date = nullif(?, '')
		WHERE id = ? AND group_id = ? AND deleted_at IS NULL`,
		c.Artist, c.Date, c.Time, c.Venue, c.TicketURL, c.OnSaleDate, id, groupID)
	if err != nil {
		return err
	}
	if err := mustAffect(res); err != nil {
		return err
	}
	return commitWithin(ctx, tx, groupID, today)
}

// commitWithin commits tx unless the group now has more upcoming concerts than the limit.
// Counted after the write, which tx can still roll back: a past concert is not counted,
// so only one moved into the future can cross the limit.
func commitWithin(ctx context.Context, tx *sql.Tx, groupID int64, today string) error {
	n, err := upcoming(ctx, tx, groupID, today)
	if err != nil {
		return err
	}
	if n > MaxConcerts {
		return ErrLimit
	}
	return tx.Commit()
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

// RestoreConcert undoes DeleteConcert; ErrNotFound if the concert is not deleted, ErrLimit if the group is full.
func (s *Store) RestoreConcert(ctx context.Context, groupID, id int64, today string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx,
		"UPDATE concerts SET deleted_at = NULL WHERE id = ? AND group_id = ? AND deleted_at IS NOT NULL",
		id, groupID)
	if err != nil {
		return err
	}
	if err := mustAffect(res); err != nil {
		return err
	}
	return commitWithin(ctx, tx, groupID, today)
}
