package cmd

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/colligo-go/scribo/internal/exitcode"
	"github.com/spf13/cobra"
)

func rootWithChildren() *cobra.Command {
	root := newRootCommand()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.AddCommand(
		&cobra.Command{
			Use:  "ok",
			Args: cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error { return nil },
		},
		&cobra.Command{
			Use:  "untyped",
			RunE: func(*cobra.Command, []string) error { return errors.New("disk on fire") },
		},
		&cobra.Command{
			Use: "wrapped",
			RunE: func(*cobra.Command, []string) error {
				return fmt.Errorf("opening: %w", exitcode.New(exitcode.Data, exitcode.CodeInvalidValue, "not a number", ""))
			},
		},
		&cobra.Command{
			Use: "domain",
			RunE: func(*cobra.Command, []string) error {
				return exitcode.New(exitcode.Data, exitcode.CodeInvalidValue, "not a number", "")
			},
		},
	)
	nested := &cobra.Command{Use: "group"}
	nested.AddCommand(&cobra.Command{
		Use:  "deep",
		RunE: func(*cobra.Command, []string) error { return errors.New("deep failure") },
	})
	root.AddCommand(nested)
	return root
}

func TestExecuteClassifiesEveryErrorSource(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantNil    bool
		wantStatus int
		wantCode   string
	}{
		{name: "success", args: []string{"ok"}, wantNil: true},
		{name: "root without arguments", args: nil, wantNil: true},
		{name: "unknown flag", args: []string{"--no-such-flag"}, wantStatus: exitcode.Usage, wantCode: exitcode.CodeUsage},
		{name: "unknown flag on child", args: []string{"ok", "--no-such-flag"}, wantStatus: exitcode.Usage, wantCode: exitcode.CodeUsage},
		{name: "unknown command", args: []string{"bogus"}, wantStatus: exitcode.Usage, wantCode: exitcode.CodeUsage},
		{name: "extra argument", args: []string{"ok", "extra"}, wantStatus: exitcode.Usage, wantCode: exitcode.CodeUsage},
		{name: "untyped handler error", args: []string{"untyped"}, wantStatus: exitcode.System, wantCode: exitcode.CodeInternal},
		{name: "untyped error in nested command", args: []string{"group", "deep"}, wantStatus: exitcode.System, wantCode: exitcode.CodeInternal},
		{name: "domain error kept", args: []string{"domain"}, wantStatus: exitcode.Data, wantCode: exitcode.CodeInvalidValue},
		{name: "wrapped domain error kept", args: []string{"wrapped"}, wantStatus: exitcode.Data, wantCode: exitcode.CodeInvalidValue},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := execute(rootWithChildren(), tt.args)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("execute(%q) = %+v, want nil", tt.args, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("execute(%q) = nil, want status %d", tt.args, tt.wantStatus)
			}
			if got.Status != tt.wantStatus || got.Code != tt.wantCode {
				t.Errorf("execute(%q) = status %d code %q, want status %d code %q", tt.args, got.Status, got.Code, tt.wantStatus, tt.wantCode)
			}
		})
	}
}

func TestExecuteIsIdempotentOverTheSameTree(t *testing.T) {
	root := rootWithChildren()
	first := execute(root, []string{"untyped"})
	second := execute(root, []string{"untyped"})
	if first == nil || second == nil || first.Code != second.Code || first.Message != second.Message {
		t.Errorf("first = %+v, second = %+v, want the same internal error", first, second)
	}
}
