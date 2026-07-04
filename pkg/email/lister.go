package email

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const listPageSize = 100

type Lister struct {
	config   Config
	creds    Credentials
	settings Settings
	client   *jmapClient

	accountID string
}

type emailQueryResponse struct {
	AccountID           string   `json:"accountId"`
	QueryState          string   `json:"queryState"`
	CanCalculateChanges bool     `json:"canCalculateChanges"`
	Position            int      `json:"position"`
	Total               *int     `json:"total"`
	IDs                 []string `json:"ids"`
}

func NewLister(config Config) (*Lister, error) {
	config = normalizeConfig(config)

	logf(config.LogOutput, "loading credentials from %s", config.CredentialsPath)
	creds, err := LoadCredentials(config.CredentialsPath)
	if err != nil {
		return nil, err
	}
	logf(config.LogOutput, "credentials loaded: username_present=%t token_present=%t password_present=%t openai_token_present=%t brave_search_token_present=%t mailbox=%q", creds.Username != "", creds.Token != "", creds.Password != "", creds.OpenAIAPIToken != "", creds.BraveSearchAPIToken != "", creds.Mailbox)

	logf(config.LogOutput, "loading email settings from %s", config.SettingsPath)
	settings, err := LoadSettings(config.SettingsPath)
	if err != nil {
		return nil, err
	}
	logf(config.LogOutput, "settings loaded: jmap_session_endpoint=%s legacy_basic_endpoint=%s", settings.JMAPSessionEndpoint, settings.JMAPLegacySessionEndpoint)

	return &Lister{
		config:   config,
		creds:    creds,
		settings: settings,
		client:   newJMAPClient(creds, config.LogOutput),
	}, nil
}

func (l *Lister) List(ctx context.Context) error {
	l.logf("starting JMAP mailbox listing")
	if err := l.client.FetchSession(ctx, l.settings); err != nil {
		return err
	}

	accountID, err := l.client.AccountID()
	if err != nil {
		return err
	}
	l.accountID = accountID
	l.logf("selected JMAP account: account_id=%s configured_username_present=%t", l.accountID, l.creds.Username != "")

	position := 0
	printed := 0
	for {
		query, messages, err := l.fetchPage(ctx, position)
		if err != nil {
			return err
		}
		l.logf("listed page: position=%d ids=%d messages=%d total_known=%t", position, len(query.IDs), len(messages), query.Total != nil)

		for _, msg := range messages {
			printed++
			fmt.Fprintf(l.config.Output, "%d\t%s\t%s\t%s\n", printed, cleanListField(msg.ReceivedAt), cleanListField(formatFrom(msg.From)), cleanListField(msg.Subject))
		}

		if len(query.IDs) < listPageSize {
			l.logf("mailbox listing complete: printed=%d", printed)
			return nil
		}
		position += len(query.IDs)
	}
}

func (l *Lister) fetchPage(ctx context.Context, position int) (emailQueryResponse, []listedEmailMessage, error) {
	envelope, err := l.client.Call(ctx, []methodCall{
		{"Email/query", map[string]any{
			"accountId":      l.accountID,
			"filter":         map[string]any{},
			"sort":           []map[string]any{{"property": "receivedAt", "isAscending": false}},
			"position":       position,
			"limit":          listPageSize,
			"calculateTotal": true,
		}, "query"},
		{"Email/get", map[string]any{
			"accountId":  l.accountID,
			"#ids":       map[string]string{"resultOf": "query", "name": "Email/query", "path": "/ids"},
			"properties": []string{"id", "from", "subject", "receivedAt"},
		}, "messages"},
	})
	if err != nil {
		return emailQueryResponse{}, nil, err
	}

	var query emailQueryResponse
	var messages listedEmailGetResponse
	for _, response := range envelope.MethodResponses {
		name, args, err := decodeMethodResponse(response)
		if err != nil {
			return emailQueryResponse{}, nil, err
		}
		switch name {
		case "Email/query":
			if err := json.Unmarshal(args, &query); err != nil {
				return emailQueryResponse{}, nil, err
			}
			if query.Total != nil {
				l.logf("Email/query response: position=%d ids=%d total=%d query_state=%s", query.Position, len(query.IDs), *query.Total, query.QueryState)
			} else {
				l.logf("Email/query response: position=%d ids=%d total=<unknown> query_state=%s", query.Position, len(query.IDs), query.QueryState)
			}
		case "Email/get":
			if err := json.Unmarshal(args, &messages); err != nil {
				return emailQueryResponse{}, nil, err
			}
			l.logf("Email/get response for list page: fetched=%d not_found=%d", len(messages.List), len(messages.NotFound))
		case "error":
			return emailQueryResponse{}, nil, fmt.Errorf("JMAP list error: %s", string(args))
		}
	}

	return query, messages.List, nil
}

func (l *Lister) logf(format string, args ...any) {
	logf(l.config.LogOutput, format, args...)
}

type listedEmailGetResponse struct {
	AccountID string               `json:"accountId"`
	State     string               `json:"state"`
	List      []listedEmailMessage `json:"list"`
	NotFound  []string             `json:"notFound"`
}

type listedEmailMessage struct {
	ID         string         `json:"id"`
	From       []emailAddress `json:"from"`
	Subject    string         `json:"subject"`
	ReceivedAt string         `json:"receivedAt"`
}

func cleanListField(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\t", " ")
	return strings.Join(strings.Fields(value), " ")
}

func normalizeConfig(config Config) Config {
	if config.CredentialsPath == "" {
		config.CredentialsPath = "creds.txt"
	}
	if config.SettingsPath == "" {
		config.SettingsPath = "EmailSettings.md"
	}
	if config.DatabasePath == "" {
		config.DatabasePath = filepath.Join(".tmp", "correspondents.sqlite3")
	}
	if config.Output == nil {
		config.Output = os.Stdout
	}
	if config.LogOutput == nil {
		config.LogOutput = os.Stderr
	}
	return config
}
