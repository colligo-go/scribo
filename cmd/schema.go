package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/colligo-go/scribo/internal/db"
	"github.com/colligo-go/scribo/internal/db/sqlite"
	"github.com/colligo-go/scribo/internal/exitcode"
	"github.com/colligo-go/scribo/internal/multiqc"
	"github.com/colligo-go/scribo/internal/schema"
	"github.com/colligo-go/scribo/internal/validate"
	"github.com/spf13/cobra"
)

const schemaExamples = `  # a table for the General Statistics of a MultiQC report, reviewed, then created
  scribo schema --from-multiqc multiqc_data.json --table qc > qc.sql
  sqlite3 study.db < qc.sql

  # a table for one MultiQC module, with the tool version
  scribo schema --from-multiqc multiqc_data.json --table fastp_qc --module fastp

  # a table for a TSV of results, keyed by sample and replicate
  scribo schema --from results.tsv --table rnaseq_qc --key sample --key replicate`

func newSchemaCommand(options *globalOptions) *cobra.Command {
	var fromTSV, fromMultiQC, module string
	var keys []string

	command := &cobra.Command{
		Use:   "schema (--from-multiqc FILE [--module NAME] | --from FILE.tsv --key K...) --table T",
		Short: "Print the CREATE TABLE for a MultiQC report or a TSV",
		Long: `Print a CREATE TABLE statement on stdout, for review. Scribo never
creates a data table: run the statement yourself, for example with sqlite3.
schema opens no database and executes nothing.

--from-multiqc reads the General Statistics of multiqc_data.json: one column
per metric MultiQC shows there, named <module>.<metric>, so adding a module
later never renames a column. --module NAME reads every per-sample field of
that module instead, under its own name, plus a tool_version column. The
table has a "sample" TEXT NOT NULL column and UNIQUE ("sample").

--from reads a TSV with its header on the first line; the --key columns are
NOT NULL and form the UNIQUE constraint.

A column is INTEGER when every value is an integer, REAL when every value is
a number and one is not an integer (593374960.0 is REAL), and TEXT otherwise.
The table is STRICT.`,
		Example: schemaExamples,
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			definition, err := buildSchema(command, options.table, fromTSV, fromMultiQC, module, keys)
			if err != nil {
				return err
			}
			_, err = io.WriteString(command.OutOrStdout(), sqlite.CreateTable(definition))
			return err
		},
	}
	command.Flags().StringVar(&fromMultiQC, "from-multiqc", "", "multiqc_data.json to read")
	command.Flags().StringVar(&module, "module", "", "MultiQC module whose per-sample data becomes the table, such as fastp")
	command.Flags().StringVar(&fromTSV, "from", "", "TSV file with a header line to read")
	command.Flags().StringArrayVar(&keys, "key", nil, "column of the TSV that names a record; repeat for a composite key")
	return command
}

func buildSchema(command *cobra.Command, table, fromTSV, fromMultiQC, module string, keys []string) (db.TableDefinition, error) {
	switch {
	case table == "":
		return db.TableDefinition{}, usageFor("schema", "--table is required")
	case fromTSV == "" && fromMultiQC == "":
		return db.TableDefinition{}, usageFor("schema", "give --from-multiqc FILE or --from FILE.tsv")
	case fromTSV != "" && fromMultiQC != "":
		return db.TableDefinition{}, usageFor("schema", "--from and --from-multiqc cannot be given together")
	case fromMultiQC != "" && command.Flags().Changed("key"):
		return db.TableDefinition{}, usageFor("schema", "--key applies to --from; a MultiQC table is keyed by sample")
	case fromTSV != "" && command.Flags().Changed("module"):
		return db.TableDefinition{}, usageFor("schema", "--module applies to --from-multiqc")
	case command.Flags().Changed("module") && module == "":
		return db.TableDefinition{}, usageFor("schema", "--module needs a module name")
	}
	if validate.Reserved(table) {
		return db.TableDefinition{}, exitcode.New(exitcode.Database, exitcode.CodeReservedTable,
			fmt.Sprintf("%s is a reserved table", table), "Scribo and Colligo own this table; name a data table")
	}

	if fromMultiQC != "" {
		report, err := readReport(fromMultiQC)
		if err != nil {
			return db.TableDefinition{}, err
		}
		read, err := multiqcTable(report, module)
		if err != nil {
			return db.TableDefinition{}, err
		}
		return schema.FromMultiQC(table, read, module != "")
	}

	if err := checkSchemaKeys(keys); err != nil {
		return db.TableDefinition{}, err
	}
	file, err := openInput(fromTSV)
	if err != nil {
		return db.TableDefinition{}, err
	}
	defer file.Close()
	definition, err := schema.FromTSV(table, file, keys)
	if err != nil {
		return db.TableDefinition{}, readError(err, fromTSV)
	}
	return definition, nil
}

func checkSchemaKeys(keys []string) error {
	if len(keys) == 0 {
		return usageFor("schema", "--key is required with --from")
	}
	seen := map[string]bool{}
	for _, key := range keys {
		if key == "" {
			return usageFor("schema", "--key needs a column name")
		}
		if seen[key] {
			return usageFor("schema", "key column %q is given twice", key)
		}
		seen[key] = true
	}
	return nil
}

func readReport(path string) (*multiqc.Report, error) {
	file, err := openInput(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	report, err := multiqc.Read(file)
	if err != nil {
		return nil, readError(err, path)
	}
	return report, nil
}

func multiqcTable(report *multiqc.Report, module string) (multiqc.Table, error) {
	if module == "" {
		return report.GeneralStats()
	}
	return report.Module(module)
}

func openInput(path string) (*os.File, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, inputError(err, path)
	}
	return file, nil
}

func readError(err error, path string) error {
	var domain *exitcode.Error
	if errors.As(err, &domain) {
		return err
	}
	return inputError(err, path)
}

func inputError(err error, path string) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return exitcode.New(exitcode.File, exitcode.CodeFileNotFound, fmt.Sprintf("%s does not exist", path), "")
	case errors.Is(err, fs.ErrPermission):
		return exitcode.New(exitcode.System, exitcode.CodePermissionDenied,
			fmt.Sprintf("cannot read %s: permission denied", path), "")
	}
	return exitcode.New(exitcode.System, exitcode.CodeIOError, fmt.Sprintf("cannot read %s: %v", path, err), "")
}
