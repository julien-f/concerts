package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

// TestMigrateFromPreviousSlice builds a database as the previous slice left it,
// with data, then checks Open migrates it and the data survives.
func TestMigrateFromPreviousSlice(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	ms, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, db, ms[:len(ms)-1]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO groups (id, name, secret_hash, created_at) VALUES (1, 'Old', ?, '2026-01-01T00:00:00Z');
		INSERT INTO members (id, group_id, name, name_key, created_at) VALUES (1, 1, 'Léa', 'léa', '2026-01-01T00:00:00Z');
		INSERT INTO concerts (id, group_id, artist, date, venue, created_by, created_at)
			VALUES (1, 1, 'A', '2026-03-14', 'V', 1, '2026-01-01T00:00:00Z');`,
		hashSecret("old")); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(ctx, path, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	g, err := s.GroupBySecret(ctx, "old")
	if err != nil {
		t.Fatal(err)
	}
	if c, err := s.Concert(ctx, g.ID, 1); err != nil || c.AddedBy != "Léa" || c.Address != "" {
		t.Fatalf("Concert = %+v, %v", c, err)
	}
	if err := s.SetStatus(ctx, g.ID, 1, 1, Going); err != nil {
		t.Fatal(err)
	}
}

func TestMembers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openTest(t)
	g, _ := s.CreateGroup(ctx, "g", "s1")
	other, _ := s.CreateGroup(ctx, "other", "s2")

	for _, name := range []string{"Zoé", "élodie", "Marc"} {
		if _, err := s.CreateMember(ctx, g.ID, name); err != nil {
			t.Fatal(err)
		}
	}
	marc, err := s.CreateMember(ctx, g.ID, "MARC")
	if !errors.Is(err, ErrDuplicate) || marc.Name != "Marc" {
		t.Fatalf("CreateMember(MARC) = %v, %v; want ErrDuplicate with the existing Marc", marc, err)
	}
	if _, err := s.CreateMember(ctx, g.ID, "Élodie"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("CreateMember(Élodie) err = %v; want ErrDuplicate (Unicode case folding)", err)
	}
	if _, err := s.CreateMember(ctx, other.ID, "Marc"); err != nil {
		t.Fatalf("same name in another group: %v", err)
	}

	ms, err := s.Members(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(ms); got != fmt.Sprintf("[{%d Marc} {%d Zoé} {%d élodie}]", marc.ID, ms[1].ID, ms[2].ID) {
		t.Fatalf("Members = %s", got)
	}
	if _, err := s.Member(ctx, other.ID, marc.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Member from another group: err = %v, want ErrNotFound", err)
	}

	if err := s.DeleteMember(ctx, g.ID, "marc"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMember(ctx, g.ID, "marc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second DeleteMember: err = %v, want ErrNotFound", err)
	}
}

func TestGroupsAndRotateSecret(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openTest(t)
	g, _ := s.CreateGroup(ctx, "g", "old")
	if _, err := s.CreateMember(ctx, g.ID, "Léa"); err != nil {
		t.Fatal(err)
	}

	gs, err := s.Groups(ctx)
	if err != nil || len(gs) != 1 || gs[0].Members != 1 || gs[0].Name != "g" {
		t.Fatalf("Groups = %+v, %v", gs, err)
	}

	if err := s.RotateSecret(ctx, g.ID, "new"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GroupBySecret(ctx, "old"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old secret still works: %v", err)
	}
	if got, err := s.GroupBySecret(ctx, "new"); err != nil || got.ID != g.ID {
		t.Fatalf("GroupBySecret(new) = %v, %v", got, err)
	}
	if err := s.RotateSecret(ctx, 999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown group: err = %v, want ErrNotFound", err)
	}
}
