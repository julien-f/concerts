package store

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
)

// TestBackupFailureLeavesNoFile runs a backup in a child process whose file size limit
// (RLIMIT_FSIZE) makes VACUUM INTO fail half way, as a full disk would.
func TestBackupFailureLeavesNoFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestBackupFailureChild$") //nolint:gosec // G204: re-runs this test binary
	// No race-detector pause at exit (1 s by default), which would only lengthen make check.
	cmd.Env = append(os.Environ(), "BACKUP_FAILURE_DIR="+dir, "GORACE=atexit_sleep_ms=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "backup.db")); !os.IsNotExist(err) {
		t.Fatalf("failed backup left a file (stat err = %v); a restore could pick it\n%s", err, out)
	}
}

func TestBackupFailureChild(t *testing.T) {
	dir := os.Getenv("BACKUP_FAILURE_DIR")
	if dir == "" {
		t.Skip("run by TestBackupFailureLeavesNoFile")
	}
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(dir, "live.db"), fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE pad (x BLOB);
		WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 200)
		INSERT INTO pad SELECT randomblob(1000) FROM n`); err != nil {
		t.Fatal(err)
	}
	signal.Ignore(syscall.SIGXFSZ) // the write fails with EFBIG instead of killing the process
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 64 << 10, Max: 64 << 10}); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup(ctx, filepath.Join(dir, "backup.db")); err == nil {
		t.Fatal("backup past the file size limit: want an error")
	}
}
