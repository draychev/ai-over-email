package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

type emailItem struct {
	UID          uint32    `json:"uid"`
	InternalDate time.Time `json:"internal_date"`
	From         string    `json:"from"`
	Subject      string    `json:"subject"`
	Body         string    `json:"body"`
}

func main() {
	var (
		n     = flag.Int("n", 10, "number of most recent emails to return")
		env   = flag.String("env", ".env", "path to env file")
		mbox  = flag.String("mailbox", "INBOX", "mailbox to read")
		quiet = flag.Bool("quiet", false, "suppress non-JSON output")
	)
	flag.Parse()

	if *n <= 0 {
		exitErr("-n must be positive", *quiet)
	}

	config, err := loadConfig(*env)
	if err != nil {
		exitErr(err.Error(), *quiet)
	}

	server := config["SERVER"]
	username := config["USERNAME"]
	password := config["PASSWORD"]

	if server == "" || username == "" || password == "" {
		exitErr("SERVER, USERNAME, and PASSWORD must be set in .env or environment", *quiet)
	}

	addr := server
	if !strings.Contains(server, ":") {
		addr = server + ":993"
	}

	c, err := client.DialTLS(addr, &tls.Config{ServerName: server})
	if err != nil {
		exitErr(fmt.Sprintf("failed to connect: %v", err), *quiet)
	}
	defer c.Logout()

	if err := c.Login(username, password); err != nil {
		exitErr(fmt.Sprintf("login failed: %v", err), *quiet)
	}

	mboxStatus, err := c.Select(*mbox, true)
	if err != nil {
		exitErr(fmt.Sprintf("select mailbox failed: %v", err), *quiet)
	}

	if mboxStatus.Messages == 0 {
		writeJSON([]emailItem{})
		return
	}

	start := uint32(1)
	if uint32(*n) < mboxStatus.Messages {
		start = mboxStatus.Messages - uint32(*n) + 1
	}

	seqset := new(imap.SeqSet)
	seqset.AddRange(start, mboxStatus.Messages)

	bodySection := &imap.BodySectionName{Specifier: imap.TextSpecifier}
	items := []imap.FetchItem{
		imap.FetchEnvelope,
		imap.FetchInternalDate,
		imap.FetchUid,
		bodySection.FetchItem(),
	}
	messages := make(chan *imap.Message, *n)
	if err := c.Fetch(seqset, items, messages); err != nil {
		exitErr(fmt.Sprintf("fetch failed: %v", err), *quiet)
	}

	results := make([]emailItem, 0, *n)
	for msg := range messages {
		if msg == nil || msg.Envelope == nil {
			continue
		}
		from := formatAddressList(msg.Envelope.From)
		body := ""
		if r := msg.GetBody(bodySection); r != nil {
			if data, err := io.ReadAll(r); err == nil {
				body = strings.TrimSpace(string(data))
			}
		}

		results = append(results, emailItem{
			UID:          msg.Uid,
			InternalDate: msg.InternalDate,
			From:         from,
			Subject:      msg.Envelope.Subject,
			Body:         body,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].InternalDate.After(results[j].InternalDate)
	})

	if len(results) > *n {
		results = results[:*n]
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

func formatAddressList(list []*imap.Address) string {
	parts := make([]string, 0, len(list))
	for _, addr := range list {
		if addr == nil {
			continue
		}
		mailbox := addr.MailboxName
		host := addr.HostName
		full := mailbox
		if host != "" {
			full = mailbox + "@" + host
		}
		if addr.PersonalName != "" {
			parts = append(parts, fmt.Sprintf("%s <%s>", addr.PersonalName, full))
		} else {
			parts = append(parts, full)
		}
	}
	return strings.Join(parts, ", ")
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
