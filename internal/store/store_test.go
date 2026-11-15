package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedNow() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }

func TestMigrateFromEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "test.db"), fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ms, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	var v int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != len(ms) {
		t.Fatalf("user_version = %d, want %d", v, len(ms))
	}

	g, err := s.CreateGroup(ctx, "Friends", "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GroupBySecret(ctx, "s3cret")
	if err != nil || got != g {
		t.Fatalf("GroupBySecret = %v, %v; want %v", got, err, g)
	}
	if _, err := s.GroupBySecret(ctx, "other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown secret: err = %v, want ErrNotFound", err)
	}
}

func TestOpenRefusesNewerSchema(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA user_version = 9999"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Open(ctx, path, fixedNow)
	if err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("Open = %v, want a 'newer' schema error", err)
	}
}

func TestBackup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, filepath.Join(dir, "live.db"), fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.CreateGroup(ctx, "Friends", "s3cret"); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "backup.db")
	if err := s.Backup(ctx, dst); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup(ctx, dst); err == nil {
		t.Fatal("second backup to the same file: want an error, got nil")
	}

	b, err := Open(ctx, dst, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	if _, err := b.GroupBySecret(ctx, "s3cret"); err != nil {
		t.Fatalf("backup lacks the group: %v", err)
	}
}
