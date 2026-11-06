package main

import (
	"fmt"
	"os"

	"github.com/colligo-go/scribo/cmd"
	"github.com/colligo-go/scribo/internal/exitcode"
)

func main() {
	os.Exit(run())
}

func run() int {
	err := cmd.Execute()
	if err == nil {
		return exitcode.OK
	}
	fmt.Fprint(os.Stderr, err.Format())
	return err.Status
}
