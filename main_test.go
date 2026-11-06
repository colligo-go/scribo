package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binaryPath string

type result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	dir, err := os.MkdirTemp("", "scribo-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)

	binaryPath = filepath.Join(dir, "scribo")
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	if output, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s", err, output)
		return 1
	}
	return m.Run()
}

func runScribo(t *testing.T, args ...string) result {
	t.Helper()
	command := exec.Command(binaryPath, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()

	exitCode := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running scribo: %v", err)
	}
	return result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode}
}

func TestUnknownFlagIsUsageError(t *testing.T) {
	got := runScribo(t, "--no-such-flag")

	if got.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1", got.ExitCode)
	}
	if !strings.Contains(got.Stderr, "ERROR [usage]") {
		t.Errorf("stderr = %q, want it to contain %q", got.Stderr, "ERROR [usage]")
	}
	if got.Stdout != "" {
		t.Errorf("stdout = %q, want empty", got.Stdout)
	}
}
