package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/db/sqlite"
	"github.com/colligo-go/scribo/internal/exitcode"
	"github.com/colligo-go/scribo/internal/format"
	"github.com/colligo-go/scribo/internal/writer"
	"github.com/spf13/cobra"
)

const upsertExamples = `  # a new record: the key says which one, the arguments are the values
  scribo upsert --db study.db --table rnaseq_qc \
      --key sample=S1 --key replicate=1 Alignment_Rate=94.3 Mapped_Reads=12

  # a real NULL; FIELD=null would store the text "null"
  scribo upsert --db study.db --table rnaseq_qc \
      --key sample=S1 --key replicate=1 --null note

  # fix a record that must already exist, and fail if it does not
  scribo upsert --db study.db --table rnaseq_qc --update-only \
      --key sample=S1 --key replicate=1 Mapped_Reads=13

  # see what a write would change, without writing anything
  scribo upsert --db study.db --table rnaseq_qc --dry-run \
      --key sample=S1 --key replicate=1 Alignment_Rate=95.1`

func newUpsertCommand(options *globalOptions) *cobra.Command {
	var keys, nulls []string
	var insertOnly, updateOnly, dryRun bool
	var outputFormat string

	command := &cobra.Command{
		Use:   "upsert --db DB --table T --key K=V... [FIELD=VALUE]... [--null FIELD]...",
		Short: "Write values to one record and record what changed",
		Long: `Write values to the record the --key flags name, creating it if it does not exist.

Each FIELD=VALUE argument is split at the first "=", so note=a=b stores "a=b".
FIELD=null stores the text "null"; a real NULL is --null FIELD. Values for a
numeric column must be decimal numbers. Only the fields whose stored value
really changes get a row in {table}_history; writing the same value again
writes nothing there. The table must already exist and the --key fields must
be exactly one of its unique keys. Nothing is printed on success.

--dry-run does everything up to the write inside a transaction that is
rolled back, and prints the changes the write would record, one per line:
first the changes found made outside Scribo (source reconciled), then its
own (source scribo). It writes nothing at all: no tool_runs row, no
{table}_history created. --format json prints one object per line.`,
		Example: upsertExamples,
		Args:    cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			request, err := parseUpsert(options, keys, nulls, args, insertOnly, updateOnly)
			if err != nil {
				return err
			}
			if command.Flags().Changed("format") && !dryRun {
				return usageFor("upsert", "--format applies to --dry-run only")
			}
			if outputFormat != "tsv" && outputFormat != "json" {
				return usageFor("upsert", "--format must be tsv or json, not %q", outputFormat)
			}
			request.ArgvShape = argvShape(command, args)
			stderr := command.ErrOrStderr()
			warn := func(warning exitcode.Warning) { fmt.Fprint(stderr, warning.Format()) }
			if dryRun {
				changes, err := writer.DryRun(command.Context(), sqlite.New(), request, warn)
				if err != nil {
					return err
				}
				return writeDryRun(command.OutOrStdout(), changes, outputFormat)
			}
			return writer.Upsert(command.Context(), sqlite.New(), request, warn)
		},
	}
	command.Flags().StringArrayVar(&keys, "key", nil, "FIELD=VALUE naming the record; repeat for a composite key")
	command.Flags().StringArrayVar(&nulls, "null", nil, "FIELD to set to a real NULL; repeatable")
	command.Flags().BoolVar(&insertOnly, "insert-only", false, "fail with key_already_exists if the record exists")
	command.Flags().BoolVar(&updateOnly, "update-only", false, "fail with key_not_found if the record does not exist")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "print the changes the write would record, and write nothing")
	command.Flags().StringVar(&outputFormat, "format", "tsv", "output of --dry-run: tsv or json")
	command.MarkFlagsMutuallyExclusive("insert-only", "update-only")
	return command
}

func parseUpsert(options *globalOptions, keys, nulls, args []string, insertOnly, updateOnly bool) (writer.Request, error) {
	if err := requireGlobals(options, "upsert"); err != nil {
		return writer.Request{}, err
	}
	if len(keys) == 0 && len(args) > 0 {
		return writer.Request{}, missingKeyHint(args[0])
	}
	key, err := parseKeys(keys, "upsert")
	if err != nil {
		return writer.Request{}, err
	}

	values := db.Values{}
	for _, text := range args {
		name, value, ok := splitAssignment(text)
		if !ok {
			return writer.Request{}, usageFor("upsert", "%q must be FIELD=VALUE", text)
		}
		if name == "" {
			return writer.Request{}, usageFor("upsert", "%q has an empty field name", text)
		}
		if err := addField(values, key, name, db.Value{Raw: value}); err != nil {
			return writer.Request{}, err
		}
	}
	for _, name := range nulls {
		if name == "" {
			return writer.Request{}, usageFor("upsert", "--null needs a field name")
		}
		if err := addField(values, key, name, db.Value{IsNull: true}); err != nil {
			return writer.Request{}, err
		}
	}
	if len(values) == 0 {
		return writer.Request{}, usageFor("upsert", "nothing to write: give FIELD=VALUE arguments or --null FIELD")
	}

	mode := db.ModeUpsert
	switch {
	case insertOnly:
		mode = db.ModeInsertOnly
	case updateOnly:
		mode = db.ModeUpdateOnly
	}
	return writer.Request{
		DB:          options.db,
		Table:       options.table,
		Key:         key,
		Values:      values,
		Mode:        mode,
		BusyTimeout: options.busyTimeout,
	}, nil
}

func addField(values db.Values, key db.Key, name string, value db.Value) error {
	if _, isKey := key[name]; isKey {
		return usageFor("upsert", "field %q is a key field and cannot also be written", name)
	}
	if _, repeated := values[name]; repeated {
		return usageFor("upsert", "field %q is given more than once", name)
	}
	values[name] = value
	return nil
}

func missingKeyHint(first string) *exitcode.Error {
	example := "sample=S1"
	if name, _, ok := splitAssignment(first); ok && name != "" {
		example = first
	}
	return exitcode.New(exitcode.Usage, exitcode.CodeUsage, "--key is required",
		"the record key goes in --key, for example --key "+example)
}

type dryRunLine struct {
	Source string  `json:"source"`
	Field  string  `json:"field"`
	Old    *string `json:"old"`
	New    *string `json:"new"`
}

func sourceName(source db.HistorySource) string {
	if source == db.SourceReconciled {
		return "reconciled"
	}
	return "scribo"
}

func writeDryRun(out io.Writer, changes []writer.Change, name string) error {
	var buffer bytes.Buffer
	if name == "json" {
		encoder := json.NewEncoder(&buffer)
		encoder.SetEscapeHTML(false)
		for _, change := range changes {
			line := dryRunLine{Source: sourceName(change.Source), Field: change.Field, Old: jsonValue(change.OldValue), New: jsonValue(change.NewValue)}
			if err := encoder.Encode(line); err != nil {
				return err
			}
		}
	} else {
		buffer.WriteString("source\tfield\told_value\told_is_null\tnew_value\tnew_is_null\n")
		for _, change := range changes {
			oldValue, oldIsNull := tsvCell(change.OldValue)
			newValue, newIsNull := tsvCell(change.NewValue)
			cells := []string{sourceName(change.Source), format.Escape(change.Field), oldValue, oldIsNull, newValue, newIsNull}
			buffer.WriteString(strings.Join(cells, "\t") + "\n")
		}
	}
	_, err := out.Write(buffer.Bytes())
	return err
}
