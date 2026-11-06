package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestArgvShapeNeverContainsValues(t *testing.T) {
	const sentinel = "LEAK-7f3a9c"
	var got string

	root := &cobra.Command{Use: "scribo"}
	child := &cobra.Command{
		Use:  "test",
		Args: cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			got = argvShape(command, args)
			return nil
		},
	}
	child.Flags().String("db", "", "")
	child.Flags().String("table", "", "")
	child.Flags().String("step", "", "")
	root.AddCommand(child)
	root.SetArgs([]string{"test", "--db", sentinel, "--table", sentinel, sentinel})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	want := `{"command":"scribo test","flags":["db","table"],"args":1}`
	if got != want {
		t.Errorf("argvShape = %s, want %s", got, want)
	}
	if strings.Contains(got, sentinel) {
		t.Errorf("argvShape leaks the sentinel: %s", got)
	}
}
