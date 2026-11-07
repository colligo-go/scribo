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
      --field Mapped_Reads --format json

  # aligned for a terminal, with the version and exit code of each run
  scribo log --db study.db --table rnaseq_qc --key sample=S1 --key replicate=1 \
      --with-runs --format pretty

  # the record right after run 7 wrote to this table
  scribo log --db study.db --table rnaseq_qc --key sample=S1 --key replicate=1 --at run:7`

const timestampLayout = "2006-01-02T15:04:05.000Z"

func newLogCommand(options *globalOptions) *cobra.Command {
	var keys []string
	var field, at, format string
	var withRuns bool

	command := &cobra.Command{
		Use:   "log --db DB --table T --key K=V... [--field F] [--at TIMESTAMP|run:N] [--with-runs] [--format tsv|json|pretty]",
		Short: "Show the history of one record, or its state at a moment",
		Long: `Show every change recorded for the record the --key flags name, oldest first.

With --at, show instead the value of each field at that moment. TIMESTAMP is
RFC 3339 with a time and a zone, such as 2026-11-06T12:00:00Z or
2026-11-06T14:00:00+02:00; a date alone is ambiguous and refused. --at run:N
shows the record right after the last change run N made to this table, to
any record; a run that wrote nothing here is run_not_found. A field whose
next change after that point was made outside Scribo is reported with a
history_approximate warning: the exact moment of that change is unknown.

--with-runs adds the tool_version, step and exit_code of the run that wrote
each row; exit_code is empty while a run is open or after it was killed.

