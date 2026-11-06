package db

import "testing"

func TestAffinityOf(t *testing.T) {
	tests := []struct {
		declared string
		want     string
	}{
		{"INT", "INTEGER"},
		{"INTEGER", "INTEGER"},
		{"BIGINT", "INTEGER"},
		{"VARCHAR(10)", "TEXT"},
		{"TEXT", "TEXT"},
		{"CLOB", "TEXT"},
		{"REAL", "REAL"},
		{"DOUBLE PRECISION", "REAL"},
		{"FLOAT", "REAL"},
		{"NUMERIC", "NUMERIC"},
		{"DECIMAL(10,2)", "NUMERIC"},
		{"BOOLEAN", "NUMERIC"},
		{"DATETIME", "NUMERIC"},
		{"", "BLOB"},
		{"BLOB", "BLOB"},
		{"ANY", "BLOB"},
		{"integer", "INTEGER"},
		{"CHARINT", "INTEGER"},
		{"POINT", "INTEGER"},
	}
	for _, tt := range tests {
		t.Run(tt.declared, func(t *testing.T) {
			if got := AffinityOf(tt.declared); got != tt.want {
				t.Errorf("AffinityOf(%q) = %s, want %s", tt.declared, got, tt.want)
			}
		})
	}
}
