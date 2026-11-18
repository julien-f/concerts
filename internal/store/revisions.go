package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// maxRevisions bounds each concert's history, so the disk cannot fill: older revisions are pruned.
const maxRevisions = 20

// Revision is a concert's editable fields as they were before an edit.
type Revision struct {
	ID       int64
	Concert            // editable fields only
	EditedBy string    // first name of the member who made the edit, "" if deleted
	EditedAt time.Time // UTC
}

// revisionSelect matches revisionFields; it joins the concert so every lookup checks
// the group and liveness in the same statement.
const revisionSelect = `SELECT r.id, r.artist, r.date, coalesce(r.time, ''), r.venue, coalesce(r.address, ''),
	coalesce(r.ticket_url, ''), coalesce(r.on_sale_date, ''), coalesce(r.price, ''), coalesce(r.description, ''),
	r.sold_out, coalesce(m.name, ''), r.edited_at
	FROM concert_revisions r JOIN concerts c ON c.id = r.concert_id LEFT JOIN members m ON m.id = r.edited_by
	WHERE r.concert_id = ? AND c.group_id = ? AND c.deleted_at IS NULL`

// timestampScanner scans a *_at column into a time.Time.
type timestampScanner struct{ t *time.Time }

func (ts timestampScanner) Scan(v any) error {
	s, ok := v.(string)
	if !ok {
		return errors.New("timestamp: not a string")
	}
	t, err := time.Parse(time.RFC3339, s)
	*ts.t = t
	return err
}

func revisionFields(r *Revision) []any {
	return []any{&r.ID, &r.Artist, &r.Date, &r.Time, &r.Venue, &r.Address, &r.TicketURL, &r.OnSaleDate,
		&r.Price, &r.Description, &r.SoldOut, &r.EditedBy, timestampScanner{&r.EditedAt}}
}

// Revisions returns the history of the group's live concert, newest first; none for an unknown or deleted concert.
func (s *Store) Revisions(ctx context.Context, groupID, concertID int64) ([]Revision, error) {
	return queryAll(ctx, s.db, revisionFields, revisionSelect+" ORDER BY r.id DESC", concertID, groupID)
}

// Revision returns revision revID of the group's live concert, or ErrNotFound.
func (s *Store) Revision(ctx context.Context, groupID, concertID, revID int64) (Revision, error) {
	var r Revision
	err := s.db.QueryRowContext(ctx, revisionSelect+" AND r.id = ?", concertID, groupID, revID).Scan(revisionFields(&r)...)
	if errors.Is(err, sql.ErrNoRows) {
		return Revision{}, ErrNotFound
	}
	return r, err
}

// pruneRevisions keeps the concert's maxRevisions newest revisions.
func pruneRevisions(ctx context.Context, tx *sql.Tx, concertID int64) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM concert_revisions WHERE concert_id = ? AND id <=
		(SELECT id FROM concert_revisions WHERE concert_id = ? ORDER BY id DESC LIMIT 1 OFFSET ?)`,
		concertID, concertID, maxRevisions)
	return err
}
