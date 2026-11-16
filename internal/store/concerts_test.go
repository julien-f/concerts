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
		id, err := s.CreateConcert(ctx, g.ID, lea.ID, c)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	late := add(Concert{Artist: "Late", Date: "2027-03-14", Time: "21:00", Venue: "V"})
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
	if fmt.Sprint(order) != fmt.Sprint([]int64{early, allDay, late}) {
		t.Fatalf("order = %v, want early, all day (no time first), late", order)
	}
	if cs[0].AddedBy != "Léa" || cs[0].OnSaleDate != "2026-12-01" || cs[1].Time != "" || cs[1].TicketURL == "" {
		t.Fatalf("fields = %+v", cs[:2])
	}

	upd := Concert{Artist: "Later", Date: "2027-04-01", Venue: "W"}
	if err := s.UpdateConcert(ctx, g.ID, late, upd); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Concert(ctx, g.ID, late); c.Artist != "Later" || c.Time != "" || c.Venue != "W" {
		t.Fatalf("after update: %+v", c)
	}

	// Another group sees nothing and changes nothing.
	if _, err := s.Concert(ctx, other.ID, late); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Concert from another group: %v", err)
	}
	for name, err := range map[string]error{
		"update":  s.UpdateConcert(ctx, other.ID, late, upd),
		"delete":  s.DeleteConcert(ctx, other.ID, late),
		"restore": s.RestoreConcert(ctx, other.ID, late),
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
	if err := s.UpdateConcert(ctx, g.ID, late, upd); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update of a deleted concert: %v", err)
	}
	if err := s.DeleteConcert(ctx, g.ID, late); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if cs, _ := s.Concerts(ctx, g.ID); len(cs) != 2 {
		t.Fatalf("list still has %d concerts", len(cs))
	}
	if err := s.RestoreConcert(ctx, g.ID, late); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreConcert(ctx, g.ID, late); !errors.Is(err, ErrNotFound) {
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

func TestConcertLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openTest(t)
	g, _ := s.CreateGroup(ctx, "g", "s")
	m, _ := s.CreateMember(ctx, g.ID, "Léa")
	c := Concert{Artist: "A", Date: "2027-01-01", Venue: "V"}
	create := func() (int64, error) { return s.CreateConcert(ctx, g.ID, m.ID, c) }
	// fill adds n rows in one statement; the limits are then crossed through the API.
	fill := func(n int, deletedAt any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
			INSERT INTO concerts (group_id, artist, date, venue, created_at, deleted_at)
			SELECT ?, 'A', '2027-01-01', 'V', '2026-01-01T00:00:00Z', ? FROM n`, n, g.ID, deletedAt); err != nil {
			t.Fatal(err)
		}
	}

	// Deleted concerts kept for undo do not count against the live limit...
	early, _ := create()
	if err := s.DeleteConcert(ctx, g.ID, early); err != nil {
		t.Fatal(err)
	}
	fill(MaxConcerts-1, nil)
	if _, err := create(); err != nil {
		t.Fatalf("create up to the limit: %v", err)
	}
	if _, err := create(); !errors.Is(err, ErrLimit) {
		t.Fatalf("create past the limit: err = %v, want ErrLimit", err)
	}
	// ...but restoring one cannot exceed it.
	if err := s.RestoreConcert(ctx, g.ID, early); !errors.Is(err, ErrLimit) {
		t.Fatalf("restore past the limit: err = %v, want ErrLimit", err)
	}
	if c, _ := s.Concert(ctx, g.ID, early); !c.Deleted {
		t.Fatal("a refused restore still restored the concert")
	}

	// Create-then-delete cannot grow the table without bound either: at 2×MaxConcerts rows,
	// a create purges the oldest deleted concert, so the group never gets stuck.
	if _, err := s.db.ExecContext(ctx, "UPDATE concerts SET deleted_at = '2026-01-01T00:00:00Z' WHERE group_id = ?", g.ID); err != nil {
		t.Fatal(err)
	}
	fill(2*MaxConcerts-(MaxConcerts+1), "2026-02-01T00:00:00Z") // all deleted, at the cap
	if _, err := create(); err != nil {
		t.Fatalf("create with %d deleted rows: %v", 2*MaxConcerts, err)
	}
	var rows, newer int
	if err := s.db.QueryRowContext(ctx,
		"SELECT count(*), count(*) FILTER (WHERE deleted_at = '2026-02-01T00:00:00Z') FROM concerts WHERE group_id = ?",
		g.ID).Scan(&rows, &newer); err != nil {
		t.Fatal(err)
	}
	if rows != 2*MaxConcerts || newer != 2*MaxConcerts-(MaxConcerts+1) {
		t.Fatalf("rows = %d, newer deleted = %d; want %d, %d", rows, newer, 2*MaxConcerts, 2*MaxConcerts-(MaxConcerts+1))
	}
	if _, err := s.Concert(ctx, g.ID, early); !errors.Is(err, ErrNotFound) {
		t.Fatalf("oldest deleted concert: err = %v, want ErrNotFound (purged)", err)
	}
}
