// Command flagctl manages feature flags through the flagd HTTP API.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ahmedr1zwan/flagctl/internal/cli"
)

// Set by GoReleaser; source builds identify themselves as development builds.
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	command := cli.NewCommand(os.Stdout, os.Stderr)
	command.Version = version
	if err := command.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
