package db

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenEnablesWALMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	runSQLite(t, path, `PRAGMA journal_mode = DELETE;`)

	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	if mode := strings.ToLower(runSQLite(t, path, `PRAGMA journal_mode;`)); mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
}

func TestCreateRunWaitsForLockedWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	locker := exec.Command("sqlite3", path)
	lockerIn, err := locker.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	lockerOut, err := locker.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	locker.Stderr = &stderr
	if err := locker.Start(); err != nil {
		t.Fatal(err)
	}

	startedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := io.WriteString(lockerIn, strings.Join([]string{
		".bail on",
		"BEGIN IMMEDIATE;",
		fmt.Sprintf(
			"INSERT INTO runs(id, pipeline_name, status, started_at, ended_at) VALUES ('locker', 'demo', 'running', '%s', NULL);",
			startedAt,
		),
		"SELECT 'ready';",
		"",
	}, "\n")); err != nil {
		t.Fatal(err)
	}

	scanner := bufio.NewScanner(lockerOut)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("locker process exited before reporting readiness: %s", stderr.String())
	}
	if scanner.Text() != "ready" {
		t.Fatalf("locker readiness output = %q", scanner.Text())
	}

	releaseDone := make(chan struct{})
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, _ = io.WriteString(lockerIn, "COMMIT;\n.quit\n")
		_ = lockerIn.Close()
		close(releaseDone)
	}()

	start := time.Now()
	err = database.CreateRun(RunRecord{
		ID:        "contender",
		Pipeline:  "demo",
		Status:    "running",
		StartedAt: time.Now().UTC(),
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("CreateRun returned error after %s: %v", elapsed, err)
	}
	if elapsed < 250*time.Millisecond {
		t.Fatalf("CreateRun returned too quickly under lock: %s", elapsed)
	}

	<-releaseDone
	if err := locker.Wait(); err != nil {
		t.Fatalf("locker sqlite3 process failed: %v: %s", err, stderr.String())
	}
}

func runSQLite(t *testing.T, path, sql string) string {
	t.Helper()
	cmd := exec.Command("sqlite3", path, sql)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sqlite3 %q failed: %v: %s", sql, err, output)
	}
	return strings.TrimSpace(string(output))
}
