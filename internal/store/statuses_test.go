package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestStatuses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := openTest(t)
	g, _ := s.CreateGroup(ctx, "g", "s1")
	other, _ := s.CreateGroup(ctx, "other", "s2")
	zoe, _ := s.CreateMember(ctx, g.ID, "Zoé")
	lea, _ := s.CreateMember(ctx, g.ID, "Léa")
	intrus, _ := s.CreateMember(ctx, other.ID, "Intrus")
	c1, _ := s.CreateConcert(ctx, g.ID, lea.ID, Concert{Artist: "A", Date: "2027-01-01", Venue: "V"}, testToday)
	c2, _ := s.CreateConcert(ctx, g.ID, lea.ID, Concert{Artist: "B", Date: "2027-01-02", Venue: "V"}, testToday)

	for _, st := range []struct {
		concert, member int64
		status          string
	}{{c1, zoe.ID, Going}, {c1, lea.ID, Interested}, {c1, lea.ID, Going}, {c1, lea.ID, Going}, {c2, zoe.ID, Going}, {c2, zoe.ID, ""}} {
		if err := s.SetStatus(ctx, g.ID, st.concert, st.member, st.status); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Statuses(ctx, g.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Upserts keep one row per member; "" clears; names in order.
	want := fmt.Sprint([]Status{{c1, lea, Going}, {c1, zoe, Going}})
	if fmt.Sprint(got) != want {
		t.Fatalf("Statuses = %v, want %v", got, want)
	}
	if one, _ := s.Statuses(ctx, g.ID, c2); len(one) != 0 {
		t.Fatalf("Statuses of c2 = %v, want none", one)
	}
	if one, _ := s.Statuses(ctx, g.ID, c1); fmt.Sprint(one) != want {
		t.Fatalf("Statuses of c1 = %v, want %v", one, want)
	}

	for name, err := range map[string]error{
		"member of another group":  s.SetStatus(ctx, g.ID, c1, intrus.ID, Going),
		"concert of another group": s.SetStatus(ctx, other.ID, c1, intrus.ID, Going),
		"unknown concert":          s.SetStatus(ctx, g.ID, 99, lea.ID, Going),
	} {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", name, err)
		}
	}

	// A deleted concert takes no answers and hides its statuses, which come back on restore.
	if err := s.DeleteConcert(ctx, g.ID, c1); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(ctx, g.ID, c1, lea.ID, Going); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted concert: err = %v, want ErrNotFound", err)
	}
	if got, _ := s.Statuses(ctx, g.ID, 0); len(got) != 0 {
		t.Errorf("statuses of a deleted concert listed: %v", got)
	}
	if err := s.RestoreConcert(ctx, g.ID, c1, testToday); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Statuses(ctx, g.ID, 0); len(got) != 2 {
		t.Errorf("after restore: %v", got)
	}

	// Deleting a member removes their answers.
	if err := s.DeleteMember(ctx, g.ID, "Zoé"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Statuses(ctx, g.ID, 0); fmt.Sprint(got) != fmt.Sprint([]Status{{c1, lea, Going}}) {
		t.Errorf("after deleting Zoé: %v", got)
	}
}
