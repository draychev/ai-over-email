package email

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	appconfig "ai-over-email/pkg/config"
)

const defaultSearchLimit = 25

type Inspector struct {
	config    Config
	creds     Credentials
	appConfig appconfig.ConfigStruct
	client    *jmapClient

	accountID  string
	mailboxes  []mailbox
	mailboxIDs map[string]string
}

type MailboxInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
}

type SearchOptions struct {
	MailboxName    string
	From           string
	Subject        string
	Text           string
	ReceivedAfter  string
	ReceivedBefore string
	Unread         bool
	Limit          int
}

type MessageSummary struct {
	ID         string         `json:"id"`
	From       []emailAddress `json:"from,omitempty"`
	To         []emailAddress `json:"to,omitempty"`
	Subject    string         `json:"subject,omitempty"`
	ReceivedAt string         `json:"receivedAt,omitempty"`
	SentAt     string         `json:"sentAt,omitempty"`
	MailboxIDs []string       `json:"mailboxIds,omitempty"`
	Preview    string         `json:"preview,omitempty"`
}

type MessageDetail struct {
	ID          string          `json:"id"`
	BlobID      string          `json:"blobId,omitempty"`
	From        []emailAddress  `json:"from,omitempty"`
	To          []emailAddress  `json:"to,omitempty"`
	Subject     string          `json:"subject,omitempty"`
	SentAt      string          `json:"sentAt,omitempty"`
	ReceivedAt  string          `json:"receivedAt,omitempty"`
	MessageID   []string        `json:"messageId,omitempty"`
	References  []string        `json:"references,omitempty"`
	MailboxIDs  []string        `json:"mailboxIds,omitempty"`
	TextBody    string          `json:"textBody,omitempty"`
	HTMLBody    string          `json:"htmlBody,omitempty"`
	Attachments []AttachmentRef `json:"attachments,omitempty"`
	RawRFC822   string          `json:"rawRfc822,omitempty"`
}

type AttachmentRef struct {
	Name        string `json:"name,omitempty"`
	Type        string `json:"type,omitempty"`
	Size        int    `json:"size,omitempty"`
	BlobID      string `json:"blobId,omitempty"`
	Disposition string `json:"disposition,omitempty"`
}

func NewInspector(config Config) (*Inspector, error) {
	config = normalizeConfig(config)

	logf(config.LogOutput, "loading credentials from environment with optional env file %s", config.EnvPath)
	creds, err := LoadCredentials(config.EnvPath)
	if err != nil {
		return nil, err
	}

	logf(config.LogOutput, "loading application config from %s", config.ConfigPath)
	appConfig, err := appconfig.Load(config.ConfigPath)
	if err != nil {
		return nil, err
	}

	return &Inspector{
		config:    config,
		creds:     creds,
		appConfig: appConfig,
		client:    newJMAPClient(creds, config.LogOutput),
	}, nil
}

func (i *Inspector) ListMailboxes(ctx context.Context) ([]MailboxInfo, error) {
	if err := i.ensureReady(ctx); err != nil {
		return nil, err
	}

	result := make([]MailboxInfo, 0, len(i.mailboxes))
	for _, box := range i.mailboxes {
		result = append(result, MailboxInfo{ID: box.ID, Name: box.Name, Role: box.Role})
	}
	return result, nil
}

