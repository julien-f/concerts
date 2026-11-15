package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"concerts/internal/store"
	"concerts/internal/web"
)

// demo fills a fresh database with a group for `make demo` and scripts/verify-ui.sh.
func demo(ctx context.Context, dbPath, baseURL string) (err error) {
	if baseURL == "" {
		return errors.New("CONCERTS_BASE_URL is required")
	}
	st, err := store.Open(ctx, dbPath, time.Now)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, st.Close()) }()
	secret := web.NewSecret()
	if _, err := st.CreateGroup(ctx, "Les copains du concert", secret); err != nil {
		return err
	}
	fmt.Println(web.GroupURL(baseURL, secret))
	return nil
}
