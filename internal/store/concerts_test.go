package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestConcerts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openTest(t)
	g, _ := s.CreateGroup(ctx, "g", "s1")
	other, _ := s.CreateGroup(ctx, "other", "s2")
	lea, _ := s.CreateMember(ctx, g.ID, "Léa")

	add := func(c Concert) int64 {
		t.Helper()
		id, err := s.CreateConcert(ctx, g.ID, lea.ID, c, testToday)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	late := add(Concert{Artist: "Late", Date: "2027-03-14", Time: "21:00", Venue: "V", Address: "1 rue X, Paris",
		Price: "29–32 €", Description: "Un.\n\nDeux."})
	allDay := add(Concert{Artist: "All day", Date: "2027-03-14", Venue: "V", TicketURL: "https://t.example/x"})
	early := add(Concert{Artist: "Early", Date: "2027-01-01", Time: "20:00", Venue: "V", OnSaleDate: "2026-12-01"})

	cs, err := s.Concerts(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	var order []int64
	for _, c := range cs {
		order = append(order, c.ID)
	}
	if cs[2].Address != "1 rue X, Paris" || cs[0].Address != "" {
		t.Fatalf("addresses = %q, %q", cs[2].Address, cs[0].Address)
	}
	if cs[2].Price != "29–32 €" || cs[2].Description != "Un.\n\nDeux." || cs[0].Price != "" || cs[0].Description != "" {
		t.Fatalf("price, description = %q, %q; %q, %q", cs[2].Price, cs[2].Description, cs[0].Price, cs[0].Description)
	}
	if fmt.Sprint(order) != fmt.Sprint([]int64{early, allDay, late}) {
		t.Fatalf("order = %v, want early, all day (no time first), late", order)
	}
	if cs[0].AddedBy != "Léa" || cs[0].OnSaleDate != "2026-12-01" || cs[1].Time != "" || cs[1].TicketURL == "" {
		t.Fatalf("fields = %+v", cs[:2])
	}
	if len(cs[0].Token) != 24 || cs[0].Token == cs[1].Token {
		t.Fatalf("tokens = %q, %q: want distinct, 24 hex digits", cs[0].Token, cs[1].Token)
	}
	if gid, id, err := s.ConcertByToken(ctx, cs[0].Token); err != nil || gid != g.ID || id != early {
		t.Fatalf("ConcertByToken = %d, %d, %v", gid, id, err)
	}
	if _, _, err := s.ConcertByToken(ctx, "unknown"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ConcertByToken(unknown): %v", err)
	}

	upd := Concert{Artist: "Later", Date: "2027-04-01", Venue: "W"}
	if _, err := s.UpdateConcert(ctx, g.ID, lea.ID, late, upd, testToday); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Concert(ctx, g.ID, late); c.Artist != "Later" || c.Time != "" || c.Venue != "W" || c.Address != "" || c.Price != "" || c.Description != "" {
		t.Fatalf("after update: %+v", c)
	}

	// Another group sees nothing and changes nothing.
	if _, err := s.Concert(ctx, other.ID, late); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Concert from another group: %v", err)
	}
	_, updateErr := s.UpdateConcert(ctx, other.ID, lea.ID, late, upd, testToday)
	for name, err := range map[string]error{
		"update":  updateErr,
		"delete":  s.DeleteConcert(ctx, other.ID, late),
		"restore": s.RestoreConcert(ctx, other.ID, late, testToday),
	} {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s from another group: err = %v, want ErrNotFound", name, err)
		}
	}

	// Soft delete hides the concert from the list and from writes, until restored.
	if err := s.DeleteConcert(ctx, g.ID, late); err != nil {
		t.Fatal(err)
	}
	if c, err := s.Concert(ctx, g.ID, late); err != nil || !c.Deleted || c.Artist != "Later" {
		t.Fatalf("deleted Concert = %+v, %v; want it flagged Deleted", c, err)
	}
	if _, err := s.UpdateConcert(ctx, g.ID, lea.ID, late, upd, testToday); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update of a deleted concert: %v", err)
	}
	if err := s.DeleteConcert(ctx, g.ID, late); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if cs, _ := s.Concerts(ctx, g.ID); len(cs) != 2 {
		t.Fatalf("list still has %d concerts", len(cs))
	}
	if err := s.RestoreConcert(ctx, g.ID, late, testToday); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreConcert(ctx, g.ID, late, testToday); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second restore: %v", err)
	}

	// The concert outlives its creator.
	if err := s.DeleteMember(ctx, g.ID, "Léa"); err != nil {
		t.Fatal(err)
	}
	if c, err := s.Concert(ctx, g.ID, early); err != nil || c.AddedBy != "" {
		t.Fatalf("after deleting the creator: %+v, %v", c, err)
	}
}

