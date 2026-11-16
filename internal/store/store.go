// Package store holds the SQLite database: connection, migrations and hand-written queries.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// ErrNotFound is returned when a looked-up row does not exist.
var ErrNotFound = errors.New("not found")

// Store wraps the database and the clock used for timestamps.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// Open opens (creating if needed) the database at path and applies pending migrations.
func Open(ctx context.Context, path string, now func() time.Time) (*Store, error) {
	// Pragmas live in the DSN so they apply to every pooled connection.
	dsn := "file:" + (&url.URL{Path: path}).EscapedPath() +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db, now: now}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Backup writes a consistent snapshot of the live database to dst, which must not exist.
// A failed snapshot is removed: a full disk would otherwise leave an empty file to restore.
func (s *Store) Backup(ctx context.Context, dst string) error {
	_, statErr := os.Stat(dst) //nolint:gosec // G703: dst is the operator's backup path
	_, err := s.db.ExecContext(ctx, "VACUUM INTO ?", dst)
	if err != nil && errors.Is(statErr, fs.ErrNotExist) {
		_ = os.Remove(dst) //nolint:gosec // G703: dst is the operator's backup path
	}
	return err
}

// Ping checks the database is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

type migration struct {
	version int
	name    string
}

func migrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}
	var ms []migration
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(prefix)
		if !ok || err != nil || v <= 0 {
			return nil, fmt.Errorf("migration %q: name must be NNNN_*.sql", e.Name())
		}
		ms = append(ms, migration{v, e.Name()})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	for i, m := range ms {
		if m.version != i+1 {
			return nil, fmt.Errorf("migration %q: expected version %d", m.name, i+1)
		}
	}
	return ms, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	ms, err := migrations()
	if err != nil {
		return err
	}
	return migrateTo(ctx, db, ms)
}

// migrateTo applies the pending migrations among ms; tests pass a prefix to build older schemas.
func migrateTo(ctx context.Context, db *sql.DB, ms []migration) error {
	var current int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	if current > len(ms) {
		return fmt.Errorf("database schema version %d is newer than this binary (%d): refusing to start", current, len(ms))
	}
	for _, m := range ms[current:] {
		body, err := migrationsFS.ReadFile("migrations/" + m.name)
		if err != nil {
			return err
		}
		if err := applyMigration(ctx, db, m.version, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
	}
	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, version int, body string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, body); err != nil {
		return err
	}
	// PRAGMA takes no placeholder; version is an int from our own file names.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return err
	}
	return tx.Commit()
}

// Group is a set of friends sharing one secret link.
type Group struct {
	ID   int64
	Name string
}

func hashSecret(secret string) []byte {
	h := sha256.Sum256([]byte(secret))
	return h[:]
}

// CreateGroup stores a group reachable by secret.
func (s *Store) CreateGroup(ctx context.Context, name, secret string) (Group, error) {
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO groups (name, secret_hash, created_at) VALUES (?, ?, ?)",
		name, hashSecret(secret), s.timestamp())
	if err != nil {
		return Group{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Group{}, err
	}
	return Group{ID: id, Name: name}, nil
}

// timestamp is the clock's current time as stored in *_at columns.
func (s *Store) timestamp() string { return s.now().UTC().Format(time.RFC3339) }

// mustAffect turns "no row changed" into ErrNotFound.
func mustAffect(res sql.Result) error {
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

// GroupSummary is a group as listed to the operator.
type GroupSummary struct {
	Group
	Members   int
	CreatedAt string
}

// Groups lists every group with its member count, oldest first.
func (s *Store) Groups(ctx context.Context) ([]GroupSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.id, g.name, g.created_at, count(m.id)
		FROM groups g LEFT JOIN members m ON m.group_id = g.id
		GROUP BY g.id ORDER BY g.id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var gs []GroupSummary
	for rows.Next() {
		var g GroupSummary
		if err := rows.Scan(&g.ID, &g.Name, &g.CreatedAt, &g.Members); err != nil {
			return nil, err
		}
		gs = append(gs, g)
	}
	return gs, rows.Err()
}

// RotateSecret replaces the group's secret, which breaks the old link; ErrNotFound if no such group.
func (s *Store) RotateSecret(ctx context.Context, groupID int64, secret string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE groups SET secret_hash = ? WHERE id = ?", hashSecret(secret), groupID)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

// GroupBySecret returns the group whose secret is secret, or ErrNotFound.
func (s *Store) GroupBySecret(ctx context.Context, secret string) (Group, error) {
	g := Group{}
	err := s.db.QueryRowContext(ctx,
		"SELECT id, name FROM groups WHERE secret_hash = ?", hashSecret(secret)).Scan(&g.ID, &g.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return Group{}, ErrNotFound
	}
	return g, err
}
