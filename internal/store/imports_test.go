package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

const later = "2026-07-01" // after testToday

func ev(id, artist, date string) Event {
	return Event{SourceID: id, Concert: Concert{Artist: artist, Date: date, Venue: "V"}}
}

func mustApply(t *testing.T, s *Store, groupID int64, source, today string, events ...Event) ImportCounts {
	t.Helper()
	n, err := s.ApplyImports(context.Background(), groupID, source, events, today)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// importedID returns the id of the group's row for source's event id, 0 if none.
func importedID(t *testing.T, s *Store, groupID int64, source, id string) (rowID int64) {
	t.Helper()
	err := s.db.QueryRowContext(context.Background(), "SELECT id FROM concerts WHERE group_id = ? AND source = ? AND source_id = ?",
		groupID, source, id).Scan(&rowID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	return rowID
}

func TestApplyImports(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tests := []struct {
		name   string
		setup  func(t *testing.T, s *Store, g, m int64) // m: a member of group g
		events []Event
		want   ImportCounts
		check  func(t *testing.T, s *Store, g int64)
	}{
		{"new events inserted; past and bad ids skipped; id cleaned", nil,
			[]Event{ev("a", "A", later), ev(" b \t c ", "B", testToday), ev("p", "P", "2026-05-31"), ev(" ", "X", later),
				ev(strings.Repeat("x", 257), "X", later), ev("x\x00", "X", later)},
			ImportCounts{Inserted: 2, Past: 1, BadID: 3},
			func(t *testing.T, s *Store, g int64) {
				c, err := s.Concert(ctx, g, importedID(t, s, g, "src", "b c"))
				if err != nil || c.Source != "src" || c.AddedBy != "" || len(c.Token) != 24 {
					t.Errorf("imported row = %+v, %v", c, err)
				}
			}},
		{"256-byte id accepted", nil, []Event{ev(strings.Repeat("é", 128), "A", later)}, ImportCounts{Inserted: 1}, nil},
		{"owned row updated in place, without a revision",
			func(t *testing.T, s *Store, g, _ int64) { mustApply(t, s, g, "src", testToday, ev("a", "A", later)) },
			[]Event{{SourceID: "a", Concert: Concert{Artist: "A2", Date: "2026-07-02", Time: "20:00", Venue: "V", Price: "Complet"}}},
			ImportCounts{Updated: 1, Moved: 1},
			func(t *testing.T, s *Store, g int64) {
				id := importedID(t, s, g, "src", "a")
				if c, _ := s.Concert(ctx, g, id); c.Artist != "A2" || c.Date != "2026-07-02" || c.Time != "20:00" || c.Price != "Complet" || c.LastRevID != 0 {
					t.Errorf("updated row = %+v", c)
				}
			}},
		{"unchanged row not counted",
			func(t *testing.T, s *Store, g, _ int64) { mustApply(t, s, g, "src", testToday, ev("a", "A", later)) },
			[]Event{ev("a", "A", later)}, ImportCounts{}, nil},
		{"tombstone never revived",
			func(t *testing.T, s *Store, g, _ int64) {
				mustApply(t, s, g, "src", testToday, ev("a", "A", later))
				if err := s.DeleteConcert(ctx, g, importedID(t, s, g, "src", "a")); err != nil {
					t.Fatal(err)
				}
			},
			[]Event{ev("a", "A2", later)}, ImportCounts{Kept: 1},
			func(t *testing.T, s *Store, g int64) {
				if c, _ := s.Concert(ctx, g, importedID(t, s, g, "src", "a")); !c.Deleted || c.Artist != "A" {
					t.Errorf("tombstone = %+v", c)
				}
			}},
		{"a member's edit stops updates",
			func(t *testing.T, s *Store, g, m int64) {
				mustApply(t, s, g, "src", testToday, ev("a", "A", later))
				if _, err := s.UpdateConcert(ctx, g, m, importedID(t, s, g, "src", "a"), Concert{Artist: "Mine", Date: later, Venue: "V"}, testToday); err != nil {
					t.Fatal(err)
				}
			},
			[]Event{ev("a", "A2", later)}, ImportCounts{Kept: 1},
			func(t *testing.T, s *Store, g int64) {
				if c, _ := s.Concert(ctx, g, importedID(t, s, g, "src", "a")); c.Artist != "Mine" {
					t.Errorf("edited row = %+v", c)
				}
			}},
		{"an edit then reverted still stops updates",
			func(t *testing.T, s *Store, g, m int64) {
				mustApply(t, s, g, "src", testToday, ev("a", "A", later))
				id := importedID(t, s, g, "src", "a")
				for _, artist := range []string{"Mine", "A"} {
					if _, err := s.UpdateConcert(ctx, g, m, id, Concert{Artist: artist, Date: later, Venue: "V"}, testToday); err != nil {
						t.Fatal(err)
					}
				}
			},
			[]Event{ev("a", "A2", later)}, ImportCounts{Kept: 1}, nil},
		{"another group's copy is its own",
			func(t *testing.T, s *Store, g, _ int64) {
				other := mustGroup(t, s, "other", "s2")
				mustApply(t, s, other.ID, "src", testToday, ev("a", "A", later))
				if err := s.DeleteConcert(ctx, other.ID, importedID(t, s, other.ID, "src", "a")); err != nil {
					t.Fatal(err)
				}
			},
			[]Event{ev("a", "A", later)}, ImportCounts{Inserted: 1}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := openTest(t)
			g := mustGroup(t, s, "g", "s")
			m := mustMember(t, s, g.ID, "Léa")
			if tt.setup != nil {
				tt.setup(t, s, g.ID, m.ID)
			}
			if got := mustApply(t, s, g.ID, "src", testToday, tt.events...); got != tt.want {
				t.Errorf("counts = %+v, want %+v", got, tt.want)
			}
			if tt.check != nil {
				tt.check(t, s, g.ID)
			}
		})
	}
}

func TestApplyImportsRefusesNoSource(t *testing.T) {
	t.Parallel()
	s := openTest(t)
	g := mustGroup(t, s, "g", "s")
	if _, err := s.ApplyImports(context.Background(), g.ID, "", []Event{ev("a", "A", later)}, testToday); err == nil {
		t.Fatal("import without a source: want an error")
	}
}

// TestApplyImportsDedup: a new event is dropped when the group has the same bill that day,
// from another source or by hand, live or deleted; a source's own rows are its other shows.
func TestApplyImportsDedup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tests := []struct {
		name, source, artist, date string // existing row; source "" = added by hand
		deleted                    bool
		event                      string // artist of the event, dated later, from "src"
		dup                        bool
	}{
		{"same artist by hand", "", "Zaz", later, false, "zaz", true},
		{"hand row is the event plus a guest", "", "Zaz + Guest", later, false, "ZAZ", true},
		{"event is the hand row plus a guest", "", "Zaz", later, false, "Zaz • Guest", true},
		{"separator /", "", "Zaz / Guest", later, false, "Zaz", true},
		{"separator ,", "", "Zaz", later, false, "Zaz, Guest", true},
		{"separator &", "", "Zaz & Guest", later, false, "Zaz", true},
		{"deleted by a member", "", "Zaz", later, true, "Zaz", true},
		{"another source", "other", "Zaz", later, false, "Zaz + Guest", true},
		{"another source, deleted", "other", "Zaz", later, true, "Zaz", true},
		{"prefix without separator", "", "Zaz", later, false, "Zazie", false},
		{"separator without spaces", "", "Zaz", later, false, "Zaz+Guest", false},
		{"another day", "", "Zaz", "2026-07-02", false, "Zaz", false},
		{"same source, another id: another show", "src", "Zaz", later, false, "Zaz", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := openTest(t)
			g := mustGroup(t, s, "g", "s")
			var id int64
			if tt.source == "" {
				id = mustConcert(t, s, g.ID, mustMember(t, s, g.ID, "Léa").ID, Concert{Artist: tt.artist, Date: tt.date, Venue: "V"}, testToday)
			} else {
				mustApply(t, s, g.ID, tt.source, testToday, ev("old", tt.artist, tt.date))
				id = importedID(t, s, g.ID, tt.source, "old")
			}
			if tt.deleted {
				if err := s.DeleteConcert(ctx, g.ID, id); err != nil {
					t.Fatal(err)
				}
			}
			want := ImportCounts{Inserted: 1}
			if tt.dup {
				want = ImportCounts{Duplicate: 1}
			}
			if got := mustApply(t, s, g.ID, "src", testToday, ev("new", tt.event, later)); got != want {
				t.Errorf("counts = %+v, want %+v", got, want)
			}
		})
	}
}

