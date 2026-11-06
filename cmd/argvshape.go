package cmd

import (
	"github.com/colligo-go/scribo/internal/run"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func argvShape(command *cobra.Command, args []string) string {
	var names []string
	command.Flags().Visit(func(flag *pflag.Flag) {
		names = append(names, flag.Name)
	})
	return run.ArgvShape(command.CommandPath(), names, len(args))
}
