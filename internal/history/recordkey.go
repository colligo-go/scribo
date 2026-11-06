package history

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/exitcode"
)

func RecordKey(key db.Key) (string, error) {
	if len(key) == 0 {
		return "", exitcode.New(exitcode.Usage, exitcode.CodeUsage,
			"record key has no fields",
			"give at least one key field")
	}

	names := make([]string, 0, len(key))
	for name := range key {
		names = append(names, name)
	}
	sort.Strings(names)

	pairs := make([][]any, 0, len(names))
	for _, name := range names {
		value := key[name]
		if !utf8.ValidString(name) {
			return "", invalidUTF8("a field name")
		}
		if value.IsNull {
			pairs = append(pairs, []any{name, nil})
			continue
		}
		if !utf8.ValidString(value.Raw) {
			return "", invalidUTF8("the value of field " + strconv.Quote(name))
		}
		pairs = append(pairs, []any{name, value.Raw})
	}

	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(pairs); err != nil {
		return "", exitcode.New(exitcode.System, exitcode.CodeInternal,
			"cannot encode record key: "+err.Error(), "")
	}
	return strings.TrimSuffix(buffer.String(), "\n"), nil
}

func invalidUTF8(what string) *exitcode.Error {
	return exitcode.New(exitcode.Data, exitcode.CodeInvalidValue,
		what+" is not valid UTF-8",
		"record keys must be valid UTF-8 text")
}
