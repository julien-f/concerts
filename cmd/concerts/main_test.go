package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"concerts/internal/store"
)

// TestServeSocketActivation passes a Unix listener as fd 3 to a child serve, as systemd does.
func TestServeSocketActivation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		listenPID string
		pidNS     bool
	}{
		{"LISTEN_PID matches", "$$", false},
		// systemd 257 with PrivatePIDs=yes sets LISTEN_PID to a host PID while the service is PID 1.
		{"PID 1 in a PID namespace", "999999", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			testServeSocket(t, tc.listenPID, tc.pidNS)
		})
	}
}

func testServeSocket(t *testing.T, listenPID string, pidNS bool) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "s") // sun_path holds 108 bytes: keep the name short
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	f, err := ln.File()
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close()

	// POSIX leaves unspecified whether `VAR=x exec …` exports VAR; export does.
	cmd := exec.Command("sh", "-c", `export LISTEN_PID=`+listenPID+` LISTEN_FDS=1; exec "$0" "$@"`, os.Args[0], "-test.run=^TestServeSocketChild$") //nolint:gosec // G204: re-runs this test binary
	if pidNS {
		// sh, then serve, is PID 1; SIGTERM reaches serve directly.
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Cloneflags:  syscall.CLONE_NEWUSER | syscall.CLONE_NEWPID,
			UidMappings: []syscall.SysProcIDMap{{ContainerID: os.Getuid(), HostID: os.Getuid(), Size: 1}},
			GidMappings: []syscall.SysProcIDMap{{ContainerID: os.Getgid(), HostID: os.Getgid(), Size: 1}},
		}
	}
	cmd.ExtraFiles = []*os.File{f} // fd 3
	cmd.Env = append(os.Environ(), "SERVE_SOCKET_CHILD=1",
		"CONCERTS_DB="+filepath.Join(dir, "c.db"), "CONCERTS_BASE_URL=http://concerts.test",
		"CONCERTS_ADDR=unused:bad",                         // a TCP fallback fails instead of passing
		"GORACE="+os.Getenv("GORACE")+" atexit_sleep_ms=0") // see CLAUDE.md Pitfalls
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close() // the child holds the only copy: if it dies, connect fails instead of hanging
	done := false
	t.Cleanup(func() {
		if !done {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	// No readiness wait: the request sits in the socket's backlog until the child accepts it.
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}
	resp, err := (&http.Client{Transport: tr}).Get("http://concerts/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	tr.CloseIdleConnections()
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "ok" {
		t.Errorf("GET /healthz = %d %q, want 200 ok", resp.StatusCode, body)
	}

	_ = cmd.Process.Signal(syscall.SIGTERM)
	err = cmd.Wait()
	done = true
	if err != nil {
		t.Fatalf("child: %v\n%s", err, &out)
	}
	if _, err := os.Stat(sock); err != nil {
		t.Errorf("socket file gone after shutdown (systemd owns it; requests would fail until the socket unit restarts): %v", err)
	}
	if !strings.Contains(out.String(), "addr="+sock) {
		t.Errorf("child did not log the inherited socket:\n%s", &out)
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC) }
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "c.db"), now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestRenameMember(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openStore(t)
	g, err := st.CreateGroup(ctx, "g", "s")
	if err != nil {
		t.Fatal(err)
	}
	m, err := st.CreateMember(ctx, g.ID, "Lea")
	if err != nil {
		t.Fatal(err)
	}

	if err := renameMember(ctx, st, strconv.FormatInt(g.ID, 10), "lea", "\u200bLéa"); err == nil {
		t.Error("invisible character accepted")
	}
	if err := renameMember(ctx, st, strconv.FormatInt(g.ID, 10), "lea", "  Léa   B. "); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Member(ctx, g.ID, m.ID); got.Name != "Léa B." {
		t.Errorf("name = %q, want the cleaned %q", got.Name, "Léa B.")
	}
}

func TestVenueCommands(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openStore(t)
	g, err := st.CreateGroup(ctx, "g", "s")
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(g.ID, 10)
	if err := followVenue(ctx, st, id, "nowhere"); err == nil {
		t.Error("unknown venue followed")
	}
	if err := followVenue(ctx, st, "999", "mc2"); err == nil {
		t.Error("unknown group followed a venue")
	}
	for _, key := range []string{"mc2", "lilyade", "mc2"} { // following twice changes nothing
		if err := followVenue(ctx, st, id, key); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := listVenues(ctx, st, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"KEY", "mc2  ", "MC2", "La Belle Électrique", "retour-de-scene"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("list-venues lacks %q:\n%s", want, out.String())
		}
	}
	for _, line := range strings.Split(out.String(), "\n") {
		following := strings.HasSuffix(line, " "+id)
		if (strings.HasPrefix(line, "mc2 ") || strings.HasPrefix(line, "lilyade ")) != following {
			t.Errorf("list-venues line %q: want group %s on mc2 and lilyade only", line, id)
		}
	}
	if err := unfollowVenue(ctx, st, id, "mc2"); err != nil {
		t.Fatal(err)
	}
	if err := unfollowVenue(ctx, st, id, "mc2"); err == nil {
		t.Error("unfollowing a venue not followed succeeded")
	}
	if got, err := st.Followed(ctx, g.ID); err != nil || !slices.Equal(got, []string{"lilyade"}) {
		t.Errorf("followed = %v, %v; want lilyade", got, err)
	}
}

// TestUsage: every command run accepts is in the usage text, and wrong arguments print it.
func TestUsage(t *testing.T) {
	t.Parallel()
	for _, cmd := range []string{"serve", "new-group", "list-groups", "rotate-secret", "delete-member", "rename-member",
		"backup", "list-venues", "follow-venue", "unfollow-venue", "sync", "demo"} {
		if !strings.Contains(usage, "  concerts "+cmd+" ") && !strings.Contains(usage, "  concerts "+cmd+"\n") {
			t.Errorf("usage lacks %s", cmd)
		}
	}
	for _, args := range [][]string{nil, {"follow-venue", "1"}, {"sync", "now"}, {"unfollow-venue"}} {
		if err := run(args, slog.New(slog.DiscardHandler)); err == nil || err.Error() != "invalid arguments" {
			t.Errorf("run(%q) = %v, want invalid arguments", args, err)
		}
	}
}

func TestNewGroupCleansName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openStore(t)
	for _, name := range []string{"  ", "a\u200bb"} {
		if _, err := newGroup(ctx, st, "http://x", name); err == nil {
			t.Errorf("newGroup(%q) accepted", name)
		}
	}
	g, err := newGroup(ctx, st, "http://x", " Les\tcopains  du concert ")
	if err != nil || g.Name != "Les copains du concert" {
		t.Errorf("newGroup = %q, %v; want the cleaned name", g.Name, err)
	}
}

