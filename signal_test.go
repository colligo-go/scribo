package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func holdWriteLock(t *testing.T, path string) {
	t.Helper()
	handle, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := handle.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.ExecContext(context.Background(), "ROLLBACK")
		conn.Close()
		handle.Close()
	})
}

func TestSignalEndsAWaitingUpsertCleanly(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		signal os.Signal
	}{
		{"SIGINT", os.Interrupt},
		{"SIGTERM", syscall.SIGTERM},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := seeded(t)
			before := snapshot(t, path, "rnaseq_qc", "rnaseq_qc_history", "tool_runs")
			holdWriteLock(t, path)

			command := exec.Command(binaryPath, "upsert", "--db", path, "--table", "rnaseq_qc",
				"--key", "sample=S1", "--key", "replicate=1", "--busy-timeout", "30000", "note=waiting")
			var stdout, stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()

			time.Sleep(300 * time.Millisecond)
			signalled := time.Now()
			if err := command.Process.Signal(tt.signal); err != nil {
				t.Fatal(err)
			}

			select {
			case err := <-done:
				exitCode := command.ProcessState.ExitCode()
				if exitCode != 5 {
					t.Errorf("exit %d (%v), stderr %q, want 5", exitCode, err, stderr.String())
				}
			case <-time.After(3 * time.Second):
				command.Process.Kill()
				<-done
				t.Fatalf("still running 3s after the signal, stderr %q", stderr.String())
			}
			if elapsed := time.Since(signalled); elapsed > 3*time.Second {
				t.Errorf("took %v to end", elapsed)
			}
			if !strings.Contains(stderr.String(), "ERROR [interrupted]") {
				t.Errorf("stderr = %q, want ERROR [interrupted]", stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}

			if after := snapshot(t, path, "rnaseq_qc", "rnaseq_qc_history", "tool_runs"); after != before {
				t.Errorf("state changed:\nbefore %s\nafter  %s", before, after)
			}
		})
	}
}
