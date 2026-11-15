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

	// Soft delete hides the concert everywhere but DeletedConcert, until restored.
	if err := s.DeleteConcert(ctx, g.ID, late); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Concert(ctx, g.ID, late); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted concert still found: %v", err)
	}
	if err := s.UpdateConcert(ctx, g.ID, late, upd); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update of a deleted concert: %v", err)
	}
	if c, err := s.DeletedConcert(ctx, g.ID, late); err != nil || c.Artist != "Later" {
		t.Fatalf("DeletedConcert = %+v, %v", c, err)
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
	var last int64
	for range MaxConcerts {
		id, err := s.CreateConcert(ctx, g.ID, m.ID, c)
		if err != nil {
			t.Fatal(err)
		}
		last = id
	}
	if _, err := s.CreateConcert(ctx, g.ID, m.ID, c); !errors.Is(err, ErrLimit) {
		t.Fatalf("err = %v, want ErrLimit", err)
	}
	// Deleted concerts do not count.
	if err := s.DeleteConcert(ctx, g.ID, last); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateConcert(ctx, g.ID, m.ID, c); err != nil {
		t.Fatal(err)
	}
}
