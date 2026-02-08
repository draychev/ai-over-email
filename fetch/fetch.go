package fetch

import (
	"crypto/tls"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

type Config struct {
	Server   string
	Username string
	Password string
	Mailbox  string
}

type Email struct {
	UID          uint32    `json:"uid"`
	InternalDate time.Time `json:"internal_date"`
	From         string    `json:"from"`
	Subject      string    `json:"subject"`
	Body         string    `json:"body"`
}

func Recent(n int, cfg Config) ([]Email, error) {
	if cfg.Server == "" || cfg.Username == "" || cfg.Password == "" {
		return nil, fmt.Errorf("SERVER, USERNAME, and PASSWORD must be set")
	}
	if n <= 0 {
		return nil, fmt.Errorf("n must be positive")
	}
	mailbox := cfg.Mailbox
	if mailbox == "" {
		mailbox = "INBOX"
	}

	addr := cfg.Server
	if !strings.Contains(cfg.Server, ":") {
		addr = cfg.Server + ":993"
	}

	c, err := client.DialTLS(addr, &tls.Config{ServerName: cfg.Server})
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}
	defer c.Logout()

	if err := c.Login(cfg.Username, cfg.Password); err != nil {
		return nil, fmt.Errorf("login failed: %w", err)
	}

	mboxStatus, err := c.Select(mailbox, true)
	if err != nil {
		return nil, fmt.Errorf("select mailbox failed: %w", err)
	}

	if mboxStatus.Messages == 0 {
		return []Email{}, nil
	}

	start := uint32(1)
	if uint32(n) < mboxStatus.Messages {
		start = mboxStatus.Messages - uint32(n) + 1
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

	messages := make(chan *imap.Message, n)
	if err := c.Fetch(seqset, items, messages); err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}

	results := make([]Email, 0, n)
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

		results = append(results, Email{
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

	if len(results) > n {
		results = results[:n]
	}

	return results, nil
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
