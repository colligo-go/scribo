package cmd

import (
	"errors"

	"github.com/colligo-go/scribo/internal/exitcode"
	"github.com/spf13/cobra"
)

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "scribo",
		Short:         "Ledger of pipeline results: values per sample, with provenance and staleness",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError(err)
	})
	return root
}

func usageError(err error) *exitcode.Error {
	return exitcode.New(exitcode.Usage, exitcode.CodeUsage, err.Error(), "run 'scribo --help' for usage")
}

func Execute() error {
	err := newRootCommand().Execute()
	if err == nil {
		return nil
	}
	var domain *exitcode.Error
	if errors.As(err, &domain) {
		return err
	}
	return usageError(err)
}
