package cmd

import (
	"fmt"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/db/sqlite"
	"github.com/colligo-go/scribo/internal/exitcode"
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
      --key sample=S1 --key replicate=1 Mapped_Reads=13`

func newUpsertCommand(options *globalOptions) *cobra.Command {
	var keys, nulls []string
	var insertOnly, updateOnly bool

	command := &cobra.Command{
		Use:   "upsert --db DB --table T --key K=V... [FIELD=VALUE]... [--null FIELD]...",
		Short: "Write values to one record and record what changed",
		Long: `Write values to the record the --key flags name, creating it if it does not exist.

Each FIELD=VALUE argument is split at the first "=", so note=a=b stores "a=b".
FIELD=null stores the text "null"; a real NULL is --null FIELD. Values for a
numeric column must be decimal numbers. Only the fields whose stored value
really changes get a row in {table}_history; writing the same value again
writes nothing there. The table must already exist and its key columns should
be UNIQUE. Nothing is printed on success.`,
		Example: upsertExamples,
		Args:    cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			request, err := parseUpsert(options, keys, nulls, args, insertOnly, updateOnly)
			if err != nil {
				return err
			}
			request.ArgvShape = argvShape(command, args)
			stderr := command.ErrOrStderr()
			return writer.Upsert(command.Context(), sqlite.New(), request, func(warning exitcode.Warning) {
				fmt.Fprint(stderr, warning.Format())
			})
		},
	}
	command.Flags().StringArrayVar(&keys, "key", nil, "FIELD=VALUE naming the record; repeat for a composite key")
	command.Flags().StringArrayVar(&nulls, "null", nil, "FIELD to set to a real NULL; repeatable")
	command.Flags().BoolVar(&insertOnly, "insert-only", false, "fail with key_already_exists if the record exists")
	command.Flags().BoolVar(&updateOnly, "update-only", false, "fail with key_not_found if the record does not exist")
	command.MarkFlagRequired("key")
	command.MarkFlagsMutuallyExclusive("insert-only", "update-only")
	return command
}

func parseUpsert(options *globalOptions, keys, nulls, args []string, insertOnly, updateOnly bool) (writer.Request, error) {
	if err := requireGlobals(options, "upsert"); err != nil {
		return writer.Request{}, err
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
