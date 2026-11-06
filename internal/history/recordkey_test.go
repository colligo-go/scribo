package history

import (
	"encoding/json"
	"errors"
	"math/rand"
	"reflect"
	"testing"
	"unicode/utf8"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
)

func text(raw string) db.Value { return db.Value{Raw: raw} }

var null = db.Value{IsNull: true}

func TestRecordKeyCanonicalForm(t *testing.T) {
	tests := []struct {
		name string
		key  db.Key
		want string
	}{
		{"spec example", db.Key{"replicate": text("1"), "accession": text("ENCFF001")}, `[["accession","ENCFF001"],["replicate","1"]]`},
		{"single field", db.Key{"sample": text("S1")}, `[["sample","S1"]]`},
		{"null is JSON null", db.Key{"sample": null}, `[["sample",null]]`},
		{"text null is a string", db.Key{"sample": text("null")}, `[["sample","null"]]`},
		{"empty text is empty string", db.Key{"sample": text("")}, `[["sample",""]]`},
		{"html is not escaped", db.Key{"a": text("<>&")}, `[["a","<>&"]]`},
		{"unicode outside the BMP", db.Key{"a": text("𝄞")}, `[["a","𝄞"]]`},
		{"names sorted bytewise", db.Key{"b": text("2"), "B": text("1"), "a": text("3")}, `[["B","1"],["a","3"],["b","2"]]`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := RecordKey(test.key)
			if err != nil {
				t.Fatalf("RecordKey: %v", err)
			}
			if got != test.want {
				t.Errorf("got %s, want %s", got, test.want)
			}
		})
	}
}

func TestRecordKeyIndependentOfInsertionOrder(t *testing.T) {
	names := []string{"delta", "alpha", "charlie", "bravo", "echo"}
	want := ""
	for round := 0; round < 50; round++ {
		key := db.Key{}
		for _, i := range rand.Perm(len(names)) {
			key[names[i]] = text(names[i] + "-value")
		}
		got, err := RecordKey(key)
		if err != nil {
			t.Fatal(err)
		}
		if want == "" {
			want = got
		}
		if got != want {
			t.Fatalf("round %d: got %s, want %s", round, got, want)
		}
	}
}