TSV escapes backslash, tab, newline and carriage return as \\, \t, \n and \r;
a NULL is an empty cell with is_null 1. JSON writes one object per line.
pretty aligns the columns for a person, drops the is_null columns and shows
a NULL as ∅. log never writes to the database.`,
		Example: logExamples,
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			request, err := parseLog(options, keys, field, at, format, withRuns)
			if err != nil {
				return err
			}
			stderr := command.ErrOrStderr()
			warn := func(warning exitcode.Warning) { fmt.Fprint(stderr, warning.Format()) }
			result, err := reader.Log(command.Context(), sqlite.New(), request, warn)
			if err != nil {
				return err
			}
			label := request.At
			if request.AtRun > 0 {
				label = at
			}
			for _, name := range result.Approximate {
				warn(exitcode.Warning{
					Code:    exitcode.CodeHistoryApproximate,
					Message: fmt.Sprintf("state of %s at %s is approximate", name, label),
					Context: "a change made outside Scribo was found later; the moment it happened is unknown",
				})
			}
			return writeLog(command.OutOrStdout(), result, at != "", format, withRuns)
		},
	}
	command.Flags().StringArrayVar(&keys, "key", nil, "FIELD=VALUE naming the record; repeat for a composite key")
	command.Flags().StringVar(&field, "field", "", "show only this field")
	command.Flags().StringVar(&at, "at", "", "RFC 3339 moment, or run:N, at which to show the record's state")
	command.Flags().BoolVar(&withRuns, "with-runs", false, "add tool_version, step and exit_code of the run behind each row")
	command.Flags().StringVar(&format, "format", "tsv", "output format: tsv, json or pretty")
	command.MarkFlagRequired("key")
	return command
}

func parseLog(options *globalOptions, keys []string, field, at, format string, withRuns bool) (reader.LogRequest, error) {
	if err := requireGlobals(options, "log"); err != nil {
		return reader.LogRequest{}, err
	}
	key, err := parseKeys(keys, "log")
	if err != nil {
		return reader.LogRequest{}, err
	}
	if format != "tsv" && format != "json" && format != "pretty" {
		return reader.LogRequest{}, usageFor("log", "--format must be tsv, json or pretty, not %q", format)
	}
	if withRuns && at != "" {
		return reader.LogRequest{}, usageFor("log", "--with-runs shows the history; it does not apply to --at")
	}
	request := reader.LogRequest{
		DB:          options.db,
		Table:       options.table,
		Key:         key,
		Field:       field,
		WithRuns:    withRuns,
		BusyTimeout: options.busyTimeout,
	}
	if text, isRun := strings.CutPrefix(at, "run:"); isRun {
		id, err := strconv.ParseInt(text, 10, 64)
		if err != nil || id <= 0 {
			return reader.LogRequest{}, usageFor("log", "--at %q needs a run id greater than zero, such as run:7", at)
		}
		request.AtRun = id
		return request, nil
	}
	if at != "" {
		parsed, err := time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return reader.LogRequest{}, usageFor("log", "--at %q is not an RFC 3339 timestamp such as 2026-11-06T12:00:00Z, nor run:N", at)
		}
		request.At = parsed.UTC().Truncate(time.Millisecond).Format(timestampLayout)
	}
	return request, nil
}

func writeLog(out io.Writer, result reader.LogResult, state bool, name string, withRuns bool) error {
	var buffer bytes.Buffer
	var err error
	switch {
	case state && name == "json":
		err = writeStateJSON(&buffer, result.State)
	case state && name == "pretty":
		err = writeStatePretty(&buffer, result.State)
	case state:
		writeStateTSV(&buffer, result.State)
	case name == "json":
		err = writeHistoryJSON(&buffer, result, withRuns)
	case name == "pretty":
		err = writeHistoryPretty(&buffer, result, withRuns)
	default:
		writeHistoryTSV(&buffer, result, withRuns)
	}
	if err != nil {
		return err
	}
	_, err = out.Write(buffer.Bytes())
	return err
}

var runColumns = []string{"tool_version", "step", "exit_code"}

func runCells(runs map[int64]db.RunInfo, runID int64) []db.Value {
	info := runs[runID]
	exit := db.Value{IsNull: true}
	if info.ExitCode.Valid {
		exit = db.Value{Raw: strconv.FormatInt(info.ExitCode.Int64, 10)}
	}
	return []db.Value{{Raw: info.ToolVersion}, {Raw: info.Step}, exit}
}

func writeHistoryPretty(out *bytes.Buffer, result reader.LogResult, withRuns bool) error {
	columns := []string{"changed_at", "run_id", "source", "field", "old_value", "new_value"}
	if withRuns {
		columns = append(columns, runColumns...)
	}
	rows := make([][]db.Value, len(result.Rows))
	for i, row := range result.Rows {
		cells := []db.Value{
			{Raw: row.ChangedAt}, {Raw: strconv.FormatInt(row.RunID, 10)}, {Raw: row.Source}, {Raw: row.Field}, row.Old, row.New,
		}
		if withRuns {
			cells = append(cells, runCells(result.Runs, row.RunID)...)
		}
		rows[i] = cells
	}
	return format.Pretty(out, columns, rows)
}

func writeStatePretty(out *bytes.Buffer, state db.Row) error {
	var rows [][]db.Value
	for _, name := range sortedFields(state) {
		rows = append(rows, []db.Value{{Raw: name}, state[name]})
	}
	return format.Pretty(out, []string{"field", "value"}, rows)
}

func tsvCell(value db.Value) (string, string) {
	if value.IsNull {
		return "", "1"
	}
	return format.Escape(value.Raw), "0"
}

func writeHistoryTSV(out *bytes.Buffer, result reader.LogResult, withRuns bool) {
	header := "changed_at\trun_id\tsource\tfield\told_value\told_is_null\tnew_value\tnew_is_null"
	if withRuns {
		header += "\t" + strings.Join(runColumns, "\t")
	}
	out.WriteString(header + "\n")
	for _, row := range result.Rows {
		oldValue, oldIsNull := tsvCell(row.Old)
		newValue, newIsNull := tsvCell(row.New)
		cells := []string{
			row.ChangedAt, strconv.FormatInt(row.RunID, 10), row.Source, format.Escape(row.Field),
			oldValue, oldIsNull, newValue, newIsNull,
		}
		if withRuns {
			for _, value := range runCells(result.Runs, row.RunID) {
				text, _ := tsvCell(value)
				cells = append(cells, text)
			}
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

type historyRunLine struct {
	historyLine
	ToolVersion string `json:"tool_version"`
	Step        string `json:"step"`
	ExitCode    *int64 `json:"exit_code"`
}

func writeHistoryJSON(out *bytes.Buffer, result reader.LogResult, withRuns bool) error {
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	for _, row := range result.Rows {
		line := historyLine{
			ChangedAt: row.ChangedAt, RunID: row.RunID, Source: row.Source, Field: row.Field,
			Old: jsonValue(row.Old), New: jsonValue(row.New),
		}
		var err error
		if withRuns {
			info := result.Runs[row.RunID]
			withRun := historyRunLine{historyLine: line, ToolVersion: info.ToolVersion, Step: info.Step}
			if info.ExitCode.Valid {
				withRun.ExitCode = &info.ExitCode.Int64
			}
			err = encoder.Encode(withRun)
		} else {
			err = encoder.Encode(line)
		}
		if err != nil {
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
