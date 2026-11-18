package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestFollow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openTest(t)
	g := mustGroup(t, s, "g", "s1")
	other := mustGroup(t, s, "other", "s2")
	for _, f := range []struct {
		group int64
		venue string
	}{{other.ID, "mc2"}, {g.ID, "mc2"}, {g.ID, "la-source"}, {g.ID, "mc2"}} {
		if err := s.Follow(ctx, f.group, f.venue); err != nil {
			t.Fatalf("Follow(%d, %s): %v", f.group, f.venue, err)
		}
	}
	if err := s.Follow(ctx, 999, "mc2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Follow(unknown group): err = %v, want ErrNotFound", err)
	}
	if vs, err := s.Followed(ctx, g.ID); err != nil || fmt.Sprint(vs) != "[la-source mc2]" {
		t.Errorf("Followed = %v, %v", vs, err)
	}
	if ids, err := s.Followers(ctx, "mc2"); err != nil || fmt.Sprint(ids) != fmt.Sprint([]int64{g.ID, other.ID}) {
		t.Errorf("Followers = %v, %v", ids, err)
	}
	if ids, err := s.Followers(ctx, "none"); err != nil || len(ids) != 0 {
		t.Errorf("Followers(none) = %v, %v", ids, err)
	}
}

// TestUnfollow: the venue's live rows nobody answered or edited go, past ones included; the
// others, its tombstones, other venues' rows and other groups' rows stay.
func TestUnfollow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openTest(t)
	g := mustGroup(t, s, "g", "s1")
	other := mustGroup(t, s, "other", "s2")
	m := mustMember(t, s, g.ID, "Léa")
	for _, gr := range []int64{g.ID, other.ID} {
		if err := s.Follow(ctx, gr, "src"); err != nil {
			t.Fatal(err)
		}
	}
	mustApply(t, s, g.ID, "src", "2026-05-01", ev("past", "P", "2026-05-31"))
	mustApply(t, s, g.ID, "src", testToday, ev("a", "A", later), ev("answered", "B", later), ev("edited", "C", later), ev("deleted", "D", later))
	mustApply(t, s, g.ID, "venue2", testToday, ev("x", "X", later))
	mustApply(t, s, other.ID, "src", testToday, ev("a", "A", later))
	id := func(source, sourceID string) int64 { return importedID(t, s, g.ID, source, sourceID) }
	if err := s.SetStatus(ctx, g.ID, id("src", "answered"), m.ID, Going); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateConcert(ctx, g.ID, m.ID, id("src", "edited"), Concert{Artist: "C2", Date: later, Venue: "V"}, testToday); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteConcert(ctx, g.ID, id("src", "deleted")); err != nil {
		t.Fatal(err)
	}

	if n, err := s.Unfollow(ctx, g.ID, "src"); err != nil || n != 2 {
		t.Fatalf("Unfollow = %d, %v; want 2 rows deleted", n, err)
	}
	for sourceID, kept := range map[string]bool{"past": false, "a": false, "answered": true, "edited": true, "deleted": true} {
		if got := id("src", sourceID) != 0; got != kept {
			t.Errorf("%s: kept = %v, want %v", sourceID, got, kept)
		}
	}
	if id("venue2", "x") == 0 || importedID(t, s, other.ID, "src", "a") == 0 {
		t.Error("another venue's or group's row deleted")
	}
	if vs, _ := s.Followed(ctx, g.ID); fmt.Sprint(vs) != "[venue2]" {
		t.Errorf("following %v, want [venue2]", vs)
	}
	if ids, _ := s.Followers(ctx, "src"); fmt.Sprint(ids) != fmt.Sprint([]int64{other.ID}) {
		t.Errorf("Followers = %v", ids)
	}
	if _, err := s.Unfollow(ctx, g.ID, "src"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Unfollow: err = %v, want ErrNotFound", err)
	}
}

