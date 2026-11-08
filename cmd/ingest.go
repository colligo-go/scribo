package cmd

import (
	"fmt"
	"strings"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/db/sqlite"
	"github.com/colligo-go/scribo/internal/exitcode"
	"github.com/colligo-go/scribo/internal/multiqc"
	"github.com/colligo-go/scribo/internal/schema"
	"github.com/colligo-go/scribo/internal/writer"
	"github.com/spf13/cobra"
)

const ingestMultiQCExamples = `  # the General Statistics, into a table made by scribo schema
  scribo schema --from-multiqc multiqc_data.json --table qc | sqlite3 study.db
  scribo ingest multiqc --db study.db --table qc multiqc_data.json

  # every per-sample field of fastp, with the fastp version
  scribo schema --from-multiqc multiqc_data.json --table fastp_qc --module fastp | sqlite3 study.db
  scribo ingest multiqc --db study.db --table fastp_qc --module fastp multiqc_data.json`

const regenerateHint = "regenerate the table with scribo schema or add the columns with ALTER TABLE"

func newIngestCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "ingest",
		Short: "Import the results of another tool",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newIngestMultiQCCommand(options))
	return command
}

func newIngestMultiQCCommand(options *globalOptions) *cobra.Command {
	var module string

	command := &cobra.Command{
		Use:   "multiqc --db DB --table T FILE [--module NAME]",
		Short: "Write the per-sample metrics of a MultiQC report, in one transaction",
		Long: `Write the per-sample metrics of multiqc_data.json to an existing table, one
record per sample keyed by the MultiQC sample name, verbatim. Every sample is
written in one transaction and one run: an error on any sample writes
nothing, and names the sample and the metric.

Without --module it writes the General Statistics, as columns named
<module>.<metric>; with --module NAME, every per-sample field of that module,
plus tool_version when the report records it. Read the table from
scribo schema: every metric of the report must be a column of the table,
and the table must have UNIQUE ("sample"). Columns of the table that the
report does not have, and samples of the table that it does not list, are
left as they are. Nothing is printed on success; warnings go to stderr.`,
		Example: ingestMultiQCExamples,
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if err := requireGlobals(options, "ingest multiqc"); err != nil {
				return err
			}
			if command.Flags().Changed("module") && module == "" {
				return usageFor("ingest multiqc", "--module needs a module name")
			}
			report, err := readReport(args[0])
			if err != nil {
				return err
			}
			table, err := multiqcTable(report, module)
			if err != nil {
				return err
			}

			stderr := command.ErrOrStderr()
			warn := func(warning exitcode.Warning) { fmt.Fprint(stderr, warning.Format()) }
			batch := writer.Batch{
				DB:          options.db,
				Table:       options.table,
				Records:     ingestRecords(table, module != ""),
				Mode:        db.ModeUpsert,
				BusyTimeout: options.busyTimeout,
				ArgvShape:   argvShape(command, args),
				ColumnsHint: regenerateHint,
			}
			if err := writer.UpsertMany(command.Context(), sqlite.New(), batch, warn); err != nil {
				return err
			}
			if len(table.Skipped) > 0 {
				warn(exitcode.Warning{
					Code:    exitcode.CodeMetricsSkipped,
					Message: fmt.Sprintf("%s: %d metrics left out: %s", options.table, len(table.Skipped), strings.Join(table.Skipped, ", ")),
					Context: "their values are objects or lists, which have no column",
				})
			}
			if module != "" && !table.HasVersion {
				warn(exitcode.Warning{
					Code:    exitcode.CodeToolVersionUnknown,
					Message: fmt.Sprintf("the report records no version of %s; tool_version is left as it is", module),
					Context: "MultiQC keeps no version of this tool in multiqc_data.json",
				})
			}
			return nil
		},
	}
	command.Flags().StringVar(&module, "module", "", "MultiQC module whose per-sample data is written, such as fastp")
	return command
}

func ingestRecords(table multiqc.Table, withVersion bool) []writer.Record {
	samples := table.Samples()
	records := make([]writer.Record, len(samples))
	for i, sample := range samples {
		values := db.Values{}
		for column, text := range table.Rows[sample] {
			values[column] = db.Value{Raw: text}
		}
		if version, ok := table.Versions[sample]; ok && withVersion {
			values[schema.VersionColumn] = db.Value{Raw: version}
		}
		records[i] = writer.Record{
			Key:    db.Key{schema.SampleColumn: db.Value{Raw: sample}},
			Values: values,
			Name:   sample,
		}
	}
	return records
}
