// Command concerts runs the concerts web app.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"concerts/internal/store"
	"concerts/internal/web"
)

const usage = `usage:
  concerts serve
  concerts new-group "<name>"
  concerts backup <file>      write a consistent snapshot of the database; <file> must not exist

environment:
  CONCERTS_ADDR      listen address (default :8080)
  CONCERTS_DB        SQLite database path (default concerts.db)
  CONCERTS_BASE_URL  public base URL, required by new-group (e.g. https://concerts.example.com)
`

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(os.Args[1:], log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func run(args []string, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dbPath := env("CONCERTS_DB", "concerts.db")

	switch {
	case len(args) == 1 && args[0] == "serve":
		return serve(ctx, log, env("CONCERTS_ADDR", ":8080"), dbPath)
	case len(args) == 2 && args[0] == "new-group":
		return newGroup(ctx, dbPath, os.Getenv("CONCERTS_BASE_URL"), args[1])
	case len(args) == 2 && args[0] == "backup":
		return backup(ctx, dbPath, args[1])
	default:
		fmt.Fprint(os.Stderr, usage)
		return errors.New("invalid arguments")
	}
}

func newGroup(ctx context.Context, dbPath, baseURL, name string) (err error) {
	if baseURL == "" {
		return errors.New("CONCERTS_BASE_URL is required")
	}
	st, err := store.Open(ctx, dbPath, time.Now)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, st.Close()) }()
	secret := web.NewSecret()
	if _, err := st.CreateGroup(ctx, name, secret); err != nil {
		return err
	}
	fmt.Println(web.GroupURL(baseURL, secret))
	return nil
}

func backup(ctx context.Context, dbPath, dst string) (err error) {
	st, err := store.Open(ctx, dbPath, time.Now)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, st.Close()) }()
	return st.Backup(ctx, dst)
}

func serve(ctx context.Context, log *slog.Logger, addr, dbPath string) (err error) {
	st, err := store.Open(ctx, dbPath, time.Now)
	if err != nil {
		return err
	}
	// Runs after Shutdown, so in-flight requests finish before the DB closes.
	defer func() { err = errors.Join(err, st.Close()) }()
	h, err := web.New(st, log)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("listening", "addr", addr)

	select {
	case err = <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