func TestEventDetails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openTest(t)
	put := func(source, id string, d EventDetail) {
		t.Helper()
		if err := s.PutEventDetail(ctx, source, id, d); err != nil {
			t.Fatal(err)
		}
	}
	put("src", "old", EventDetail{Date: "2026-05-31", Time: "20:00"})
	put("src", "a", EventDetail{Date: later})
	put("src", " a ", EventDetail{Date: later, Time: "20:30"}) // same id once cleaned: replaced
	put("other", "a", EventDetail{Date: testToday, Time: "19:00"})

	for _, tt := range []struct {
		source, id string
		want       EventDetail
		err        error
	}{
		{"src", "a", EventDetail{Date: later, Time: "20:30"}, nil},
		{"other", "a", EventDetail{Date: testToday, Time: "19:00"}, nil},
		{"src", "unknown", EventDetail{}, ErrNotFound},
		{"src", "", EventDetail{}, ErrSourceID},
	} {
		if got, err := s.EventDetail(ctx, tt.source, tt.id); got != tt.want || !errors.Is(err, tt.err) {
			t.Errorf("EventDetail(%s, %q) = %+v, %v; want %+v, %v", tt.source, tt.id, got, err, tt.want, tt.err)
		}
	}
	if err := s.PutEventDetail(ctx, "src", "\x01", EventDetail{Date: later}); !errors.Is(err, ErrSourceID) {
		t.Errorf("PutEventDetail(bad id): err = %v, want ErrSourceID", err)
	}
	if n, err := s.PruneEventDetails(ctx, testToday); err != nil || n != 1 {
		t.Fatalf("PruneEventDetails = %d, %v; want 1", n, err)
	}
	if _, err := s.EventDetail(ctx, "src", "old"); !errors.Is(err, ErrNotFound) {
		t.Errorf("pruned detail: err = %v", err)
	}
	if _, err := s.EventDetail(ctx, "other", "a"); err != nil {
		t.Errorf("detail dated today pruned: %v", err)
	}
}

// TestVenueEvents: what a venue last said of an event, kept once for every group, read through an import.
func TestVenueEvents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	s, err := Open(ctx, filepath.Join(t.TempDir(), "test.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	g := mustGroup(t, s, "g", "s1")
	m := mustMember(t, s, g.ID, "Léa")
	a := Event{SourceID: "a", Concert: Concert{Artist: "A", Date: later, Time: "20:00", Venue: "V", Price: "20 €", SoldOut: true}}
	mustApply(t, s, g.ID, "src", testToday, a)
	id := importedID(t, s, g.ID, "src", "a")
	byHand := mustConcert(t, s, g.ID, m.ID, a.Concert, testToday)

	if _, err := s.SourceEvent(ctx, g.ID, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("before any sync kept it: %v", err)
	}
	moved := a
	moved.Date, moved.Time, moved.SoldOut = "2026-07-02", "", false
	for _, es := range [][]Event{{a, ev(" ", "Bad id", later)}, {moved}} {
		if err := s.PutVenueEvents(ctx, "src", es); err != nil {
			t.Fatal(err)
		}
	}
	if c, err := s.SourceEvent(ctx, g.ID, id); err != nil || c != moved.Concert {
		t.Errorf("SourceEvent = %+v, %v; want the last values read %+v", c, err, moved.Concert)
	}
	for name, ids := range map[string][2]int64{"added by hand": {g.ID, byHand}, "another group": {mustGroup(t, s, "o", "s2").ID, id}} {
		if _, err := s.SourceEvent(ctx, ids[0], ids[1]); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}

	// Pruned once past, or unseen for 30 days; an event missing from one run stays.
	if err := s.PutVenueEvents(ctx, "src", []Event{ev("past", "P", "2026-05-31"), ev("b", "B", later)}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(29 * 24 * time.Hour)
	if err := s.PutVenueEvents(ctx, "src", []Event{ev("b", "B", later)}); err != nil {
		t.Fatal(err)
	}
	if n, err := s.PruneVenueEvents(ctx, testToday); err != nil || n != 1 {
		t.Errorf("PruneVenueEvents = %d, %v; want the past one", n, err)
	}
	now = now.Add(2 * 24 * time.Hour) // a unseen for 31 days, b for 2
	if n, err := s.PruneVenueEvents(ctx, testToday); err != nil || n != 1 {
		t.Errorf("PruneVenueEvents = %d, %v; want the one unseen for 31 days", n, err)
	}
	if _, err := s.SourceEvent(ctx, g.ID, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("unseen for 31 days, still kept: %v", err)
	}
}
