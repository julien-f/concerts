package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// MaxConcerts bounds a group's upcoming concerts, so a leaked link cannot flood the list while
// past ones never lock a group; rows, past and deleted ones included, are at most twice that,
// so the disk cannot fill. "Upcoming" means a date on or after today, a Paris YYYY-MM-DD date
// that callers pass, so the store holds no time zone.
const MaxConcerts = 500

// Concert is a gig shared in a group. Optional fields are "" when unset.
type Concert struct {
	ID          int64
	Token       string // of the public link /c/<token>
	Artist      string
	Date        string // YYYY-MM-DD, Europe/Paris
	Time        string // HH:MM or ""
	Venue       string
	Address     string // street address of the venue, or ""
	TicketURL   string
	OnSaleDate  string // YYYY-MM-DD or ""
	Price       string // as shown, e.g. « 29–32 € », or ""
	Description string // paragraphs separated by "\n", or ""
	Genre       string // free text, « Électro, DJ set », or ""
	SoldOut     bool   // « Complet »: no tickets left on sale
	AddedBy     string // first name of the member who added it, "" if that member was deleted or a venue did
	Source      string // key of the venue it was imported from, "" if a member added it
	Deleted     bool
	CreatedAt   time.Time // UTC: added, or imported
	LastRevID   int64     // newest revision, 0 if never edited (set by Concert only)
	EditedBy    string    // first name of the member behind LastRevID, "" if deleted (set by Concert only)
}

// concertSelect matches concertFields.
const concertSelect = `SELECT c.id, c.token, c.artist, c.date, coalesce(c.time, ''), c.venue,
	coalesce(c.address, ''), coalesce(c.ticket_url, ''), coalesce(c.on_sale_date, ''),
	coalesce(c.price, ''), coalesce(c.description, ''), coalesce(c.genre, ''), c.sold_out, coalesce(m.name, ''), coalesce(c.source, ''), c.deleted_at IS NOT NULL,
	c.created_at
	FROM concerts c LEFT JOIN members m ON m.id = c.created_by`

func concertFields(c *Concert) []any {
	return []any{&c.ID, &c.Token, &c.Artist, &c.Date, &c.Time, &c.Venue, &c.Address, &c.TicketURL, &c.OnSaleDate, &c.Price, &c.Description, &c.Genre, &c.SoldOut, &c.AddedBy, &c.Source, &c.Deleted,
		timestampScanner{&c.CreatedAt}}
}

// setEditable and differs take editable(c) as arguments; stored empties are NULL, ours "",
// and IS NOT compares NULL-safely.
const (
	setEditable = `artist = ?, date = ?, time = nullif(?, ''), venue = ?, address = nullif(?, ''),
		ticket_url = nullif(?, ''), on_sale_date = nullif(?, ''), price = nullif(?, ''), description = nullif(?, ''),
		genre = nullif(?, ''), sold_out = ?`
	differs = `(artist IS NOT ? OR date IS NOT ? OR time IS NOT nullif(?, '') OR venue IS NOT ? OR address IS NOT nullif(?, '')
		OR ticket_url IS NOT nullif(?, '') OR on_sale_date IS NOT nullif(?, '') OR price IS NOT nullif(?, '') OR description IS NOT nullif(?, '')
		OR genre IS NOT nullif(?, '') OR sold_out IS NOT ?)`
)

