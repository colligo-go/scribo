package cmd

import (
	"bytes"
	"fmt"

	"github.com/colligo-go/scribo/internal/db/sqlite"
	"github.com/colligo-go/scribo/internal/exitcode"
	"github.com/colligo-go/scribo/internal/reader"
	"github.com/spf13/cobra"
)

const queryExamples = `  # every record, ordered by the table's unique key
  scribo query --db study.db --table rnaseq_qc

  # a numeric filter: Alignment_Rate is REAL, so 100 > 90
  scribo query --db study.db --table rnaseq_qc --where "Alignment_Rate>90" \
      --fields sample,replicate,Alignment_Rate

  # the records whose note is a real NULL; --where note=null means the text "null"
  scribo query --db study.db --table rnaseq_qc --is-null note

  # a date kept as ISO text compared as text
  scribo query --db study.db --table rnaseq_qc --where "run_date>=2026-01-01" --force-text

  # JSON Lines for jq
  scribo query --db study.db --table rnaseq_qc --format json | jq .Mapped_Reads`

var queryFormats = []string{"tsv", "json", "csv", "pretty"}

func newQueryCommand(options *globalOptions) *cobra.Command {
	flags := &readFlags{}
	var limit int

	command := &cobra.Command{
		Use:   "query --db DB --table T [--where COND]... [--is-null F]... [--not-null F]... [--fields F1,F2] [--format tsv|json|csv|pretty] [--limit N]",
		Short: "Print the records of a table that match the filters",
		Long: `Print the records of a data table, ordered by its unique key.

--where FIELD<op>VALUE filters with one of = != > < >= <=, and every filter
is joined by AND. On a numeric column the value must be a number and the
comparison is numeric. On any other column only = and != are allowed:
> < >= <= would compare text, and need --force-text to say so. != keeps the
records where the field is NULL. "null" in a value is the text null; a real
NULL is filtered with --is-null FIELD or --not-null FIELD.

TSV (the default) writes a NULL as \N and escapes backslash, tab, newline and
carriage return as \\, \t, \n and \r. CSV follows RFC 4180, with a NULL as an
empty field and the empty text as "". JSON writes one object per line with
the columns in table order. pretty aligns the columns for a person, with a
NULL as ∅. query never writes to the database.`,
		Example: queryExamples,
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			request, err := parseRead(options, flags, "query", queryFormats)
			if err != nil {
				return err
			}
			if command.Flags().Changed("limit") && limit <= 0 {
				return usageFor("query", "--limit must be greater than zero")
			}
			request.Limit = limit
			stderr := command.ErrOrStderr()
			result, err := reader.Query(command.Context(), sqlite.New(), request, func(warning exitcode.Warning) {
				fmt.Fprint(stderr, warning.Format())
			})
			if err != nil {
				return err
			}
			var buffer bytes.Buffer
			if err := writeTable(&buffer, flags.format, result); err != nil {
				return err
			}
			_, err = command.OutOrStdout().Write(buffer.Bytes())
			return err
		},
	}
	flags.register(command, "tsv, json, csv or pretty")
	command.Flags().StringArrayVar(&flags.fields, "fields", nil, "comma-separated fields to print, in this order; repeatable")
	command.Flags().IntVar(&limit, "limit", 0, "print at most N records")
	return command
}
