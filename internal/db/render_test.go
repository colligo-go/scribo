package db

import (
	"errors"
	"testing"
	"time"

	"github.com/colligo-go/scribo/internal/exitcode"
)

func TestRender(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want Value
	}{
		{"whole float", 3.0, Value{Raw: "3"}},
		{"float", 94.3, Value{Raw: "94.3"}},
		{"large float", 1e21, Value{Raw: "1e+21"}},
		{"negative integer", int64(-7), Value{Raw: "-7"}},
		{"nil", nil, Value{IsNull: true}},
		{"string", "abc", Value{Raw: "abc"}},
		{"empty string", "", Value{}},
		{"bytes", []byte("xyz"), Value{Raw: "xyz"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Render(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("Render(%v) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestRenderUnknownTypeIsInternal(t *testing.T) {
	_, err := Render(time.Now())

	var domain *exitcode.Error
	if !errors.As(err, &domain) || domain.Code != exitcode.CodeInternal {
		t.Errorf("error = %v, want internal", err)
	}
}

func TestCanonical(t *testing.T) {
	tests := []struct {
		declared string
		raw      string
		want     string
	}{
		{"INTEGER", "01", "1"},
		{"REAL", "94.30", "94.3"},
		{"REAL", "5", "5"},
		{"TEXT", "01", "01"},
		{"DATETIME", "2026-11-06 12:00:00", "2026-11-06 12:00:00"},
	}
	for _, tt := range tests {
		t.Run(tt.declared+"/"+tt.raw, func(t *testing.T) {
			got, err := Canonical(column(tt.declared), Value{Raw: tt.raw})
			if err != nil {
				t.Fatal(err)
			}
			if got != (Value{Raw: tt.want}) {
				t.Errorf("Canonical = %+v, want %q", got, tt.want)
			}
		})
	}
}

func TestCanonicalKeepsNullApartFromText(t *testing.T) {
	null, _ := Canonical(column("TEXT"), Value{IsNull: true})
	text, _ := Canonical(column("TEXT"), Value{Raw: "null"})
	if !null.IsNull || text.IsNull || text.Raw != "null" {
		t.Errorf("null = %+v, text = %+v", null, text)
	}
}
