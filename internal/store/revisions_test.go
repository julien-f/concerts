package store

import (
	"context"
	"errors"
	"testing"
)

func TestRevisions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openTest(t)
	g := mustGroup(t, s, "g", "s1")
	other := mustGroup(t, s, "other", "s2")
	lea := mustMember(t, s, g.ID, "Léa")
	tom := mustMember(t, s, g.ID, "Tom")
	orig := Concert{Artist: "A", Date: "2027-03-14", Venue: "V"}
	id, err := s.CreateConcert(ctx, g.ID, lea.ID, orig, testToday)
	if err != nil {
		t.Fatal(err)
	}
	update := func(by int64, c Concert) int64 {
		t.Helper()
		rev, err := s.UpdateConcert(ctx, g.ID, by, id, c, testToday)
		if err != nil {
			t.Fatal(err)
		}
		return rev
	}

	// Never edited: no history.
	if c, _ := s.Concert(ctx, g.ID, id); c.LastRevID != 0 || c.EditedBy != "" {
		t.Fatalf("never edited: LastRevID = %d, EditedBy = %q", c.LastRevID, c.EditedBy)
	}
	if rs, err := s.Revisions(ctx, g.ID, id); err != nil || len(rs) != 0 {
		t.Fatalf("never edited: %d revisions, %v", len(rs), err)
	}

	// An unchanged form records nothing, empty optional fields included.
	if rev := update(tom.ID, orig); rev != 0 {
		t.Fatalf("no-op edit recorded revision %d", rev)
	}

	// An edit records the replaced values, its author and the clock's time.
	withTime := orig
	withTime.Time = "20:00"
	first := update(tom.ID, withTime)
	if first == 0 {
		t.Fatal("edit recorded no revision")
	}
	r, err := s.Revision(ctx, g.ID, id, first)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != first || r.Artist != "A" || r.Time != "" || r.Venue != "V" || r.EditedBy != "Tom" || !r.EditedAt.Equal(fixedNow()) {
		t.Fatalf("revision = %+v", r)
	}
	// Clearing an optional field is a change too.
	second := update(lea.ID, orig)
	if second == 0 {
		t.Fatal(`"20:00" → "" recorded no revision`)
	}
	if r, _ := s.Revision(ctx, g.ID, id, second); r.Time != "20:00" {
		t.Fatalf("second revision time = %q, want the replaced 20:00", r.Time)
	}
	if c, _ := s.Concert(ctx, g.ID, id); c.LastRevID != second || c.EditedBy != "Léa" {
		t.Fatalf("LastRevID = %d, EditedBy = %q; want %d, Léa", c.LastRevID, c.EditedBy, second)
	}
	if rs, _ := s.Revisions(ctx, g.ID, id); len(rs) != 2 || rs[0].ID != second || rs[1].ID != first {
		t.Fatalf("revisions not newest first: %+v", rs)
	}

	// Another group, another concert or an unknown id finds nothing.
	otherID := mustConcert(t, s, other.ID, lea.ID, orig, testToday)
	if _, err := s.Revision(ctx, other.ID, id, first); !errors.Is(err, ErrNotFound) {
		t.Errorf("revision from another group: %v", err)
	}
	if _, err := s.Revision(ctx, other.ID, otherID, first); !errors.Is(err, ErrNotFound) {
		t.Errorf("revision through another concert: %v", err)
	}
	if rs, _ := s.Revisions(ctx, other.ID, id); len(rs) != 0 {
		t.Errorf("history from another group: %d revisions", len(rs))
	}

	// A deleted concert's history is hidden, and back once restored.
	if err := s.DeleteConcert(ctx, g.ID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Revision(ctx, g.ID, id, first); !errors.Is(err, ErrNotFound) {
		t.Errorf("revision of a deleted concert: %v", err)
	}
	if rs, _ := s.Revisions(ctx, g.ID, id); len(rs) != 0 {
		t.Errorf("history of a deleted concert: %d revisions", len(rs))
	}
	if err := s.RestoreConcert(ctx, g.ID, id, testToday); err != nil {
		t.Fatal(err)
	}
	if rs, _ := s.Revisions(ctx, g.ID, id); len(rs) != 2 {
		t.Fatalf("history after restore: %d revisions, want 2", len(rs))
	}

	// Revisions outlive their author, without a name.
	if err := s.DeleteMember(ctx, g.ID, "Tom"); err != nil {
		t.Fatal(err)
	}
	if r, err := s.Revision(ctx, g.ID, id, first); err != nil || r.EditedBy != "" {
		t.Fatalf("after deleting the author: %+v, %v", r, err)
	}

	// Only the newest maxRevisions are kept.
	var last int64
	for i := range maxRevisions + 5 {
		c := orig
		c.Venue = string(rune('a' + i))
		last = update(lea.ID, c)
	}
	rs, _ := s.Revisions(ctx, g.ID, id)
	if len(rs) != maxRevisions || rs[0].ID != last || rs[len(rs)-1].ID != last-maxRevisions+1 {
		t.Fatalf("kept %d revisions, ids %d…%d; want the %d newest ending at %d", len(rs), rs[len(rs)-1].ID, rs[0].ID, maxRevisions, last)
	}
}
