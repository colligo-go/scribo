package schema

import (
	"bytes"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/colligo-go/scribo/internal/db/sqlite"
	"github.com/colligo-go/scribo/internal/exitcode"
)

func FuzzSchemaTSV(f *testing.F) {
	for _, seed := range []string{
		"sample\tvalue\nS1\t1\n", "sample\tvalue\r\nS1\t1.5\r\n", "", "sample\n", "sample\t\tvalue\nS1\t1\t2\n",
		"sample\tValue\tvalue\nS1\t1\t2\n", "sample\tvalue\nS1\t1\t2\n", "sam\"ple\tva]lue\nS1\tx\n", "sample\tn\nS1\t-0\nS2\t1e300\n",
	} {
		f.Add([]byte(seed))
	}
	handle, err := sql.Open("sqlite", "file:fuzzed?mode=memory")
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { handle.Close() })
	handle.SetMaxOpenConns(1)
	f.Fuzz(func(t *testing.T, content []byte) {
		header, _, _ := strings.Cut(string(content), "\n")
		key, _, _ := strings.Cut(strings.TrimSuffix(header, "\r"), "\t")
		definition, err := FromTSV("fuzzed", bytes.NewReader(content), []string{key})
		if err != nil {
			var domain *exitcode.Error
			if !errors.As(err, &domain) {
				t.Fatalf("error %v is not an *exitcode.Error", err)
			}
			return
		}
		ddl := sqlite.CreateTable(definition)
		if _, err := handle.Exec(ddl); err != nil {
			t.Fatalf("accepted DDL does not run: %v\n%s", err, ddl)
		}
		if _, err := handle.Exec(`DROP TABLE "fuzzed"`); err != nil {
			t.Fatal(err)
		}
	})
}
