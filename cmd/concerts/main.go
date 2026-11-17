// Command concerts runs the concerts web app.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"text/tabwriter"
	"time"

	"concerts/internal/store"
	"concerts/internal/web"
)

const usage = `usage:
  concerts serve
  concerts new-group "<name>"                    print the new group's link
  concerts list-groups
  concerts rotate-secret <group-id>              replace the link (the old one stops working) and print it
  concerts delete-member <group-id> <first-name>
  concerts rename-member <group-id> <first-name> <new-first-name>   keeps their answers and concerts
  concerts backup <file>                         write a consistent snapshot of the database; <file> must not exist
  concerts demo                                  add a demo group (development)

environment:
  CONCERTS_ADDR      listen address (default 127.0.0.1:8080); serve uses the socket
                     systemd passes instead, when started by a .socket unit
  CONCERTS_DB        SQLite database path (default concerts.db)
  CONCERTS_BASE_URL  public base URL, e.g. https://concerts.example.com
                     (required by serve, new-group, rotate-secret and demo)
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
	baseURL := os.Getenv("CONCERTS_BASE_URL")

	var cmd func(*store.Store) error
	needsURL := false
	switch {
	case len(args) == 1 && args[0] == "serve":
		needsURL = true
		cmd = func(st *store.Store) error {
			return serve(ctx, log, st, env("CONCERTS_ADDR", "127.0.0.1:8080"), baseURL)
		}
	case len(args) == 2 && args[0] == "new-group":
		needsURL = true
		cmd = func(st *store.Store) error { _, err := newGroup(ctx, st, baseURL, args[1]); return err }
	case len(args) == 1 && args[0] == "list-groups":
		cmd = func(st *store.Store) error { return listGroups(ctx, st) }
	case len(args) == 2 && args[0] == "rotate-secret":
		needsURL = true
		cmd = func(st *store.Store) error { return rotateSecret(ctx, st, baseURL, args[1]) }
	case len(args) == 3 && args[0] == "delete-member":
		cmd = func(st *store.Store) error { return deleteMember(ctx, st, args[1], args[2]) }
	case len(args) == 4 && args[0] == "rename-member":
		cmd = func(st *store.Store) error { return renameMember(ctx, st, args[1], args[2], args[3]) }
	case len(args) == 2 && args[0] == "backup":
		cmd = func(st *store.Store) error {
			if err := st.Backup(ctx, args[1]); err != nil {
				return fmt.Errorf("backup %s: %w", args[1], err)
			}
			return nil
		}
	case len(args) == 1 && args[0] == "demo":
		needsURL = true
		cmd = func(st *store.Store) error { return demo(ctx, st, baseURL, time.Now()) }
	default:
		fmt.Fprint(os.Stderr, usage)
		return errors.New("invalid arguments")
	}
	if needsURL {
		if _, err := web.ParseBaseURL(baseURL); err != nil {
			return fmt.Errorf("CONCERTS_BASE_URL: %w", err)
		}
	}

	st, err := store.Open(ctx, dbPath, time.Now)
	if err != nil {
		return err
	}
	// Runs after serve's Shutdown, so in-flight requests finish before the DB closes.
	return errors.Join(cmd(st), st.Close())
}

// newGroup creates a group and prints its link.
func newGroup(ctx context.Context, st *store.Store, baseURL, name string) (store.Group, error) {
	clean, ok := web.CleanGroupName(name)
	if !ok {
		return store.Group{}, fmt.Errorf("group name %q: empty, too long or with invisible characters", name)
	}
	secret := web.NewSecret()
	g, err := st.CreateGroup(ctx, clean, secret)
	if err == nil {
		fmt.Println(web.GroupURL(baseURL, secret))
	}
	return g, err
}

func listGroups(ctx context.Context, st *store.Store) error {
	gs, err := st.Groups(ctx)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	// tabwriter buffers: write errors surface at Flush.
	_, _ = fmt.Fprintln(tw, "ID\tNAME\tMEMBERS\tCREATED")
	for _, g := range gs {
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%d\t%s\n", g.ID, g.Name, g.Members, g.CreatedAt)
	}
	return tw.Flush()
}

func parseGroupID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("group id %q: not a number (see list-groups)", s)
	}
	return id, nil
}

func rotateSecret(ctx context.Context, st *store.Store, baseURL, groupID string) error {
	id, err := parseGroupID(groupID)
	if err != nil {
		return err
	}
	secret := web.NewSecret()
	if err := st.RotateSecret(ctx, id, secret); err != nil {
		return fmt.Errorf("group %d: %w", id, err)
	}
	fmt.Println(web.GroupURL(baseURL, secret))
	fmt.Fprintln(os.Stderr, "The old link no longer works; members pick their first name again from the list.")
	return nil
}

func deleteMember(ctx context.Context, st *store.Store, groupID, name string) error {
	id, err := parseGroupID(groupID)
	if err != nil {
		return err
	}
	if err := st.DeleteMember(ctx, id, name); err != nil {
		return fmt.Errorf("member %q of group %d: %w", name, id, err)
	}
	return nil
}

func renameMember(ctx context.Context, st *store.Store, groupID, name, newName string) error {
	id, err := parseGroupID(groupID)
	if err != nil {
		return err
	}
	clean, ok := web.CleanName(newName)
	if !ok {
		return fmt.Errorf("new first name %q: the picker would refuse it (length, first character, invisible characters)", newName)
	}
	if err := st.RenameMember(ctx, id, name, clean); err != nil {
		return fmt.Errorf("member %q of group %d: %w", name, id, err)
	}
	return nil
}

// listen returns the socket systemd passes (fd 3, see sd_listen_fds(3)), else a TCP listener on addr.
func listen(addr string) (net.Listener, error) {
	// PID 1 is the process systemd started: systemd 257 with PrivatePIDs=yes sets LISTEN_PID to a host PID.
	// Remove once the server runs systemd 259+ (https://github.com/systemd/systemd/pull/39575).
	pid := os.Getenv("LISTEN_PID")
	if pid == "" || pid != strconv.Itoa(os.Getpid()) && os.Getpid() != 1 {
		return net.Listen("tcp", addr)
	}
	// An error rather than TCP: a service looking healthy but unreachable through the proxy hides the bug.
	if n := os.Getenv("LISTEN_FDS"); n != "1" {
		return nil, fmt.Errorf("LISTEN_FDS=%q: want exactly one socket", n)
	}
	f := os.NewFile(3, "systemd socket")
	defer func() { _ = f.Close() }() // FileListener holds its own close-on-exec copy
	return net.FileListener(f)
}

func serve(ctx context.Context, log *slog.Logger, st *store.Store, addr, baseURL string) error {
	h, err := web.New(st, log, web.Config{BaseURL: baseURL, Now: time.Now})
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
	ln, err := listen(addr)
	if err != nil {
		return err
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	// The actual address, so a ":0" listener (scripts/verify-ui.sh) can be found.
	log.Info("listening", "addr", ln.Addr().String())

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
