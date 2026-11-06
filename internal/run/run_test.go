package run

import (
	"runtime"
	"testing"
)

func TestArgvShape(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		flags      []string
		positional int
		want       string
	}{
		{"flags are sorted", "scribo upsert", []string{"table", "db", "null"}, 2, `{"command":"scribo upsert","flags":["db","null","table"],"args":2}`},
		{"repeated flags appear once", "scribo upsert", []string{"null", "db", "null", "null"}, 0, `{"command":"scribo upsert","flags":["db","null"],"args":0}`},
		{"zero flags", "scribo log", nil, 1, `{"command":"scribo log","flags":[],"args":1}`},
		{"empty flag list", "scribo log", []string{}, 1, `{"command":"scribo log","flags":[],"args":1}`},
		{"zero args", "scribo", []string{"help"}, 0, `{"command":"scribo","flags":["help"],"args":0}`},
		{"html is not escaped", "scribo x", []string{"a<b&c"}, 0, `{"command":"scribo x","flags":["a<b&c"],"args":0}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ArgvShape(tt.path, tt.flags, tt.positional); got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestArgvShapeDoesNotReorderTheCallersSlice(t *testing.T) {
	flags := []string{"b", "a"}
	ArgvShape("scribo x", flags, 0)
	if flags[0] != "b" || flags[1] != "a" {
		t.Errorf("caller's slice was reordered: %v", flags)
	}
}

func TestMeta(t *testing.T) {
	meta := Meta(`{"command":"scribo x","flags":[],"args":0}`, "align")

	if meta.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", meta.GoVersion, runtime.Version())
	}
	if meta.GitSHA == "" {
		t.Error("GitSHA is empty, want a revision or \"unknown\"")
	}
	if meta.ToolVersion == "" {
		t.Error("ToolVersion is empty")
	}
	if meta.Step != "align" || meta.ArgvShape == "" {
		t.Errorf("Step or ArgvShape not carried over: %+v", meta)
	}
}
