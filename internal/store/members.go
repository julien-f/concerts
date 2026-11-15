package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// MaxMembers bounds a group's size, so a leaked link cannot flood the picker.
const MaxMembers = 100

var (
	// ErrDuplicate is returned when a first name is already taken in the group.
	ErrDuplicate = errors.New("duplicate")
	// ErrLimit is returned when a group already holds as many rows as allowed.
	ErrLimit = errors.New("limit reached")
)

// Member is a first name within a group.
type Member struct {
	ID   int64
	Name string
}

func nameKey(name string) string { return strings.ToLower(name) }

// Members returns the group's members in alphabetical order.
func (s *Store) Members(ctx context.Context, groupID int64) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name FROM members WHERE group_id = ? ORDER BY name_key", groupID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ms []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.Name); err != nil {
			return nil, err
		}
		ms = append(ms, m)
	}
	return ms, rows.Err()
}

// CreateMember adds name to the group. It returns ErrDuplicate with the existing
// member if the name is taken (case-insensitively), or ErrLimit past MaxMembers.
func (s *Store) CreateMember(ctx context.Context, groupID int64, name string) (Member, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Member{}, err
	}
	defer func() { _ = tx.Rollback() }()

	existing := Member{}
	err = tx.QueryRowContext(ctx, "SELECT id, name FROM members WHERE group_id = ? AND name_key = ?",
		groupID, nameKey(name)).Scan(&existing.ID, &existing.Name)
	if err == nil {
		return existing, ErrDuplicate
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Member{}, err
	}
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM members WHERE group_id = ?", groupID).Scan(&n); err != nil {
		return Member{}, err
	}
	if n >= MaxMembers {
		return Member{}, ErrLimit
	}
	res, err := tx.ExecContext(ctx,
		"INSERT INTO members (group_id, name, name_key, created_at) VALUES (?, ?, ?, ?)",
		groupID, name, nameKey(name), s.timestamp())
	if err != nil {
		return Member{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Member{}, err
	}
	return Member{ID: id, Name: name}, tx.Commit()
}

// Member returns the group's member id, or ErrNotFound.
func (s *Store) Member(ctx context.Context, groupID, id int64) (Member, error) {
	m := Member{ID: id}
	err := s.db.QueryRowContext(ctx,
		"SELECT name FROM members WHERE id = ? AND group_id = ?", id, groupID).Scan(&m.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return Member{}, ErrNotFound
	}
	return m, err
}

// DeleteMember removes the group's member called name (case-insensitively), or returns ErrNotFound.
func (s *Store) DeleteMember(ctx context.Context, groupID int64, name string) error {
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM members WHERE group_id = ? AND name_key = ?", groupID, nameKey(name))
	if err != nil {
		return err
	}
	return mustAffect(res)
}
