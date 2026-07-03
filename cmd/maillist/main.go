package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"ai-over-email/pkg/email"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	lister, err := email.NewLister(email.Config{
		CredentialsPath: "creds.txt",
		SettingsPath:    "EmailSettings.md",
		Output:          os.Stdout,
		LogOutput:       os.Stderr,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "maillist: %v\n", err)
		os.Exit(1)
	}

	if err := lister.List(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "maillist: %v\n", err)
		os.Exit(1)
	}
}
