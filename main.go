package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"ai-over-email/fetch"
	"ai-over-email/internal/config"
	"ai-over-email/send"
)

func main() {
	var (
		n        = flag.Int("n", 10, "number of most recent emails to return")
		env      = flag.String("env", ".env", "path to env file")
		mbox     = flag.String("mailbox", "INBOX", "mailbox to read")
		sendMode = flag.Bool("send", false, "send an email instead of fetching")
		to       = flag.String("to", "", "recipient email address")
		subject  = flag.String("subject", "", "email subject")
		body     = flag.String("body", "", "email body")
		quiet    = flag.Bool("quiet", false, "suppress non-JSON output")
	)
	flag.Parse()

	configMap, err := config.Load(*env)
	if err != nil {
		exitErr(err.Error(), *quiet)
	}

	if *sendMode {
		if *to == "" || *subject == "" || *body == "" {
			exitErr("-to, -subject, and -body are required in send mode", *quiet)
		}
		sendCfg := send.Config{
			Server:   config.Value(configMap, "SMTP_SERVER", "smtp.gmail.com"),
			Port:     config.Value(configMap, "SMTP_PORT", "587"),
			Username: configMap["USERNAME"],
			Password: configMap["PASSWORD"],
			From:     config.Value(configMap, "FROM_EMAIL", ""),
		}
		if err := send.Send(*to, *subject, *body, sendCfg); err != nil {
			exitErr(fmt.Sprintf("send failed: %v", err), *quiet)
		}
		return
	}

	if *n <= 0 {
		exitErr("-n must be positive", *quiet)
	}

	fetchCfg := fetch.Config{
		Server:   configMap["SERVER"],
		Username: configMap["USERNAME"],
		Password: configMap["PASSWORD"],
		Mailbox:  *mbox,
	}

	results, err := fetch.Recent(*n, fetchCfg)
	if err != nil {
		exitErr(err.Error(), *quiet)
	}
	writeJSON(results)
}

func writeJSON(payload interface{}) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(payload)
}

func exitErr(msg string, quiet bool) {
	if !quiet {
		fmt.Fprintln(os.Stderr, msg)
	}
	os.Exit(1)
}
