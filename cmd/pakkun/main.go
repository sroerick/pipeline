package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"pakkun/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cli.Run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "pakkun:", err)
		os.Exit(1)
	}
}