func TestRecordKeyDifficultValuesDoNotCollide(t *testing.T) {
	keys := []db.Key{
		{"a": text("x,y")},
		{"a": text("x"), "y": text("")},
		{"a": text("x=y")},
		{"a": text("x"), "y": text("y")},
		{"a": text(`x"y`)},
		{"a": text(`x\"y`)},
		{"a": text("[x]")},
		{"a": text(`["x"]`)},
		{"a": text(`\`)},
		{"a": text(`\\`)},
		{"a": text("")},
		{"a": text("null")},
		{"a": null},
		{"a": text("1,b=2"), "b": text("")},
		{"a": text("1"), "b": text("2")},
		{"a": text("<>&")},
		{"a": text("<>" + "\\" + "u0026")},
	}
	seen := map[string]int{}
	for i, key := range keys {
		got, err := RecordKey(key)
		if err != nil {
			t.Fatalf("key %d: %v", i, err)
		}
		if j, ok := seen[got]; ok {
			t.Errorf("keys %d and %d both give %s", j, i, got)
		}
		seen[got] = i
	}
}

func TestRecordKeyEmptyTextTypedNullAndNullAreDistinct(t *testing.T) {
	keys := []string{}
	for _, value := range []db.Value{text(""), text("null"), null} {
		got, err := RecordKey(db.Key{"a": value})
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, got)
	}
	if keys[0] == keys[1] || keys[1] == keys[2] || keys[0] == keys[2] {
		t.Errorf("keys not distinct: %v", keys)
	}
}

func TestRecordKeyRefusals(t *testing.T) {
	tests := []struct {
		name       string
		key        db.Key
		wantStatus int
		wantCode   string
	}{
		{"invalid UTF-8 value", db.Key{"a": text("\xff")}, exitcode.Data, exitcode.CodeInvalidValue},
		{"invalid UTF-8 name", db.Key{"\xff": text("x")}, exitcode.Data, exitcode.CodeInvalidValue},
		{"truncated sequence", db.Key{"a": text("\xe2\x82")}, exitcode.Data, exitcode.CodeInvalidValue},
		{"encoded surrogate", db.Key{"a": text("\xed\xa0\x80")}, exitcode.Data, exitcode.CodeInvalidValue},
		{"empty key", db.Key{}, exitcode.Usage, exitcode.CodeUsage},
		{"nil key", nil, exitcode.Usage, exitcode.CodeUsage},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := RecordKey(test.key)
			if err == nil {
				t.Fatalf("got %q, want an error", got)
			}
			var domain *exitcode.Error
			if !errors.As(err, &domain) {
				t.Fatalf("error %v is not an *exitcode.Error", err)
			}
			if domain.Status != test.wantStatus || domain.Code != test.wantCode {
				t.Errorf("got status %d code %q, want %d %q", domain.Status, domain.Code, test.wantStatus, test.wantCode)
			}
		})
	}
}

func TestRecordKeyNullIgnoresRaw(t *testing.T) {
	got, err := RecordKey(db.Key{"a": db.Value{Raw: "\xff", IsNull: true}})
	if err != nil {
		t.Fatalf("RecordKey: %v", err)
	}
	if got != `[["a",null]]` {
		t.Errorf("got %s", got)
	}
}

func TestRecordKeyDistinctRandomKeysGiveDistinctStrings(t *testing.T) {
	alphabet := []string{"", "a", "b", ",", "=", `"`, "[", "]", `\`, "null", "<", ">", "&", " ", "é", "𝄞", "\n", " "}
	random := rand.New(rand.NewSource(1))
	piece := func() string {
		out := ""
		for n := random.Intn(4); n > 0; n-- {
			out += alphabet[random.Intn(len(alphabet))]
		}
		return out
	}

	seen := map[string]db.Key{}
	for i := 0; i < 20000; i++ {
		key := db.Key{}
		for n := 1 + random.Intn(3); n > 0; n-- {
			if random.Intn(5) == 0 {
				key[piece()] = null
			} else {
				key[piece()] = text(piece())
			}
		}
		got, err := RecordKey(key)
		if err != nil {
			t.Fatalf("RecordKey(%v): %v", key, err)
		}
		if previous, ok := seen[got]; ok && !reflect.DeepEqual(previous, key) {
			t.Fatalf("keys %v and %v both give %s", previous, key, got)
		}
		seen[got] = key
	}
}

func buildKey(nameA, valueA string, nullA bool, nameB, valueB string, nullB bool) db.Key {
	key := db.Key{}
	for _, field := range []struct {
		name, value string
		isNull      bool
	}{{nameA, valueA, nullA}, {nameB, valueB, nullB}} {
		if field.isNull {
			key[field.name] = null
		} else {
			key[field.name] = text(field.value)
		}
	}
	return key
}

func FuzzRecordKey(f *testing.F) {
	seeds := []string{",", "=", `"`, "[", "]", `\`, "<>&", "null", "", "𝄞", "\xff", "a,b=c", `["x","y"]`}
	for _, seed := range seeds {
		f.Add("a", seed, false, "b", seed, false)
		f.Add(seed, "v", false, "b", "w", true)
		f.Add("a", seed, true, "b", "", false)
	}

	f.Fuzz(func(t *testing.T, nameA, valueA string, nullA bool, nameB, valueB string, nullB bool) {
		key := buildKey(nameA, valueA, nullA, nameB, valueB, nullB)
		got, err := RecordKey(key)
		if err != nil {
			if len(key) != 0 && !hasInvalidUTF8(key) {
				t.Fatalf("refused a valid non-empty key %v: %v", key, err)
			}
			return
		}
		if hasInvalidUTF8(key) {
			t.Fatalf("accepted a key with invalid UTF-8: %v -> %s", key, got)
		}

		var pairs [][2]*string
		if err := json.Unmarshal([]byte(got), &pairs); err != nil {
			t.Fatalf("output %q is not the expected JSON: %v", got, err)
		}
		decoded := db.Key{}
		for _, pair := range pairs {
			if pair[0] == nil {
				t.Fatalf("null field name in %q", got)
			}
			if pair[1] == nil {
				decoded[*pair[0]] = null
			} else {
				decoded[*pair[0]] = text(*pair[1])
			}
		}
		if !reflect.DeepEqual(decoded, key) {
			t.Fatalf("round trip lost information: %v -> %s -> %v", key, got, decoded)
		}
	})
}

func hasInvalidUTF8(key db.Key) bool {
	for name, value := range key {
		if !utf8.ValidString(name) {
			return true
		}
		if !value.IsNull && !utf8.ValidString(value.Raw) {
			return true
		}
	}
	return false
}
