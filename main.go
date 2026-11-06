package main

import (
	"errors"
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
	var domain *exitcode.Error
	if errors.As(err, &domain) {
		fmt.Fprint(os.Stderr, domain.Format())
		return domain.Status
	}
	fmt.Fprintf(os.Stderr, "ERROR [%s]: %s\n", exitcode.CodeUsage, err)
	return exitcode.Usage
}
