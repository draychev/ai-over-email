package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"ai-over-email/pkg/usenet"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	watcher, err := usenet.NewWatcher(usenet.Config{
		EnvPath:    ".env",
		ConfigPath: "config.json",
		Output:     os.Stdout,
		LogOutput:  os.Stderr,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "usenetwatch: %v\n", err)
		os.Exit(1)
	}

	if err := watcher.Run(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stdout, "Ciao!")
			return
		}
		fmt.Fprintf(os.Stderr, "usenetwatch: %v\n", err)
		os.Exit(1)
	}
}
