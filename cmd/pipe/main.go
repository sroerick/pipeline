package main

import (
	"context"
	"fmt"
	"os"

	"pipe/internal/cli"
)

func main() {
	if err := cli.Run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "pipe:", err)
		os.Exit(1)
	}
}
