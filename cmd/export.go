package cmd

import (
	"bytes"
	"fmt"

	"github.com/colligo-go/scribo/internal/db/sqlite"
	"github.com/colligo-go/scribo/internal/exitcode"
	"github.com/colligo-go/scribo/internal/reader"
	"github.com/spf13/cobra"
)

const exportExamples = `  # the whole table as TSV, into a file
  scribo export --db study.db --table rnaseq_qc --out rnaseq_qc.tsv

  # replace a previous export, as CSV, with the records above a threshold
  scribo export --db study.db --table rnaseq_qc --where "Alignment_Rate>90" \
      --format csv --out passing.csv --overwrite

  # JSON Lines on stdout for jq
  scribo export --db study.db --table rnaseq_qc --format json | jq .sample`

var exportFormats = []string{"tsv", "json", "csv"}

func newExportCommand(options *globalOptions) *cobra.Command {
	flags := &readFlags{}
	var out string
	var overwrite bool

	command := &cobra.Command{
		Use:   "export --db DB --table T [--where COND]... [--is-null F]... [--not-null F]... [--out PATH] [--overwrite] [--format tsv|json|csv]",
		Short: "Write every column of the matching records to a file or stdout",
		Long: `Write every column of the records that match the filters, in the formats
and with the filters of query, to --out or to stdout.

An existing --out is refused with file_exists unless --overwrite is given.
The file is written to a temporary file in the same directory and renamed
into place, so a failure leaves the destination as it was and a reader never
sees half a file. export never writes to the database.`,
		Example: exportExamples,
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			request, err := parseRead(options, flags, "export", exportFormats)
			if err != nil {
				return err
			}
			if out != "" && !overwrite {
				if err := refuseExisting(out); err != nil {
					return err
				}
			}
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
			if out == "" {
				_, err = command.OutOrStdout().Write(buffer.Bytes())
				return err
			}
			return writeFileAtomic(out, buffer.Bytes())
		},
	}
	flags.register(command, "tsv, json or csv")
	command.Flags().StringVar(&out, "out", "", "file to write; stdout when absent")
	command.Flags().BoolVar(&overwrite, "overwrite", false, "replace --out when it exists")
	return command
}
