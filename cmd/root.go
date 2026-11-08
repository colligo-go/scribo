package cmd

import (
	"context"
	"errors"
	"os"

	"github.com/colligo-go/scribo/internal/exitcode"
	"github.com/colligo-go/scribo/internal/run"
	"github.com/spf13/cobra"
)

type handler func(*cobra.Command, []string) error

type globalOptions struct {
	db          string
	table       string
	busyTimeout int
}

func newRootCommand() *cobra.Command {
	options := &globalOptions{}
	root := &cobra.Command{
		Use:           "scribo",
		Short:         "Ledger of pipeline results: values per sample, with provenance and staleness",
		Args:          cobra.NoArgs,
		Version:       run.Version(),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	root.PersistentFlags().StringVar(&options.db, "db", "", "path of the SQLite database file")
	root.PersistentFlags().StringVar(&options.table, "table", "", "name of the data table")
	root.PersistentFlags().IntVar(&options.busyTimeout, "busy-timeout", 5000, "milliseconds to wait for a lock held by another process")
	root.AddCommand(newUpsertCommand(options))
	root.AddCommand(newLogCommand(options))
	root.AddCommand(newQueryCommand(options))
	root.AddCommand(newExportCommand(options))
	root.AddCommand(newSchemaCommand(options))
	root.AddCommand(newIngestCommand(options))
	root.AddCommand(newExitCodesTopic())
	root.SetVersionTemplate("scribo {{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError(err)
	})
	return root
}

func Execute(ctx context.Context) *exitcode.Error {
	return execute(ctx, newRootCommand(), os.Args[1:])
}

func execute(ctx context.Context, root *cobra.Command, args []string) *exitcode.Error {
	typeHandlerErrors(root)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return nil
	}
	var domain *exitcode.Error
	if errors.As(err, &domain) {
		return domain
	}
	return usageError(err)
}

func typeHandlerErrors(command *cobra.Command) {
	if command.RunE != nil {
		command.RunE = typed(command.RunE)
	}
	for _, child := range command.Commands() {
		typeHandlerErrors(child)
	}
}

func typed(run handler) handler {
	return func(command *cobra.Command, args []string) error {
		err := run(command, args)
		if err == nil {
			return nil
		}
		var domain *exitcode.Error
		if errors.As(err, &domain) {
			return domain
		}
		return exitcode.New(exitcode.System, exitcode.CodeInternal, err.Error(), "an error Scribo did not classify: this is a bug, please report it")
	}
}

func usageError(err error) *exitcode.Error {
	return exitcode.New(exitcode.Usage, exitcode.CodeUsage, err.Error(), "run 'scribo --help' for usage")
}
