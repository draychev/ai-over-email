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
	if n <= 0 {
		return nil, fmt.Errorf("n must be positive")
	}

	results, err := listRange(cfg, rangeRecent(n))
	if err != nil {
		return nil, err
	}

	if len(results) > n {
		results = results[:n]
	}

	return results, nil
}

func ListAll(cfg Config) ([]Email, error) {
	return listRange(cfg, rangeAll())
}

func Search(query string, limit int, cfg Config) ([]Email, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query must be non-empty")
	}

	results, err := ListAll(cfg)
	if err != nil {
		return nil, err
	}

	terms := strings.Fields(strings.ToLower(query))
	matches := make([]Email, 0, len(results))
	for _, msg := range results {
		if matchEmail(msg, terms) {
			matches = append(matches, msg)
		}
	}

	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}

	return matches, nil
}

func MoveToMailbox(uids []uint32, dest string, cfg Config) (int, error) {
	if len(uids) == 0 {
		return 0, nil
	}
	if dest == "" {
		return 0, fmt.Errorf("destination mailbox must be set")
	}

	client, status, err := connect(cfg, false)
	if err != nil {
		return 0, err
	}
	defer client.Logout()

	if status.Messages == 0 {
		return 0, nil
	}

	if err := client.Create(dest); err != nil {
		// ignore if mailbox already exists
	}

	seqset := new(imap.SeqSet)
	for _, uid := range uids {
		if uid > 0 {
			seqset.AddNum(uid)
		}
	}

	if err := client.UidMove(seqset, dest); err != nil {
		return 0, fmt.Errorf("move failed: %w", err)
	}

	return len(uids), nil
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

type rangeSelector func(total uint32) (start uint32, end uint32, limit int)

func rangeRecent(n int) rangeSelector {
	return func(total uint32) (uint32, uint32, int) {
		start := uint32(1)
		if uint32(n) < total {
			start = total - uint32(n) + 1
		}
		return start, total, n
	}
}

func rangeAll() rangeSelector {
	return func(total uint32) (uint32, uint32, int) {
		return 1, total, int(total)
	}
}

func listRange(cfg Config, selector rangeSelector) ([]Email, error) {
	if cfg.Server == "" || cfg.Username == "" || cfg.Password == "" {
		return nil, fmt.Errorf("SERVER, USERNAME, and PASSWORD must be set")
	}

	client, status, err := connect(cfg, true)
	if err != nil {
		return nil, err
	}
	defer client.Logout()

	if status.Messages == 0 {
		return []Email{}, nil
	}

	start, end, limit := selector(status.Messages)
	if start < 1 {
		start = 1
	}
	if end == 0 || end > status.Messages {
		end = status.Messages
	}
	if start > end {
		return []Email{}, nil
	}

	seqset := new(imap.SeqSet)
	seqset.AddRange(start, end)

	bodySection := &imap.BodySectionName{
		BodyPartName: imap.BodyPartName{Specifier: imap.TextSpecifier},
	}
	items := []imap.FetchItem{
		imap.FetchEnvelope,
		imap.FetchInternalDate,
		imap.FetchUid,
		bodySection.FetchItem(),
	}

	messages := make(chan *imap.Message, limit)
	if err := client.Fetch(seqset, items, messages); err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}

	results := make([]Email, 0, limit)
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

	return results, nil
}

func connect(cfg Config, readOnly bool) (*client.Client, *imap.MailboxStatus, error) {
	mailbox := cfg.Mailbox
	if mailbox == "" {
		mailbox = "INBOX"
	}

	addr := cfg.Server
	if !strings.Contains(cfg.Server, ":") {
		addr = cfg.Server + ":993"
	}

	client, err := client.DialTLS(addr, &tls.Config{ServerName: cfg.Server})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect: %w", err)
	}

	if err := client.Login(cfg.Username, cfg.Password); err != nil {
		_ = client.Logout()
		return nil, nil, fmt.Errorf("login failed: %w", err)
	}

	status, err := client.Select(mailbox, readOnly)
	if err != nil {
		_ = client.Logout()
		return nil, nil, fmt.Errorf("select mailbox failed: %w", err)
	}

	return client, status, nil
}

func matchEmail(msg Email, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	haystack := strings.ToLower(msg.From + " " + msg.Subject + " " + msg.Body)
	for _, term := range terms {
		if !strings.Contains(haystack, term) {
			return false
		}
	}
	return true
}
