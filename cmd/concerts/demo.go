package main

import (
	"context"
	"fmt"

	"concerts/internal/store"
	"concerts/internal/web"
)

// demo fills a fresh database with a group for `make demo` and scripts/verify-ui.sh.
func demo(ctx context.Context, st *store.Store, baseURL string) error {
	secret := web.NewSecret()
	g, err := st.CreateGroup(ctx, "Les copains du concert", secret)
	if err != nil {
		return err
	}
	for _, name := range []string{"Léa", "Marc", "Sam"} {
		if _, err := st.CreateMember(ctx, g.ID, name); err != nil {
			return err
		}
	}
	fmt.Println(web.GroupURL(baseURL, secret))
	return nil
}