// TestRunChecksBaseURL: a command printing a link refuses a base URL serve would refuse,
// before rotate-secret kills the old link. Not parallel: t.Setenv.
func TestRunChecksBaseURL(t *testing.T) {
	t.Setenv("CONCERTS_DB", filepath.Join(t.TempDir(), "c.db"))
	for _, u := range []string{"", "x", "ftp://h", "https://h/?x"} {
		t.Setenv("CONCERTS_BASE_URL", u)
		if err := run([]string{"rotate-secret", "1"}, slog.New(slog.DiscardHandler)); err == nil || !strings.Contains(err.Error(), "CONCERTS_BASE_URL") {
			t.Errorf("base URL %q: err = %v, want one naming CONCERTS_BASE_URL", u, err)
		}
	}
}

// TestDemo: make demo and verify-ui depend on it; it must keep up with the store.
func TestDemo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := openStore(t)
	if err := demo(ctx, st, "http://x", time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	gs, err := st.Groups(ctx)
	if err != nil || len(gs) != 1 {
		t.Fatalf("groups = %+v, %v; want the demo group", gs, err)
	}
	cs, err := st.Concerts(ctx, gs[0].ID)
	if err != nil || len(cs) != 34 {
		t.Errorf("concerts = %d, %v; want 34", len(cs), err)
	}
}

func TestServeSocketChild(t *testing.T) {
	if os.Getenv("SERVE_SOCKET_CHILD") == "" {
		t.Skip("run by TestServeSocketActivation")
	}
	if err := run([]string{"serve"}, slog.New(slog.NewTextHandler(os.Stderr, nil))); err != nil {
		t.Fatal(err)
	}
}
