package main

import (
	"context"

	"concerts/internal/store"
)

// demo fills a fresh database with a group for `make demo` and scripts/verify-ui.sh.
func demo(ctx context.Context, st *store.Store, baseURL string) error {
	g, err := newGroup(ctx, st, baseURL, "Les copains du concert")
	if err != nil {
		return err
	}
	for _, name := range []string{"Léa", "Marc", "Sam"} {
		if _, err := st.CreateMember(ctx, g.ID, name); err != nil {
			return err
		}
	}
	return nil
}