func (i *Inspector) SearchMessages(ctx context.Context, opts SearchOptions) ([]MessageSummary, error) {
	if err := i.ensureReady(ctx); err != nil {
		return nil, err
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	filter, err := i.buildSearchFilter(opts)
	if err != nil {
		return nil, err
	}

	envelope, err := i.client.Call(ctx, []methodCall{
		{"Email/query", map[string]any{
			"accountId": i.accountID,
			"filter":    filter,
			"sort":      []map[string]any{{"property": "receivedAt", "isAscending": false}},
			"limit":     limit,
		}, "query"},
		{"Email/get", map[string]any{
			"accountId":           i.accountID,
			"#ids":                map[string]string{"resultOf": "query", "name": "Email/query", "path": "/ids"},
			"properties":          []string{"id", "from", "to", "subject", "receivedAt", "sentAt", "mailboxIds", "textBody", "bodyValues"},
			"fetchTextBodyValues": true,
			"maxBodyValueBytes":   4096,
		}, "messages"},
	})
	if err != nil {
		return nil, err
	}

	var messages emailGetResponse
	for _, response := range envelope.MethodResponses {
		name, args, err := decodeMethodResponse(response)
		if err != nil {
			return nil, err
		}
		switch name {
		case "Email/get":
			if err := json.Unmarshal(args, &messages); err != nil {
				return nil, err
			}
		case "error":
			return nil, fmt.Errorf("JMAP search error: %s", string(args))
		}
	}

	result := make([]MessageSummary, 0, len(messages.List))
	for _, msg := range messages.List {
		result = append(result, MessageSummary{
			ID:         msg.ID,
			From:       msg.From,
			To:         msg.To,
			Subject:    msg.Subject,
			ReceivedAt: msg.ReceivedAt,
			SentAt:     msg.SentAt,
			MailboxIDs: i.mailboxNamesForIDs(msg.MailboxIDs),
			Preview:    truncatePreview(extractBodyText(msg.TextBody, msg.BodyValues), 280),
		})
	}
	return result, nil
}

func (i *Inspector) GetMessage(ctx context.Context, id string, includeRaw bool) (MessageDetail, error) {
	if err := i.ensureReady(ctx); err != nil {
		return MessageDetail{}, err
	}
	if strings.TrimSpace(id) == "" {
		return MessageDetail{}, fmt.Errorf("message id is required")
	}

	envelope, err := i.client.Call(ctx, []methodCall{
		{"Email/get", map[string]any{
			"accountId":           i.accountID,
			"ids":                 []string{id},
			"properties":          []string{"id", "blobId", "from", "to", "subject", "sentAt", "receivedAt", "mailboxIds", "textBody", "htmlBody", "attachments", "bodyValues", "messageId", "references"},
			"fetchTextBodyValues": true,
			"fetchHTMLBodyValues": true,
			"maxBodyValueBytes":   200000,
		}, "message"},
	})
	if err != nil {
		return MessageDetail{}, err
	}

	for _, response := range envelope.MethodResponses {
		name, args, err := decodeMethodResponse(response)
		if err != nil {
			return MessageDetail{}, err
		}
		switch name {
		case "Email/get":
			var got emailGetResponse
			if err := json.Unmarshal(args, &got); err != nil {
				return MessageDetail{}, err
			}
			if len(got.List) == 0 {
				return MessageDetail{}, fmt.Errorf("message %s not found", id)
			}
			msg := got.List[0]
			detail := MessageDetail{
				ID:          msg.ID,
				BlobID:      msg.BlobID,
				From:        msg.From,
				To:          msg.To,
				Subject:     msg.Subject,
				SentAt:      msg.SentAt,
				ReceivedAt:  msg.ReceivedAt,
				MessageID:   msg.MessageID,
				References:  msg.References,
				MailboxIDs:  i.mailboxNamesForIDs(msg.MailboxIDs),
				TextBody:    extractBodyText(msg.TextBody, msg.BodyValues),
				HTMLBody:    extractBodyText(msg.HTMLBody, msg.BodyValues),
				Attachments: collectAttachmentRefs(msg.Attachments),
			}
			if includeRaw && msg.BlobID != "" {
				raw, err := i.client.Download(ctx, i.accountID, msg.BlobID, "message.eml", "message/rfc822")
				if err != nil {
					return MessageDetail{}, err
				}
				detail.RawRFC822 = string(raw)
			}
			return detail, nil
		case "error":
			return MessageDetail{}, fmt.Errorf("JMAP get message error: %s", string(args))
		}
	}

	return MessageDetail{}, fmt.Errorf("JMAP get message returned no Email/get response")
}

func (i *Inspector) ensureReady(ctx context.Context) error {
	if i.accountID != "" && len(i.mailboxes) > 0 {
		return nil
	}
	if err := i.client.FetchSession(ctx, i.appConfig); err != nil {
		return err
	}
	accountID, err := i.client.AccountID()
	if err != nil {
		return err
	}
	i.accountID = accountID

	envelope, err := i.client.Call(ctx, []methodCall{
		{"Mailbox/get", map[string]any{
			"accountId": i.accountID,
		}, "mailboxes"},
	})
	if err != nil {
		return err
	}

	for _, response := range envelope.MethodResponses {
		name, args, err := decodeMethodResponse(response)
		if err != nil {
			return err
		}
		switch name {
		case "Mailbox/get":
			var mailboxes mailboxGetResponse
			if err := json.Unmarshal(args, &mailboxes); err != nil {
				return err
			}
			i.mailboxes = mailboxes.List
			i.mailboxIDs = make(map[string]string, len(mailboxes.List))
			for _, box := range mailboxes.List {
				i.mailboxIDs[strings.ToLower(strings.TrimSpace(box.Name))] = box.ID
				if box.Role != "" {
					i.mailboxIDs[strings.ToLower(strings.TrimSpace(box.Role))] = box.ID
				}
			}
		case "error":
			return fmt.Errorf("JMAP mailbox lookup error: %s", string(args))
		}
	}
	return nil
}

func (i *Inspector) buildSearchFilter(opts SearchOptions) (map[string]any, error) {
	filter := map[string]any{}
	if mailbox := strings.ToLower(strings.TrimSpace(opts.MailboxName)); mailbox != "" {
		id, ok := i.mailboxIDs[mailbox]
		if !ok {
			return nil, fmt.Errorf("mailbox %q not found", opts.MailboxName)
		}
		filter["inMailbox"] = id
	}
	if value := strings.TrimSpace(opts.From); value != "" {
		filter["from"] = value
	}
	if value := strings.TrimSpace(opts.Subject); value != "" {
		filter["subject"] = value
	}
	if value := strings.TrimSpace(opts.Text); value != "" {
		filter["text"] = value
	}
	if value := strings.TrimSpace(opts.ReceivedAfter); value != "" {
		filter["after"] = value
	}
	if value := strings.TrimSpace(opts.ReceivedBefore); value != "" {
		filter["before"] = value
	}
	if opts.Unread {
		filter["notKeyword"] = "$seen"
	}
	return filter, nil
}

func (i *Inspector) mailboxNamesForIDs(ids map[string]bool) []string {
	if len(ids) == 0 {
		return nil
	}
	byID := make(map[string]string, len(i.mailboxes))
	for _, box := range i.mailboxes {
		byID[box.ID] = box.Name
	}
	names := make([]string, 0, len(ids))
	for id, enabled := range ids {
		if !enabled {
			continue
		}
		if name := byID[id]; name != "" {
			names = append(names, name)
		} else {
			names = append(names, id)
		}
	}
	slices.Sort(names)
	return names
}

func extractBodyText(parts []emailBodyPart, values map[string]emailBodyValue) string {
	if len(parts) == 0 || len(values) == 0 {
		return ""
	}
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(values[part.PartID].Value)
		if value == "" {
			continue
		}
		segments = append(segments, value)
	}
	return strings.TrimSpace(strings.Join(segments, "\n\n"))
}

func collectAttachmentRefs(parts []emailBodyPart) []AttachmentRef {
	if len(parts) == 0 {
		return nil
	}
	result := make([]AttachmentRef, 0, len(parts))
	for _, part := range parts {
		result = append(result, AttachmentRef{
			Name:        part.Name,
			Type:        part.Type,
			Size:        part.Size,
			BlobID:      part.BlobID,
			Disposition: part.Disposition,
		})
	}
	return result
}

func truncatePreview(value string, max int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 1 {
		return value[:max]
	}
	return value[:max-1] + "…"
}