// testToday is the Paris date callers pass; concerts dated before it are past.
const testToday = "2026-06-01"

// TestConcertErrorIsEmpty: on an error, Concert returns no half-read concert, whose Deleted
// would make callers answer 404 instead of 500.
func TestConcertErrorIsEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openTest(t)
	g, err := s.CreateGroup(ctx, "g", "s")
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.CreateMember(ctx, g.ID, "Léa")
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.CreateConcert(ctx, g.ID, m.ID, Concert{Artist: "A", Date: "2026-07-01", Venue: "V"}, "2026-01-02")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteConcert(ctx, g.ID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "DROP TABLE concert_revisions"); err != nil {
		t.Fatal(err)
	}
	if c, err := s.Concert(ctx, g.ID, id); err == nil || c != (Concert{}) {
		t.Errorf("Concert = %+v, %v; want an empty concert and the error", c, err)
	}
}

func TestConcertLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openTest(t)
	g, _ := s.CreateGroup(ctx, "g", "s")
	m, _ := s.CreateMember(ctx, g.ID, "Léa")
	const past, later = "2026-05-31", "2026-07-01"
	create := func(date string) (int64, error) {
		return s.CreateConcert(ctx, g.ID, m.ID, Concert{Artist: "A", Date: date, Venue: "V"}, testToday)
	}
	mustCreate := func(date string) int64 {
		t.Helper()
		id, err := create(date)
		if err != nil {
			t.Fatalf("create %s: %v", date, err)
		}
		return id
	}
	update := func(id int64, date string) error {
		_, err := s.UpdateConcert(ctx, g.ID, m.ID, id, Concert{Artist: "A", Date: date, Venue: "V"}, testToday)
		return err
	}
	// fill adds n rows in one statement; the limits are then crossed through the API.
	fill := func(n int, date string) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
			INSERT INTO concerts (group_id, token, artist, date, venue, created_at)
			SELECT ?, lower(hex(randomblob(12))), 'A', ?, 'V', '2026-01-01T00:00:00Z' FROM n`, n, g.ID, date); err != nil {
			t.Fatal(err)
		}
	}
	count := func(where string) (n int) {
		t.Helper()
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM concerts WHERE group_id = ? AND "+where, g.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	gone := func(name string, id int64) {
		t.Helper()
		if _, err := s.Concert(ctx, g.ID, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s: err = %v, want ErrNotFound (purged)", name, err)
		}
	}

	// Deleted and past concerts do not count against the limit; a concert dated today does.
	deleted := mustCreate(later)
	if err := s.DeleteConcert(ctx, g.ID, deleted); err != nil {
		t.Fatal(err)
	}
	pastID := mustCreate(past)
	fill(MaxConcerts-1, testToday)
	upcoming := mustCreate(later)
	for _, date := range []string{testToday, later} {
		if _, err := create(date); !errors.Is(err, ErrLimit) {
			t.Fatalf("create %s past the limit: err = %v, want ErrLimit", date, err)
		}
	}
	mustCreate(past)

	// Restoring or editing a concert into the future cannot exceed the limit; into the past can.
	if err := s.RestoreConcert(ctx, g.ID, deleted, testToday); !errors.Is(err, ErrLimit) {
		t.Fatalf("restore past the limit: err = %v, want ErrLimit", err)
	}
	if c, _ := s.Concert(ctx, g.ID, deleted); !c.Deleted {
		t.Fatal("a refused restore still restored the concert")
	}
	if err := update(pastID, later); !errors.Is(err, ErrLimit) {
		t.Fatalf("edit into the future past the limit: err = %v, want ErrLimit", err)
	}
	if c, _ := s.Concert(ctx, g.ID, pastID); c.Date != past {
		t.Fatalf("a refused edit still changed the date to %s", c.Date)
	}
	if rs, err := s.Revisions(ctx, g.ID, pastID); err != nil || len(rs) != 0 {
		t.Fatalf("a refused edit recorded %d revisions (%v)", len(rs), err)
	}
	if err := update(upcoming, testToday); err != nil {
		t.Fatalf("edit an upcoming concert at the limit: %v", err)
	}
	if err := update(upcoming, past); err != nil {
		t.Fatalf("edit into the past at the limit: %v", err)
	}
	if err := s.DeleteConcert(ctx, g.ID, upcoming); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreConcert(ctx, g.ID, upcoming, testToday); err != nil {
		t.Fatalf("restore a past concert at the limit: %v", err)
	}

	// At 2×MaxConcerts rows a create purges a row instead of locking the group: the oldest
	// deleted concert first, then the oldest past one with its statuses, never an upcoming one.
	oldest := mustCreate("2025-01-01")
	if err := s.SetStatus(ctx, g.ID, oldest, m.ID, "going"); err != nil {
		t.Fatal(err)
	}
	if err := update(oldest, "2025-01-02"); err != nil {
		t.Fatal(err)
	}
	fill(2*MaxConcerts-count("1"), "2026-03-01")
	mustCreate(past)
	gone("oldest deleted concert", deleted)
	if _, err := s.Concert(ctx, g.ID, oldest); err != nil {
		t.Fatalf("oldest past concert purged before the deleted one: %v", err)
	}
	mustCreate(past)
	gone("oldest past concert", oldest)
	var statuses int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM statuses WHERE concert_id = ?", oldest).Scan(&statuses); err != nil || statuses != 0 {
		t.Fatalf("statuses of the purged concert: %d, %v; want 0", statuses, err)
	}
	var revisions int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM concert_revisions WHERE concert_id = ?", oldest).Scan(&revisions); err != nil || revisions != 0 {
		t.Fatalf("revisions of the purged concert: %d, %v; want 0", revisions, err)
	}
	if rows, up := count("1"), count("date >= '"+testToday+"'"); rows != 2*MaxConcerts || up != MaxConcerts-1 {
		t.Fatalf("rows = %d, upcoming = %d; want %d, %d", rows, up, 2*MaxConcerts, MaxConcerts-1)
	}
}

func TestSameConcert(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openTest(t)
	g, _ := s.CreateGroup(ctx, "g", "s1")
	other, _ := s.CreateGroup(ctx, "other", "s2")
	lea, _ := s.CreateMember(ctx, g.ID, "Léa")
	add := func(groupID int64, artist, date string) int64 {
		t.Helper()
		id, err := s.CreateConcert(ctx, groupID, lea.ID, Concert{Artist: artist, Date: date, Venue: "V"}, testToday)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	deleted := add(g.ID, "Émilie Simon", "2027-03-14")
	if err := s.DeleteConcert(ctx, g.ID, deleted); err != nil {
		t.Fatal(err)
	}
	first := add(g.ID, "Émilie Simon", "2027-03-14")
	add(g.ID, "émilie simon", "2027-03-14")
	add(g.ID, "Air", "2027-03-14")
	add(other.ID, "Justice", "2027-03-14")
	add(g.ID, "Justice", "2027-03-15")

	c, err := s.SameConcert(ctx, g.ID, "ÉMILIE SIMON", "2027-03-14")
	if err != nil || c.ID != first || c.AddedBy != "Léa" {
		t.Fatalf("SameConcert = %+v, %v; want the oldest live one, %d", c, err, first)
	}
	for _, artist := range []string{"Justice", "Émilie"} {
		if _, err := s.SameConcert(ctx, g.ID, artist, "2027-03-14"); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", artist, err)
		}
	}
}
