package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/db/sqlite"
	"github.com/colligo-go/scribo/internal/exitcode"
	"github.com/colligo-go/scribo/internal/format"
	"github.com/colligo-go/scribo/internal/reader"
	"github.com/spf13/cobra"
)

const logExamples = `  # every change of one record, oldest first
  scribo log --db study.db --table rnaseq_qc --key sample=S1 --key replicate=1

  # the record as it stood at a moment, one field per line
  scribo log --db study.db --table rnaseq_qc --key sample=S1 --key replicate=1 \
      --at 2026-11-06T12:00:00Z

  # the history of one field as JSON Lines
  scribo log --db study.db --table rnaseq_qc --key sample=S1 --key replicate=1 \
      --field Mapped_Reads --format json`

const timestampLayout = "2006-01-02T15:04:05.000Z"

func newLogCommand(options *globalOptions) *cobra.Command {
	var keys []string
	var field, at, format string

	command := &cobra.Command{
		Use:   "log --db DB --table T --key K=V... [--field F] [--at TIMESTAMP] [--format tsv|json]",
		Short: "Show the history of one record, or its state at a moment",
		Long: `Show every change recorded for the record the --key flags name, oldest first.

With --at, show instead the value of each field at that moment. TIMESTAMP is
RFC 3339 with a time and a zone, such as 2026-11-06T12:00:00Z or
2026-11-06T14:00:00+02:00; a date alone is ambiguous and refused. A field whose
next change after that moment was made outside Scribo is reported with a
history_approximate warning: the exact moment of that change is unknown.

TSV escapes backslash, tab, newline and carriage return as \\, \t, \n and \r;
a NULL is an empty cell with is_null 1. JSON writes one object per line. log
never writes to the database.`,
		Example: logExamples,
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			request, err := parseLog(options, keys, field, at, format)
			if err != nil {
				return err
			}
			stderr := command.ErrOrStderr()
			warn := func(warning exitcode.Warning) { fmt.Fprint(stderr, warning.Format()) }
			result, err := reader.Log(command.Context(), sqlite.New(), request, warn)
			if err != nil {
				return err
			}
			for _, name := range result.Approximate {
				warn(exitcode.Warning{
					Code:    exitcode.CodeHistoryApproximate,
					Message: fmt.Sprintf("state of %s at %s is approximate", name, request.At),
					Context: "a change made outside Scribo was found later; the moment it happened is unknown",
				})
			}
			return writeLog(command.OutOrStdout(), result, request.At != "", format)
		},
	}
	command.Flags().StringArrayVar(&keys, "key", nil, "FIELD=VALUE naming the record; repeat for a composite key")
	command.Flags().StringVar(&field, "field", "", "show only this field")
	command.Flags().StringVar(&at, "at", "", "RFC 3339 moment at which to show the record's state")
	command.Flags().StringVar(&format, "format", "tsv", "output format: tsv or json")
	command.MarkFlagRequired("key")
	return command
}

func parseLog(options *globalOptions, keys []string, field, at, format string) (reader.LogRequest, error) {
	if err := requireGlobals(options, "log"); err != nil {
		return reader.LogRequest{}, err
	}
	key, err := parseKeys(keys, "log")
	if err != nil {
		return reader.LogRequest{}, err
	}
	if format != "tsv" && format != "json" {
		return reader.LogRequest{}, usageFor("log", "--format must be tsv or json, not %q", format)
	}
	moment := ""
	if at != "" {
		parsed, err := time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return reader.LogRequest{}, usageFor("log", "--at %q is not an RFC 3339 timestamp such as 2026-11-06T12:00:00Z", at)
		}
		moment = parsed.UTC().Truncate(time.Millisecond).Format(timestampLayout)
	}
	return reader.LogRequest{
		DB:          options.db,
		Table:       options.table,
		Key:         key,
		Field:       field,
		At:          moment,
		BusyTimeout: options.busyTimeout,
	}, nil
}

func writeLog(out io.Writer, result reader.LogResult, atMoment bool, format string) error {
	var buffer bytes.Buffer
	var err error
	switch {
	case atMoment && format == "json":
		err = writeStateJSON(&buffer, result.State)
	case atMoment:
		writeStateTSV(&buffer, result.State)
	case format == "json":
		err = writeHistoryJSON(&buffer, result.Rows)
	default:
		writeHistoryTSV(&buffer, result.Rows)
	}
	if err != nil {
		return err
	}
	_, err = out.Write(buffer.Bytes())
	return err
}

func tsvCell(value db.Value) (string, string) {
	if value.IsNull {
		return "", "1"
	}
	return format.Escape(value.Raw), "0"
}

func writeHistoryTSV(out *bytes.Buffer, rows []db.HistoryRow) {
	out.WriteString("changed_at\trun_id\tsource\tfield\told_value\told_is_null\tnew_value\tnew_is_null\n")
	for _, row := range rows {
		oldValue, oldIsNull := tsvCell(row.Old)
		newValue, newIsNull := tsvCell(row.New)
		cells := []string{
			row.ChangedAt, strconv.FormatInt(row.RunID, 10), row.Source, format.Escape(row.Field),
			oldValue, oldIsNull, newValue, newIsNull,
		}
		out.WriteString(strings.Join(cells, "\t") + "\n")
	}
}

func writeStateTSV(out *bytes.Buffer, state db.Row) {
	out.WriteString("field\tvalue\tis_null\n")
	for _, name := range sortedFields(state) {
		value, isNull := tsvCell(state[name])
		out.WriteString(format.Escape(name) + "\t" + value + "\t" + isNull + "\n")
	}
}

func jsonValue(value db.Value) *string {
	if value.IsNull {
		return nil
	}
	raw := value.Raw
	return &raw
}

type historyLine struct {
	ChangedAt string  `json:"changed_at"`
	RunID     int64   `json:"run_id"`
	Source    string  `json:"source"`
	Field     string  `json:"field"`
	Old       *string `json:"old"`
	New       *string `json:"new"`
}

func writeHistoryJSON(out *bytes.Buffer, rows []db.HistoryRow) error {
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	for _, row := range rows {
		line := historyLine{
			ChangedAt: row.ChangedAt, RunID: row.RunID, Source: row.Source, Field: row.Field,
			Old: jsonValue(row.Old), New: jsonValue(row.New),
		}
		if err := encoder.Encode(line); err != nil {
			return err
		}
	}
	return nil
}

func writeStateJSON(out *bytes.Buffer, state db.Row) error {
	object := make(map[string]*string, len(state))
	for name, value := range state {
		object[name] = jsonValue(value)
	}
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(object)
}

func sortedFields(row db.Row) []string {
	names := make([]string, 0, len(row))
	for name := range row {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
