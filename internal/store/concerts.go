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
	Address    string // street address of the venue, or ""
	TicketURL  string
	OnSaleDate string // YYYY-MM-DD or ""
	AddedBy    string // first name of the member who added it, "" if deleted
	Deleted    bool
	LastRevID  int64  // newest revision, 0 if never edited (set by Concert only)
	EditedBy   string // first name of the member behind LastRevID, "" if deleted (set by Concert only)
}

// concertSelect matches concertFields.
const concertSelect = `SELECT c.id, c.artist, c.date, coalesce(c.time, ''), c.venue,
	coalesce(c.address, ''), coalesce(c.ticket_url, ''), coalesce(c.on_sale_date, ''), coalesce(m.name, ''), c.deleted_at IS NOT NULL
	FROM concerts c LEFT JOIN members m ON m.id = c.created_by`

func concertFields(c *Concert) []any {
	return []any{&c.ID, &c.Artist, &c.Date, &c.Time, &c.Venue, &c.Address, &c.TicketURL, &c.OnSaleDate, &c.AddedBy, &c.Deleted}
}

// Concerts returns the group's live concerts in chronological order (no time first within a day).
func (s *Store) Concerts(ctx context.Context, groupID int64) ([]Concert, error) {
	return queryAll(ctx, s.db, concertFields, concertSelect+`
		WHERE c.group_id = ? AND c.deleted_at IS NULL
		ORDER BY c.date, c.time, c.id`, groupID)
}

// Concert returns the group's concert id, deleted or not, or ErrNotFound.
func (s *Store) Concert(ctx context.Context, groupID, id int64) (Concert, error) {
	var c Concert
	err := s.db.QueryRowContext(ctx, concertSelect+" WHERE c.id = ? AND c.group_id = ?", id, groupID).Scan(concertFields(&c)...)
	if errors.Is(err, sql.ErrNoRows) {
		return Concert{}, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT r.id, coalesce(m.name, '') FROM concert_revisions r
		LEFT JOIN members m ON m.id = r.edited_by WHERE r.concert_id = ? ORDER BY r.id DESC LIMIT 1`,
		id).Scan(&c.LastRevID, &c.EditedBy)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return c, err
}

// SameConcert returns the group's oldest live concert dated date whose artist matches artist
// as first names do (ignoring case), or ErrNotFound. Compared in Go: SQLite's lower() is ASCII-only.
func (s *Store) SameConcert(ctx context.Context, groupID int64, artist, date string) (Concert, error) {
	cs, err := queryAll(ctx, s.db, concertFields, concertSelect+`
		WHERE c.group_id = ? AND c.date = ? AND c.deleted_at IS NULL ORDER BY c.id`, groupID, date)
	if err != nil {
		return Concert{}, err
	}
	for _, c := range cs {
		if nameKey(c.Artist) == nameKey(artist) {
			return c, nil
		}
	}
	return Concert{}, ErrNotFound
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
		(group_id, artist, date, time, venue, address, ticket_url, on_sale_date, created_by, created_at)
		VALUES (?, ?, ?, nullif(?, ''), ?, nullif(?, ''), nullif(?, ''), nullif(?, ''), ?, ?)`,
		groupID, c.Artist, c.Date, c.Time, c.Venue, c.Address, c.TicketURL, c.OnSaleDate, memberID, s.timestamp())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// UpdateConcert replaces the editable fields of the group's live concert id on behalf of
// memberID, first saving the replaced values as a revision; revID is that revision, 0 if
// nothing changed. ErrNotFound if there is no such concert, ErrLimit if moving it to the
// future would exceed the limit.
func (s *Store) UpdateConcert(ctx context.Context, groupID, memberID, id int64, c Concert, today string) (revID int64, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	// IS NOT compares NULL-safely: stored empties are NULL, form ones "".
	res, err := tx.ExecContext(ctx, `INSERT INTO concert_revisions
		(concert_id, artist, date, time, venue, address, ticket_url, on_sale_date, edited_by, edited_at)
		SELECT id, artist, date, time, venue, address, ticket_url, on_sale_date, ?, ? FROM concerts
		WHERE id = ? AND group_id = ? AND deleted_at IS NULL AND (artist IS NOT ? OR date IS NOT ?
			OR time IS NOT nullif(?, '') OR venue IS NOT ? OR address IS NOT nullif(?, '')
			OR ticket_url IS NOT nullif(?, '') OR on_sale_date IS NOT nullif(?, ''))`,
		memberID, s.timestamp(), id, groupID,
		c.Artist, c.Date, c.Time, c.Venue, c.Address, c.TicketURL, c.OnSaleDate)
	if err != nil {
		return 0, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return 0, err
	} else if n > 0 {
		if revID, err = res.LastInsertId(); err != nil {
			return 0, err
		}
		if err := pruneRevisions(ctx, tx, id); err != nil {
			return 0, err
		}
	}
	res, err = tx.ExecContext(ctx, `UPDATE concerts SET artist = ?, date = ?, time = nullif(?, ''),
		venue = ?, address = nullif(?, ''), ticket_url = nullif(?, ''), on_sale_date = nullif(?, '')
		WHERE id = ? AND group_id = ? AND deleted_at IS NULL`,
		c.Artist, c.Date, c.Time, c.Venue, c.Address, c.TicketURL, c.OnSaleDate, id, groupID)
	if err != nil {
		return 0, err
	}
	if err := mustAffect(res); err != nil {
		return 0, err
	}
	return revID, commitWithin(ctx, tx, groupID, today)
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
