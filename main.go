package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ai-over-email/fetch"
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

	config, err := loadConfig(*env)
	if err != nil {
		exitErr(err.Error(), *quiet)
	}

	if *sendMode {
		if *to == "" || *subject == "" || *body == "" {
			exitErr("-to, -subject, and -body are required in send mode", *quiet)
		}
		sendCfg := send.Config{
			Server:   configValue(config, "SMTP_SERVER", "smtp.gmail.com"),
			Port:     configValue(config, "SMTP_PORT", "587"),
			Username: config["USERNAME"],
			Password: config["PASSWORD"],
			From:     configValue(config, "FROM_EMAIL", ""),
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
		Server:   config["SERVER"],
		Username: config["USERNAME"],
		Password: config["PASSWORD"],
		Mailbox:  *mbox,
	}

	results, err := fetch.Recent(*n, fetchCfg)
	if err != nil {
		exitErr(err.Error(), *quiet)
	}
	writeJSON(results)
}

func loadConfig(envPath string) (map[string]string, error) {
	config := map[string]string{}

	if envPath != "" {
		path := envPath
		if !filepath.IsAbs(path) {
			if cwd, err := os.Getwd(); err == nil {
				path = filepath.Join(cwd, envPath)
			}
		}
		if data, err := os.ReadFile(path); err == nil {
			parsed := parseEnv(string(data))
			for k, v := range parsed {
				config[k] = v
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("failed to read env file: %w", err)
		}
	}

	for _, key := range []string{"SERVER", "USERNAME", "PASSWORD", "SSL"} {
		if val := os.Getenv(key); val != "" {
			config[key] = val
		}
	}

	return config, nil
}

func parseEnv(content string) map[string]string {
	result := map[string]string{}
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "="); idx != -1 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			val = strings.TrimSuffix(val, ",")
			val = strings.TrimSpace(val)
			val = strings.Trim(val, "\"'")
			if key != "" {
				result[key] = val
			}
		}
	}
	return result
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

func configValue(config map[string]string, key, fallback string) string {
	if val := config[key]; val != "" {
		return val
	}
	return fallback
}
