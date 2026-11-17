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
	"strings"
	"syscall"
	"testing"
)

// TestServeSocketActivation passes a Unix listener as fd 3 to a child serve, as systemd does.
func TestServeSocketActivation(t *testing.T) {
	t.Parallel()
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
	cmd := exec.Command("sh", "-c", `export LISTEN_PID=$$ LISTEN_FDS=1; exec "$0" "$@"`, os.Args[0], "-test.run=^TestServeSocketChild$") //nolint:gosec // G204: re-runs this test binary
	cmd.ExtraFiles = []*os.File{f}                                                                                                       // fd 3
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

func TestServeSocketChild(t *testing.T) {
	if os.Getenv("SERVE_SOCKET_CHILD") == "" {
		t.Skip("run by TestServeSocketActivation")
	}
	if err := run([]string{"serve"}, slog.New(slog.NewTextHandler(os.Stderr, nil))); err != nil {
		t.Fatal(err)
	}
}
