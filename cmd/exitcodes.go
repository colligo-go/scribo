package cmd

import (
	"fmt"
	"strings"

	"github.com/colligo-go/scribo/internal/exitcode"
	"github.com/spf13/cobra"
)

func newExitCodesTopic() *cobra.Command {
	return &cobra.Command{
		Use:   "exit-codes",
		Short: "The exit statuses and typed error codes of Scribo",
		Long:  exitCodesText(),
	}
}

func exitCodesText() string {
	var text strings.Builder
	text.WriteString("Every command exits with one of these statuses:\n\n")
	for _, status := range exitcode.Statuses {
		fmt.Fprintf(&text, "  %d  %s\n", status.Status, status.Meaning)
	}
	text.WriteString("\nOn stderr an error reads \"ERROR [code]: message\", and a warning \"WARNING [code]: message\".\n")
	text.WriteString("Each code below comes with the status it exits with; a warning never changes the status.\n\n")
	for _, info := range exitcode.Codes {
		status := fmt.Sprint(info.Status)
		if info.Status == exitcode.WarningStatus {
			status = "-"
		}
		fmt.Fprintf(&text, "  %-26s %s  %s\n", info.Code, status, info.Meaning)
	}
	return strings.TrimRight(text.String(), "\n")
}
