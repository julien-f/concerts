package store

import "context"

// Statuses a member can give a concert, in the order buttons show them.
const (
	Interested = "interested"
	Going      = "going"
	Ticket     = "ticket"
)

// Status is one member's answer to one concert.
type Status struct {
	ConcertID int64
	Member    Member
	Status    string
}

// Statuses returns the answers to the group's live concerts (to one concert if
// concertID is not 0), members in name order.
func (s *Store) Statuses(ctx context.Context, groupID, concertID int64) ([]Status, error) {
	return queryAll(ctx, s.db, func(st *Status) []any {
		return []any{&st.ConcertID, &st.Member.ID, &st.Member.Name, &st.Status}
	}, `
		SELECT s.concert_id, m.id, m.name, s.status
		FROM statuses s
		JOIN concerts c ON c.id = s.concert_id
		JOIN members m ON m.id = s.member_id
		WHERE c.group_id = ?1 AND c.deleted_at IS NULL AND (?2 = 0 OR c.id = ?2)
		ORDER BY m.name_key`, groupID, concertID)
}

// SetStatus records memberID's answer to the group's live concert; "" clears it.
// It returns ErrNotFound unless both the concert and the member belong to the group.
func (s *Store) SetStatus(ctx context.Context, groupID, concertID, memberID int64, status string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM concerts c, members m
		WHERE c.id = ? AND c.group_id = ? AND c.deleted_at IS NULL AND m.id = ? AND m.group_id = ?`,
		concertID, groupID, memberID, groupID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	if status == "" {
		_, err = tx.ExecContext(ctx, "DELETE FROM statuses WHERE concert_id = ? AND member_id = ?", concertID, memberID)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO statuses (concert_id, member_id, status) VALUES (?, ?, ?)
			ON CONFLICT DO UPDATE SET status = excluded.status`, concertID, memberID, status)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