func editable(c Concert) []any {
	return []any{c.Artist, c.Date, c.Time, c.Venue, c.Address, c.TicketURL, c.OnSaleDate, c.Price, c.Description, c.Genre, c.SoldOut}
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
		return Concert{}, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT r.id, coalesce(m.name, '') FROM concert_revisions r
		LEFT JOIN members m ON m.id = r.edited_by WHERE r.concert_id = ? ORDER BY r.id DESC LIMIT 1`,
		id).Scan(&c.LastRevID, &c.EditedBy)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Concert{}, err
	}
	return c, nil
}

// ConcertByToken returns the group and id of the concert whose public link holds token,
// deleted or not, or ErrNotFound.
func (s *Store) ConcertByToken(ctx context.Context, token string) (groupID, id int64, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT group_id, id FROM concerts WHERE token = ?", token).Scan(&groupID, &id)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return groupID, id, err
}

// newToken returns a public link token: 12 random bytes in hex, like the migration's backfill.
func newToken() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b) // never fails, see crypto/rand.Read
	return hex.EncodeToString(b)
}

// SameConcert returns the group's oldest live concert dated date whose artist matches artist
// as first names do (ignoring case), or ErrNotFound. Compared in Go: SQLite's lower() is ASCII-only.
func (s *Store) SameConcert(ctx context.Context, groupID int64, artist, date string) (Concert, error) {
	cs, err := s.ConcertsOn(ctx, groupID, date)
	if err != nil {
		return Concert{}, err
	}
	for _, c := range cs {
		if !c.Deleted && nameKey(c.Artist) == nameKey(artist) {
			return c, nil
		}
	}
	return Concert{}, ErrNotFound
}

// ConcertsOn returns the group's concerts dated date, deleted ones included, oldest first.
func (s *Store) ConcertsOn(ctx context.Context, groupID int64, date string) ([]Concert, error) {
	return concertsOn(ctx, s.db, groupID, date)
}

func concertsOn(ctx context.Context, q querier, groupID int64, date string) ([]Concert, error) {
	return queryAll(ctx, q, concertFields, concertSelect+" WHERE c.group_id = ? AND c.date = ? ORDER BY c.id", groupID, date)
}

// purgeIfFull makes room for one more row at the row cap instead of locking the group. It
// deletes, oldest first: a past row that is deleted or an unanswered import; another deleted
// row, except an upcoming import's tombstone; a past row; last, an upcoming import's tombstone
// (only abuse gets there, and that import may then come back).
func purgeIfFull(ctx context.Context, tx *sql.Tx, groupID int64, today string) error {
	var all int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM concerts WHERE group_id = ?", groupID).Scan(&all); err != nil {
		return err
	}
	if all >= 2*MaxConcerts {
		// Never a live upcoming concert, even if another request already moved today forward.
		_, err := tx.ExecContext(ctx, `DELETE FROM concerts WHERE id = (SELECT id FROM concerts c
			WHERE group_id = ?1 AND (deleted_at IS NOT NULL OR date < ?2)
			ORDER BY CASE
				WHEN date < ?2 AND (deleted_at IS NOT NULL OR (source IS NOT NULL
					AND NOT EXISTS (SELECT 1 FROM statuses s WHERE s.concert_id = c.id))) THEN 0
				WHEN deleted_at IS NOT NULL AND source IS NULL THEN 1
				WHEN date < ?2 THEN 2
				ELSE 3 END,
			deleted_at IS NULL, deleted_at, date, time, id LIMIT 1)`, groupID, today)
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
	if err := purgeIfFull(ctx, tx, groupID, today); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO concerts
		(group_id, token, artist, date, time, venue, address, ticket_url, on_sale_date, price, description, genre, sold_out,
			created_by, created_at)
		VALUES (?, ?, ?, ?, nullif(?, ''), ?, nullif(?, ''), nullif(?, ''), nullif(?, ''), nullif(?, ''), nullif(?, ''), nullif(?, ''), ?, ?, ?)`,
		append(append([]any{groupID, newToken()}, editable(c)...), memberID, s.timestamp())...)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := commitWithin(ctx, tx, groupID, today); err != nil {
		return 0, err
	}
	return id, nil
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
	res, err := tx.ExecContext(ctx, `INSERT INTO concert_revisions
		(concert_id, artist, date, time, venue, address, ticket_url, on_sale_date, price, description, genre, sold_out, edited_by, edited_at)
		SELECT id, artist, date, time, venue, address, ticket_url, on_sale_date, price, description, genre, sold_out, ?, ? FROM concerts
		WHERE id = ? AND group_id = ? AND deleted_at IS NULL AND `+differs,
		append([]any{memberID, s.timestamp(), id, groupID}, editable(c)...)...)
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
	res, err = tx.ExecContext(ctx, "UPDATE concerts SET "+setEditable+" WHERE id = ? AND group_id = ? AND deleted_at IS NULL",
		append(editable(c), id, groupID)...)
	if err != nil {
		return 0, err
	}
	if err := mustAffect(res); err != nil {
		return 0, err
	}
	return revID, commitWithin(ctx, tx, groupID, today)
}

// commitWithin commits tx unless the group now has more live concerts dated today or later
// than the limit. Counted after the write, which tx can still roll back: a past concert
// is not counted, so only one added, restored or moved into the future can cross it.
func commitWithin(ctx context.Context, tx *sql.Tx, groupID int64, today string) error {
	var n int
	if err := tx.QueryRowContext(ctx,
		"SELECT count(*) FROM concerts WHERE group_id = ? AND deleted_at IS NULL AND date >= ?",
		groupID, today).Scan(&n); err != nil {
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
