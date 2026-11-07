package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/colligo-go/scribo/cmd"
	"github.com/colligo-go/scribo/internal/exitcode"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()

	err := cmd.Execute(ctx)
	if err == nil {
		return exitcode.OK
	}
	fmt.Fprint(os.Stderr, err.Format())
	return err.Status
}
