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
	build := exec.Command("go", "build", "-race", "-o", binaryPath, ".")
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

func TestExitCodesAndStreams(t *testing.T) {
	usageHint := "→ run 'scribo --help' for usage\n"
	tests := []struct {
		name       string
		args       []string
		wantExit   int
		wantStderr string
		wantStdout bool
	}{
		{name: "no arguments prints help", args: nil, wantExit: 0, wantStdout: true},
		{name: "help flag", args: []string{"--help"}, wantExit: 0, wantStdout: true},
		{
			name:       "unknown flag",
			args:       []string{"--no-such-flag"},
			wantExit:   1,
			wantStderr: "ERROR [usage]: unknown flag: --no-such-flag\n" + usageHint,
		},
		{
			name:       "unknown command",
			args:       []string{"bogus"},
			wantExit:   1,
			wantStderr: "ERROR [usage]: unknown command \"bogus\" for \"scribo\"\n" + usageHint,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runScribo(t, tt.args...)
			if got.ExitCode != tt.wantExit {
				t.Errorf("exit code = %d, want %d", got.ExitCode, tt.wantExit)
			}
			if got.Stderr != tt.wantStderr {
				t.Errorf("stderr = %q, want %q", got.Stderr, tt.wantStderr)
			}
			if tt.wantStdout && !strings.Contains(got.Stdout, "scribo") {
				t.Errorf("stdout = %q, want the help text", got.Stdout)
			}
			if !tt.wantStdout && got.Stdout != "" {
				t.Errorf("stdout = %q, want empty", got.Stdout)
			}
		})
	}
}
