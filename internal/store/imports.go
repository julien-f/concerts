package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// memberReserve is the part of MaxConcerts imports never take, so members can still add concerts.
const memberReserve = 100

// maxSourceIDBytes caps a venue's event id: a URL at most, as the venue serves it.
const maxSourceIDBytes = 256

// ErrSourceID is returned for a source id CleanSourceID refuses.
var ErrSourceID = errors.New("invalid source id")

// Event is one concert a venue lists: its editable fields, already cleaned and validated
// (validate.Concert), and the venue's stable id for it.
type Event struct {
	SourceID string
	Concert
}

// ImportCounts is what ApplyImports did with each event.
type ImportCounts struct {
	Inserted int
	Updated  int // rows the import owns, changed
	Moved    int // of Updated, those whose date changed
	// Skipped:
	Past      int // dated before today
	Duplicate int // the group already has it, from another source or added by hand, live or deleted
	Kept      int // a member deleted or edited its row
	Full      int // over the imports' share of MaxConcerts
	BadID     int // source id refused by CleanSourceID
	// Unfollowed: every event, when the group stopped following the source after the sync read its followers.
	Unfollowed int
}

// CleanSourceID collapses whitespace runs in id; it reports false unless the result is
// 1 to maxSourceIDBytes bytes of printable UTF-8.
func CleanSourceID(id string) (string, bool) {
	id = strings.Join(strings.Fields(id), " ")
	return id, id != "" && len(id) <= maxSourceIDBytes && utf8.ValidString(id) &&
		!strings.ContainsFunc(id, func(r rune) bool { return !unicode.IsGraphic(r) })
}

// artistSeparators join the artists of a bill: « A + B » is a duplicate of « A ».
var artistSeparators = []string{" + ", " • ", " / ", ", ", " & "}

// sameBill reports whether the artist keys are equal or one is the other followed by a separator.
func sameBill(a, b string) bool {
	a, b = nameKey(a), nameKey(b)
	if len(a) < len(b) {
		a, b = b, a
	}
	rest, ok := strings.CutPrefix(a, b)
	return ok && (rest == "" || slices.ContainsFunc(artistSeparators, func(sep string) bool { return strings.HasPrefix(rest, sep) }))
}

// ApplyImports applies a venue's events to the group in one transaction, none if the group no
// longer follows the venue (Unfollowed): a new event is
// inserted unless the group has the same bill that day from elsewhere; a row the import owns
// (live, never edited) is updated in place without a revision; a deleted or edited row is left
// as is. Inserts and moves into the future stop at MaxConcerts-memberReserve upcoming concerts.
func (s *Store) ApplyImports(ctx context.Context, groupID int64, source string, events []Event, today string) (ImportCounts, error) {
	var n ImportCounts
	if source == "" {
		return n, errors.New("import without a source")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return n, err
	}
	defer func() { _ = tx.Rollback() }()
	var following bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM group_venues WHERE group_id = ? AND venue = ?)",
		groupID, source).Scan(&following); err != nil {
		return n, err
	}
	if !following {
		n.Unfollowed = len(events)
		return n, nil
	}
	var upcoming int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM concerts WHERE group_id = ? AND deleted_at IS NULL AND date >= ?",
		groupID, today).Scan(&upcoming); err != nil {
		return n, err
	}
	for _, e := range events {
		id, ok := CleanSourceID(e.SourceID)
		switch {
		case !ok:
			n.BadID++
			continue
		case e.Date < today:
			n.Past++
			continue
		}
		var rowID int64
		var date string
		var deleted, edited bool
		err := tx.QueryRowContext(ctx, `SELECT id, date, deleted_at IS NOT NULL,
			EXISTS (SELECT 1 FROM concert_revisions r WHERE r.concert_id = concerts.id)
			FROM concerts WHERE group_id = ? AND source = ? AND source_id = ?`,
			groupID, source, id).Scan(&rowID, &date, &deleted, &edited)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if dup, err := hasBill(ctx, tx, groupID, source, e.Concert); err != nil {
				return n, err
			} else if dup {
				n.Duplicate++
				continue
			}
			if upcoming >= MaxConcerts-memberReserve {
				n.Full++
				continue
			}
			inserted, err := s.insertImport(ctx, tx, groupID, source, id, e.Concert, today)
			if err != nil {
				return n, err
			}
			if inserted {
				n.Inserted++
				upcoming++
			}
		case err != nil:
			return n, err
		case deleted || edited:
			n.Kept++
		default:
			intoFuture := date < today
			if intoFuture && upcoming >= MaxConcerts-memberReserve {
				n.Full++
				continue
			}
			res, err := tx.ExecContext(ctx, "UPDATE concerts SET "+setEditable+` WHERE id = ? AND deleted_at IS NULL
				AND NOT EXISTS (SELECT 1 FROM concert_revisions r WHERE r.concert_id = concerts.id) AND `+differs,
				append(append(editable(e.Concert), rowID), editable(e.Concert)...)...)
			if err != nil {
				return n, err
			}
			if changed, err := res.RowsAffected(); err != nil {
				return n, err
			} else if changed > 0 {
				n.Updated++
				if date != e.Date {
					n.Moved++
				}
				if intoFuture {
					upcoming++
				}
			}
		}
	}
	return n, commitWithin(ctx, tx, groupID, today)
}

// hasBill reports whether the group has a concert on c's date, live or deleted, from another
// source or added by hand, with the same bill: a source's own rows are its other shows that day.
func hasBill(ctx context.Context, tx *sql.Tx, groupID int64, source string, c Concert) (bool, error) {
	cs, err := concertsOn(ctx, tx, groupID, c.Date)
	if err != nil {
		return false, err
	}
	for _, o := range cs {
		if o.Source != source && sameBill(o.Artist, c.Artist) {
			return true, nil
		}
	}
	return false, nil
}

// insertImport adds an imported concert, purging a row at the row cap; false if the row already exists.
func (s *Store) insertImport(ctx context.Context, tx *sql.Tx, groupID int64, source, sourceID string, c Concert, today string) (bool, error) {
	if err := purgeIfFull(ctx, tx, groupID, today); err != nil {
		return false, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO concerts
		(group_id, token, artist, date, time, venue, address, ticket_url, on_sale_date, price, description, sold_out,
			source, source_id, created_at)
		VALUES (?, ?, ?, ?, nullif(?, ''), ?, nullif(?, ''), nullif(?, ''), nullif(?, ''), nullif(?, ''), nullif(?, ''), ?, ?, ?, ?)
		ON CONFLICT (group_id, source, source_id) WHERE source IS NOT NULL DO NOTHING`,
		append(append([]any{groupID, newToken()}, editable(c)...), source, sourceID, s.timestamp())...)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
