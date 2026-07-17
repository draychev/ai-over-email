package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"ai-over-email/pkg/mcpserver"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := mcpserver.RunStdio(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "fastmail-mcp: %v\n", err)
		os.Exit(1)
	}
}
