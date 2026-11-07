package sqlite

import (
	"context"
	"fmt"
	"testing"
)

func TestUniqueKeys(t *testing.T) {
	tests := []struct {
		name string
		ddl  []string
		want string
	}{
		{"unique constraint", []string{`CREATE TABLE t (sample TEXT, replicate INTEGER, UNIQUE(sample, replicate))`}, "[[replicate sample]]"},
		{"integer primary key is a rowid alias", []string{`CREATE TABLE t (id INTEGER PRIMARY KEY, sample TEXT, UNIQUE(sample))`}, "[[id] [sample]]"},
		{"lower case integer primary key", []string{`CREATE TABLE t (id integer PRIMARY KEY, sample TEXT)`}, "[[id]]"},
		{"composite primary key without rowid", []string{`CREATE TABLE t (a TEXT, b TEXT, PRIMARY KEY(a, b)) WITHOUT ROWID`}, "[[a b]]"},
		{"integer primary key without rowid is listed once", []string{`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT) WITHOUT ROWID`}, "[[id]]"},
		{"unique index", []string{`CREATE TABLE t (x TEXT)`, `CREATE UNIQUE INDEX t_x ON t(x)`}, "[[x]]"},
		{"partial unique index", []string{`CREATE TABLE t (x TEXT)`, `CREATE UNIQUE INDEX t_x ON t(x) WHERE x IS NOT NULL`}, "[]"},
		{"unique index on an expression", []string{`CREATE TABLE t (x TEXT)`, `CREATE UNIQUE INDEX t_x ON t(lower(x))`}, "[]"},
		{"INT primary key is not a rowid alias but has an index", []string{`CREATE TABLE t (id INT PRIMARY KEY, v TEXT)`}, "[[id]]"},
		{"composite integer primary key is no alias", []string{`CREATE TABLE t (a INTEGER, b INTEGER, PRIMARY KEY(a, b))`}, "[[a b]]"},
		{"plain index is not unique", []string{`CREATE TABLE t (x TEXT)`, `CREATE INDEX t_x ON t(x)`}, "[]"},
		{"nothing", []string{`CREATE TABLE t (x TEXT, y TEXT)`}, "[]"},
		{"same set twice is listed once", []string{`CREATE TABLE t (x TEXT, UNIQUE(x))`, `CREATE UNIQUE INDEX t_x ON t(x)`}, "[[x]]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := newQCBackend(t, tt.ddl[0])
			for _, statement := range tt.ddl[1:] {
				if _, err := backend.db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}

			keys, err := backend.UniqueKeys(context.Background(), "t")

			if err != nil {
				t.Fatal(err)
			}
			if got := fmt.Sprint(keys); got != tt.want {
				t.Errorf("UniqueKeys = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestUniqueKeysUseTheSpellingOfTheTable(t *testing.T) {
	backend := newQCBackend(t, `CREATE TABLE t (Sample TEXT, Replicate INTEGER, UNIQUE(sample, REPLICATE))`)

	keys, err := backend.UniqueKeys(context.Background(), "t")

	if err != nil || fmt.Sprint(keys) != "[[Replicate Sample]]" {
		t.Errorf("UniqueKeys = %v, %v, want [[Replicate Sample]]", keys, err)
	}
}