// TestApplyImportsHeadroom: imports stop at MaxConcerts-memberReserve upcoming concerts,
// counted once per transaction, moves of a past row into the future included.
func TestApplyImportsHeadroom(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openTest(t)
	g := mustGroup(t, s, "g", "s")
	m := mustMember(t, s, g.ID, "Léa")
	mustApply(t, s, g.ID, "src", "2026-05-01", ev("past", "P", "2026-05-31"))
	if _, err := s.db.ExecContext(ctx, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		INSERT INTO concerts (group_id, token, artist, date, venue, created_at)
		SELECT ?, lower(hex(randomblob(12))), 'Filler', ?, 'V', '2026-01-01T00:00:00Z' FROM n`,
		MaxConcerts-memberReserve-1, g.ID, later); err != nil {
		t.Fatal(err)
	}
	// Room for one: the move into the future takes it, the new events are refused.
	got := mustApply(t, s, g.ID, "src", testToday, ev("past", "P", later), ev("a", "A", later), ev("b", "B", later))
	if want := (ImportCounts{Updated: 1, Moved: 1, Full: 2}); got != want {
		t.Fatalf("counts = %+v, want %+v", got, want)
	}
	if got := mustApply(t, s, g.ID, "src", testToday, ev("a", "A", later)); got != (ImportCounts{Full: 1}) {
		t.Fatalf("at the headroom: counts = %+v", got)
	}
	// Members keep their share.
	mustConcert(t, s, g.ID, m.ID, Concert{Artist: "Mine", Date: later, Venue: "V"}, testToday)
}

// TestPurgeOrder: at the row cap, rows go oldest first by category, never a live upcoming one.
func TestPurgeOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openTest(t)
	g := mustGroup(t, s, "g", "s")
	m := mustMember(t, s, g.ID, "Léa")
	row := func(source, date string, deleted, answered bool) int64 {
		t.Helper()
		var id int64
		if source == "" {
			id = mustConcert(t, s, g.ID, m.ID, Concert{Artist: "A", Date: date, Venue: "V"}, "2026-01-01")
		} else {
			mustApply(t, s, g.ID, source, "2026-01-01", ev(date+source, "A", date))
			id = importedID(t, s, g.ID, source, date+source)
		}
		if answered {
			if err := s.SetStatus(ctx, g.ID, id, m.ID, Interested); err != nil {
				t.Fatal(err)
			}
		}
		if deleted {
			if err := s.DeleteConcert(ctx, g.ID, id); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	upcomingTombstone := row("src", "2026-06-02", true, false)
	pastAnswered := row("src", "2026-05-02", false, true)
	pastByHand := row("", "2026-05-01", false, false)
	upcomingDeleted := row("", "2026-06-03", true, false)
	pastUnanswered := row("src", "2026-05-03", false, false)
	pastDeleted := row("", "2026-05-04", true, false)
	keep := []int64{row("src", "2026-06-04", false, false), row("", "2026-06-05", false, false)}
	want := []int64{pastDeleted, pastUnanswered, upcomingDeleted, pastByHand, pastAnswered, upcomingTombstone}

	fill := func() { // to the row cap, with live upcoming rows
		t.Helper()
		var n int
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM concerts WHERE group_id = ?", g.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.ExecContext(ctx, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
			INSERT INTO concerts (group_id, token, artist, date, venue, created_at)
			SELECT ?, lower(hex(randomblob(12))), 'Filler', ?, 'V', '2026-01-01T00:00:00Z' FROM n`, 2*MaxConcerts-n, g.ID, later); err != nil {
			t.Fatal(err)
		}
	}
	exists := func(id int64) bool {
		_, err := s.Concert(ctx, g.ID, id)
		if err != nil && !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		return err == nil
	}
	purge := func() {
		t.Helper()
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		if err := purgeIfFull(ctx, tx, g.ID, testToday); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	for i, id := range append(want, 0) {
		fill()
		purge()
		for _, gone := range want[:i] {
			if exists(gone) {
				t.Fatalf("purge %d: row %d came back", i, gone)
			}
		}
		for _, other := range append(want[min(i+1, len(want)):], keep...) {
			if !exists(other) {
				t.Fatalf("purge %d: row %d purged, want %d first", i, other, id)
			}
		}
		if id != 0 && exists(id) {
			t.Fatalf("purge %d: row %d kept", i, id)
		}
	}
}
