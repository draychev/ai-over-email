package email

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	appconfig "ai-over-email/pkg/config"
)

const (
	inboxSafetyScanInterval = 5 * time.Minute
	inboxSafetyScanLimit    = 50
	dailyMessageLimit       = 10
)

type Config struct {
	EnvPath      string
	ConfigPath   string
	DatabasePath string
	Output       io.Writer
	LogOutput    io.Writer
}

type Watcher struct {
	config    Config
	creds     Credentials
	appConfig appconfig.ConfigStruct
	client    *jmapClient
	openai    *openAIClient
	store     *correspondentStore

	accountID  string
	inboxID    string
	draftsID   string
	identityID string
	emailState string
	seen       map[string]struct{}
	mu         sync.Mutex
}

type mailbox struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type emailMessage struct {
	ID          string                    `json:"id"`
	BlobID      string                    `json:"blobId"`
	From        []emailAddress            `json:"from"`
	To          []emailAddress            `json:"to"`
	Subject     string                    `json:"subject"`
	SentAt      string                    `json:"sentAt"`
	ReceivedAt  string                    `json:"receivedAt"`
	MailboxIDs  map[string]bool           `json:"mailboxIds"`
	TextBody    []emailBodyPart           `json:"textBody"`
	HTMLBody    []emailBodyPart           `json:"htmlBody"`
	Attachments []emailBodyPart           `json:"attachments"`
	BodyValues  map[string]emailBodyValue `json:"bodyValues"`
	MessageID   []string                  `json:"messageId"`
	References  []string                  `json:"references"`
	Raw         []byte                    `json:"-"`
}

type emailAddress struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type emailBodyPart struct {
	PartID      string `json:"partId"`
	BlobID      string `json:"blobId"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Size        int    `json:"size"`
	Disposition string `json:"disposition"`
}

type emailBodyValue struct {
	Value string `json:"value"`
}

type emailAttachment struct {
	Name   string
	Type   string
	Data   []byte
	BlobID string
	Size   int
}

type stateChange struct {
	Type    string                       `json:"@type"`
	Changed map[string]map[string]string `json:"changed"`
}

type emailGetResponse struct {
	AccountID string         `json:"accountId"`
	State     string         `json:"state"`
	List      []emailMessage `json:"list"`
	NotFound  []string       `json:"notFound"`
}

type emailChangesResponse struct {
	AccountID      string   `json:"accountId"`
	OldState       string   `json:"oldState"`
	NewState       string   `json:"newState"`
	HasMoreChanges bool     `json:"hasMoreChanges"`
	Created        []string `json:"created"`
	Updated        []string `json:"updated"`
	Destroyed      []string `json:"destroyed"`
}

type mailboxGetResponse struct {
	AccountID string    `json:"accountId"`
	State     string    `json:"state"`
	List      []mailbox `json:"list"`
}

type identity struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type identityGetResponse struct {
	AccountID string     `json:"accountId"`
	State     string     `json:"state"`
	List      []identity `json:"list"`
}

func NewWatcher(config Config) (*Watcher, error) {
	config = normalizeConfig(config)

	logf(config.LogOutput, "loading credentials from environment with optional env file %s", config.EnvPath)
	creds, err := LoadCredentials(config.EnvPath)
	if err != nil {
		return nil, err
	}
	logf(config.LogOutput, "credentials loaded: username_present=%t token_present=%t password_present=%t openai_token_present=%t brave_search_token_present=%t mailbox=%q", creds.Username != "", creds.Token != "", creds.Password != "", creds.OpenAIAPIToken != "", creds.BraveSearchAPIToken != "", creds.Mailbox)

	logf(config.LogOutput, "loading application config from %s", config.ConfigPath)
	appConfig, err := appconfig.Load(config.ConfigPath)
	if err != nil {
		return nil, err
	}
	logf(config.LogOutput, "application config loaded: jmap_session_endpoint=%s legacy_basic_endpoint=%s", appConfig.JMAP.SessionEndpoint, appConfig.JMAP.LegacyBasicAuthSessionEndpoint)

	store, err := openCorrespondentStore(config.DatabasePath)
	if err != nil {
		return nil, err
	}
	logf(config.LogOutput, "correspondent database opened: path=%s", config.DatabasePath)

	return &Watcher{
		config:    config,
		creds:     creds,
		appConfig: appConfig,
		client:    newJMAPClient(creds, config.LogOutput),
		openai:    newOpenAIClient(creds.OpenAIAPIToken, creds.PublicEmail, creds.BraveSearchAPIToken, config.LogOutput),
		store:     store,
		seen:      make(map[string]struct{}),
	}, nil
}

func (w *Watcher) Run(ctx context.Context) error {
	w.logf("starting JMAP mailbox watcher")
	if err := w.client.FetchSession(ctx, w.appConfig); err != nil {
		return err
	}

	accountID, err := w.client.AccountID()
	if err != nil {
		return err
	}
	w.accountID = accountID
	w.logf("selected JMAP account: account_id=%s", w.accountID)

	if err := w.initialize(ctx); err != nil {
		return err
	}

	go w.runInboxSafetyScanner(ctx)

	w.logf("initialization complete; listening for mailbox changes")
	return w.listen(ctx)
}

func (w *Watcher) initialize(ctx context.Context) error {
	w.logf("initializing mailbox and email state: desired_mailbox=%q", w.creds.Mailbox)
	envelope, err := w.client.Call(ctx, []methodCall{
		{"Mailbox/get", map[string]any{
			"accountId":  w.accountID,
			"properties": []string{"id", "name", "role"},
		}, "mailboxes"},
		{"Identity/get", map[string]any{
			"accountId":  w.accountID,
			"properties": []string{"id", "email", "name"},
		}, "identities"},
		{"Email/query", map[string]any{
			"accountId": w.accountID,
			"filter":    map[string]any{},
			"sort":      []map[string]any{{"property": "receivedAt", "isAscending": false}},
			"limit":     1,
		}, "query"},
		{"Email/get", map[string]any{
			"accountId":  w.accountID,
			"#ids":       map[string]string{"resultOf": "query", "name": "Email/query", "path": "/ids"},
			"properties": []string{"id"},
		}, "state"},
	})
	if err != nil {
		return err
	}

	for _, response := range envelope.MethodResponses {
		name, args, err := decodeMethodResponse(response)
		if err != nil {
			return err
		}
		w.logf("initialization response received: method=%s bytes=%d", name, len(args))

		switch name {
		case "Mailbox/get":
			var mailboxes mailboxGetResponse
			if err := json.Unmarshal(args, &mailboxes); err != nil {
				return err
			}
			w.inboxID = selectMailboxID(mailboxes.List, w.creds.Mailbox)
			w.draftsID = selectMailboxID(mailboxes.List, "drafts")
			w.logf("mailboxes loaded: count=%d selected_mailbox_id=%s drafts_mailbox_id=%s", len(mailboxes.List), w.inboxID, w.draftsID)
		case "Identity/get":
			var identities identityGetResponse
			if err := json.Unmarshal(args, &identities); err != nil {
				return err
			}
			w.identityID = selectIdentityID(identities.List, w.creds.Username)
			w.logf("identities loaded: count=%d selected_identity_id=%s", len(identities.List), w.identityID)
		case "Email/get":
			var emails emailGetResponse
			if err := json.Unmarshal(args, &emails); err != nil {
				return err
			}
			w.emailState = emails.State
			w.logf("email state initialized: state=%s baseline_messages=%d", w.emailState, len(emails.List))
		case "error":
			return fmt.Errorf("JMAP initialization error: %s", string(args))
		}
	}

	if w.inboxID == "" {
		return fmt.Errorf("mailbox %q not found", w.creds.Mailbox)
	}
	if w.emailState == "" {
		return fmt.Errorf("could not initialize email state")
	}
	return nil
}

func (w *Watcher) runInboxSafetyScanner(ctx context.Context) {
	w.logf("starting inbox safety scanner: interval=%s limit=%d", inboxSafetyScanInterval, inboxSafetyScanLimit)
	if err := w.scanInbox(ctx, "startup"); err != nil && ctx.Err() == nil {
		w.logf("inbox safety scan failed: reason=startup err=%v", err)
	}

	ticker := time.NewTicker(inboxSafetyScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logf("inbox safety scanner stopped")
			return
		case <-ticker.C:
			if err := w.scanInbox(ctx, "periodic"); err != nil && ctx.Err() == nil {
				w.logf("inbox safety scan failed: reason=periodic err=%v", err)
			}
		}
	}
}

func (w *Watcher) listen(ctx context.Context) error {
	var lastEventID string
	for {
		if err := w.listenOnce(ctx, &lastEventID); err != nil {
			if ctx.Err() != nil {
				w.logf("watcher context canceled")
				return ctx.Err()
			}
			w.logf("event stream disconnected: err=%v reconnect_delay=500ms", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
}

func (w *Watcher) listenOnce(ctx context.Context, lastEventID *string) error {
	req, err := w.client.NewEventSourceRequest(ctx, *lastEventID)
	if err != nil {
		return err
	}
	w.logf("connecting to JMAP EventSource: url=%s last_event_id_present=%t", req.URL.Redacted(), *lastEventID != "")

	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	w.logf("EventSource response received: status=%s content_type=%s", resp.Status, resp.Header.Get("Content-Type"))

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("event source: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)

	var eventName string
	var eventID string
	var data strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if data.Len() > 0 {
				if eventID != "" {
					*lastEventID = eventID
					w.logf("EventSource event id updated: id=%s", eventID)
				}
				w.logf("EventSource event received: event=%q data_bytes=%d", eventName, data.Len())
				if eventName == "state" || eventName == "" {
					if err := w.handleState(ctx, data.String()); err != nil {
						return err
					}
				}
			}
			eventName = ""
			eventID = ""
			data.Reset()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if ok {
			value = strings.TrimPrefix(value, " ")
		}
		switch field {
		case "event":
			eventName = value
		case "id":
			eventID = value
		case "data":
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.EOF
}

func (w *Watcher) handleState(ctx context.Context, raw string) error {
	var change stateChange
	if err := json.Unmarshal([]byte(raw), &change); err != nil {
		return err
	}
	w.logf("state change received: type=%s account_count=%d", change.Type, len(change.Changed))

	if accountChange := change.Changed[w.accountID]; accountChange["Email"] != "" {
		w.logf("email state changed: old_state=%s pushed_state=%s", w.emailState, accountChange["Email"])
		return w.syncEmailChanges(ctx)
	}
	w.logf("state change ignored: no Email change for selected account")
	return nil
}

func (w *Watcher) syncEmailChanges(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.syncEmailChangesLocked(ctx)
}

func (w *Watcher) syncEmailChangesLocked(ctx context.Context) error {
	for {
		w.logf("syncing email changes: since_state=%s", w.emailState)
		envelope, err := w.client.Call(ctx, []methodCall{
			{"Email/changes", map[string]any{
				"accountId":  w.accountID,
				"sinceState": w.emailState,
				"maxChanges": 256,
			}, "changes"},
			{"Email/get", map[string]any{
				"accountId":  w.accountID,
				"#ids":       map[string]string{"resultOf": "changes", "name": "Email/changes", "path": "/created"},
				"properties": []string{"id", "from", "to", "subject", "mailboxIds"},
			}, "created"},
		})
		if err != nil {
			return err
		}

		var changes emailChangesResponse
		var created emailGetResponse
		for _, response := range envelope.MethodResponses {
			name, args, err := decodeMethodResponse(response)
			if err != nil {
				return err
			}
			switch name {
			case "Email/changes":
				if err := json.Unmarshal(args, &changes); err != nil {
					return err
				}
				w.logf("Email/changes response: created=%d updated=%d destroyed=%d has_more=%t new_state=%s", len(changes.Created), len(changes.Updated), len(changes.Destroyed), changes.HasMoreChanges, changes.NewState)
			case "Email/get":
				if err := json.Unmarshal(args, &created); err != nil {
					return err
				}
				w.logf("Email/get response for created messages: fetched=%d not_found=%d", len(created.List), len(created.NotFound))
			case "error":
				return fmt.Errorf("JMAP sync error: %s", string(args))
			}
		}

		for _, msg := range created.List {
			if !msg.MailboxIDs[w.inboxID] {
				w.logf("created message ignored outside watched mailbox: id=%s", msg.ID)
				continue
			}
			if _, ok := w.seen[msg.ID]; ok {
				w.logf("created message already seen: id=%s", msg.ID)
				continue
			}
			w.seen[msg.ID] = struct{}{}
			if reason := w.skipAutoReplyReason(msg); reason != "" {
				w.handleAutoReplyGuard(ctx, msg, reason, "event")
				continue
			}
			w.logf("new watched message: id=%s from=%q subject=%q", msg.ID, formatFrom(msg.From), msg.Subject)
			fmt.Fprintf(w.config.Output, "FROM: %s\tSUBJECT: %s\n", formatFrom(msg.From), msg.Subject)
			if err := w.maybeAutoReply(ctx, msg); err != nil {
				w.logf("auto-reply failed: id=%s err=%v", msg.ID, err)
			}
		}

		if changes.NewState != "" {
			w.emailState = changes.NewState
			w.logf("email state advanced: state=%s", w.emailState)
		}
		if !changes.HasMoreChanges {
			w.logf("email sync complete")
			return nil
		}
		w.logf("more email changes available; continuing sync")
	}
}

func (w *Watcher) scanInbox(ctx context.Context, reason string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.logf("running inbox safety scan: reason=%s mailbox_id=%s limit=%d", reason, w.inboxID, inboxSafetyScanLimit)
	envelope, err := w.client.Call(ctx, []methodCall{
		{"Email/query", map[string]any{
			"accountId": w.accountID,
			"filter":    map[string]any{"inMailbox": w.inboxID},
			"sort":      []map[string]any{{"property": "receivedAt", "isAscending": true}},
			"limit":     inboxSafetyScanLimit,
		}, "query"},
		{"Email/get", map[string]any{
			"accountId":  w.accountID,
			"#ids":       map[string]string{"resultOf": "query", "name": "Email/query", "path": "/ids"},
			"properties": []string{"id", "from", "to", "subject", "mailboxIds"},
		}, "messages"},
	})
	if err != nil {
		return err
	}

	var query emailQueryResponse
	var messages emailGetResponse
	for _, response := range envelope.MethodResponses {
		name, args, err := decodeMethodResponse(response)
		if err != nil {
			return err
		}
		switch name {
		case "Email/query":
			if err := json.Unmarshal(args, &query); err != nil {
				return err
			}
			w.logf("inbox safety scan query response: ids=%d query_state=%s", len(query.IDs), query.QueryState)
		case "Email/get":
			if err := json.Unmarshal(args, &messages); err != nil {
				return err
			}
			w.logf("inbox safety scan get response: fetched=%d not_found=%d", len(messages.List), len(messages.NotFound))
		case "error":
			return fmt.Errorf("JMAP inbox safety scan error: %s", string(args))
		}
	}

	attempted := 0
	for _, msg := range messages.List {
		if !msg.MailboxIDs[w.inboxID] {
			w.logf("inbox safety scan ignored message outside watched mailbox: id=%s", msg.ID)
			continue
		}
		if _, ok := w.seen[msg.ID]; ok {
			w.logf("inbox safety scan skipped already attempted message: id=%s", msg.ID)
			continue
		}
		w.seen[msg.ID] = struct{}{}
		if reason := w.skipAutoReplyReason(msg); reason != "" {
			w.handleAutoReplyGuard(ctx, msg, reason, "safety_scan")
			continue
		}
		attempted++
		w.logf("inbox safety scan found unprocessed message: id=%s from=%q subject=%q", msg.ID, formatFrom(msg.From), msg.Subject)
		fmt.Fprintf(w.config.Output, "FROM: %s\tSUBJECT: %s\n", formatFrom(msg.From), msg.Subject)
		if err := w.maybeAutoReply(ctx, msg); err != nil {
			w.logf("auto-reply failed from inbox safety scan: id=%s err=%v", msg.ID, err)
		}
	}

	w.logf("inbox safety scan complete: reason=%s fetched=%d attempted=%d", reason, len(messages.List), attempted)
	return nil
}

func (w *Watcher) skipAutoReplyReason(msg emailMessage) string {
	if len(msg.From) == 0 {
		return "missing_from"
	}

	username := strings.ToLower(strings.TrimSpace(w.creds.Username))
	for _, from := range msg.From {
		address := strings.ToLower(strings.TrimSpace(from.Email))
		if address == "" {
			continue
		}
		if username != "" && address == username {
			return "self_sender"
		}

		local, domain, ok := strings.Cut(address, "@")
		if !ok {
			continue
		}
		if domain == "keys.openpgp.org" {
			return "automated_sender"
		}

		normalizedLocal := strings.NewReplacer("-", "", "_", "", ".", "").Replace(local)
		switch normalizedLocal {
		case "noreply", "donotreply", "noresponse", "mailerdaemon", "postmaster":
			return "automated_sender"
		}
		if strings.Contains(normalizedLocal, "noreply") || strings.Contains(normalizedLocal, "donotreply") {
			return "automated_sender"
		}
	}

	return ""
}

func (w *Watcher) handleAutoReplyGuard(ctx context.Context, msg emailMessage, reason, source string) {
	w.logf("%s skipped message by auto-reply guard: id=%s reason=%s from=%q subject=%q", source, msg.ID, reason, formatFrom(msg.From), msg.Subject)
	if reason != "self_sender" {
		return
	}
	if err := w.deleteEmail(ctx, msg.ID); err != nil {
		w.logf("%s failed to delete self-sent guarded message: id=%s err=%v", source, msg.ID, err)
		return
	}
	w.logf("%s deleted self-sent guarded message: id=%s", source, msg.ID)
}

func (w *Watcher) maybeAutoReply(ctx context.Context, msg emailMessage) error {
	if w.creds.OpenAIAPIToken == "" {
		return fmt.Errorf("OPENAI_API_TOKEN is missing from credentials")
	}
	if w.draftsID == "" {
		return fmt.Errorf("drafts mailbox not found")
	}
	if w.identityID == "" {
		return fmt.Errorf("identity for %s not found", w.creds.Username)
	}

	full, err := w.fetchEmailForReply(ctx, msg.ID)
	if err != nil {
		return err
	}
	usage, limited, err := w.enforceDailyMessageLimit(ctx, full)
	if err != nil {
		return err
	}
	if limited {
		return w.deleteEmail(ctx, full.ID)
	}
	if err := w.registerCorrespondents(ctx, full, usage); err != nil {
		return err
	}
	body, protectedSubject, attachments, rejectReason, err := w.decryptVerifiedEmail(ctx, full)
	if err != nil {
		return err
	}
	if protectedSubject != "" {
		w.logf("auto-reply using decrypted protected subject: id=%s subject=%q", msg.ID, protectedSubject)
		full.Subject = protectedSubject
	}
	if rejectReason != "" {
		w.logf("auto-reply rejected by PGP policy: id=%s reason=%s", msg.ID, rejectReason)
		if err := w.sendReply(ctx, full, pgpRequiredReply(rejectReason, w.creds.PublicEmail), "", nil, emailFooterStats{RemainingToday: usage.remaining(), DailyMessageLimit: dailyMessageLimit}); err != nil {
			return err
		}
		return w.deleteEmail(ctx, full.ID)
	}
	if err := w.updateCorrespondentProfiles(ctx, full, body); err != nil {
		return err
	}

	w.logf("auto-reply calling OpenAI: id=%s body_bytes=%d attachments=%d", msg.ID, len(body), len(attachments))
	reply, err := w.openai.AnswerEmail(ctx, full.Subject, body, attachments)
	if err != nil {
		return err
	}
	w.logf("auto-reply model response received: id=%s response_bytes=%d total_tokens=%d", msg.ID, len(reply.Text), reply.Usage.TotalTokens)

	if err := w.sendReply(ctx, full, reply.Text, body, attachments, emailFooterStats{
		TokensUsed:        reply.Usage.TotalTokens,
		Model:             reply.Model,
		ToolsUsed:         reply.ToolsUsed,
		RemainingToday:    usage.remaining(),
		DailyMessageLimit: dailyMessageLimit,
	}); err != nil {
		return err
	}
	return w.deleteEmail(ctx, full.ID)
}

func (w *Watcher) fetchEmailForReply(ctx context.Context, id string) (emailMessage, error) {
	envelope, err := w.client.Call(ctx, []methodCall{
		{"Email/get", map[string]any{
			"accountId":           w.accountID,
			"ids":                 []string{id},
			"properties":          []string{"id", "blobId", "from", "to", "subject", "sentAt", "receivedAt", "textBody", "htmlBody", "attachments", "bodyValues", "messageId", "references"},
			"fetchTextBodyValues": true,
			"fetchHTMLBodyValues": false,
			"maxBodyValueBytes":   200000,
		}, "message"},
	})
	if err != nil {
		return emailMessage{}, err
	}

	for _, response := range envelope.MethodResponses {
		name, args, err := decodeMethodResponse(response)
		if err != nil {
			return emailMessage{}, err
		}
		switch name {
		case "Email/get":
			var got emailGetResponse
			if err := json.Unmarshal(args, &got); err != nil {
				return emailMessage{}, err
			}
			if len(got.List) == 0 {
				return emailMessage{}, fmt.Errorf("message %s not found", id)
			}
			msg := got.List[0]
			if msg.BlobID != "" {
				raw, err := w.client.Download(ctx, w.accountID, msg.BlobID, "message.eml", "message/rfc822")
				if err != nil {
					return emailMessage{}, err
				}
				msg.Raw = raw
			}
			return msg, nil
		case "error":
			return emailMessage{}, fmt.Errorf("JMAP fetch email error: %s", string(args))
		}
	}

	return emailMessage{}, fmt.Errorf("JMAP fetch email returned no Email/get response")
}

func (w *Watcher) registerCorrespondents(ctx context.Context, msg emailMessage, usage correspondentDailyUsage) error {
	if w.store == nil {
		return nil
	}
	timezone := deriveTimezoneFromEmailHeaders(msg.Raw)
	for _, from := range msg.From {
		email := strings.ToLower(strings.TrimSpace(from.Email))
		if email == "" {
			continue
		}
		registered, err := w.store.Register(ctx, email, strings.TrimSpace(from.Name), timezone)
		if err != nil {
			return err
		}
		w.logf("correspondent registered: email=%s new=%t zip_present=%t timezone_present=%t profile_request_needed=%t", email, registered.New, registered.ZipPresent, registered.TimezonePresent, registered.ProfileRequestNeeded)
		if registered.ProfileRequestNeeded {
			if err := w.sendProfileRequest(ctx, from, emailFooterStats{RemainingToday: usage.remaining(), DailyMessageLimit: dailyMessageLimit}); err != nil {
				return err
			}
			if err := w.store.MarkProfileRequestSent(ctx, email); err != nil {
				return err
			}
			w.logf("correspondent profile request sent: email=%s", email)
		}
	}
	return nil
}

func (w *Watcher) enforceDailyMessageLimit(ctx context.Context, msg emailMessage) (correspondentDailyUsage, bool, error) {
	if w.store == nil {
		return correspondentDailyUsage{Count: 0, Allowed: true}, false, nil
	}
	for _, from := range msg.From {
		email := strings.ToLower(strings.TrimSpace(from.Email))
		if email == "" {
			continue
		}
		usage, err := w.store.CountInboundMessage(ctx, email, dailyMessageLimit, time.Now())
		if err != nil {
			return correspondentDailyUsage{}, false, err
		}
		w.logf("correspondent daily usage counted: email=%s day=%s count=%d limit=%d allowed=%t", email, usage.Day, usage.Count, dailyMessageLimit, usage.Allowed)
		if !usage.Allowed {
			if err := w.sendRateLimitReply(ctx, from, usage, emailFooterStats{RemainingToday: usage.remaining(), DailyMessageLimit: dailyMessageLimit}); err != nil {
				return correspondentDailyUsage{}, false, err
			}
			w.logf("correspondent daily limit reply sent: email=%s day=%s count=%d limit=%d", email, usage.Day, usage.Count, dailyMessageLimit)
			return usage, true, nil
		}
		return usage, false, nil
	}
	return correspondentDailyUsage{Count: 0, Allowed: true}, false, nil
}

func (w *Watcher) sendRateLimitReply(ctx context.Context, to emailAddress, usage correspondentDailyUsage, footer emailFooterStats) error {
	body := fmt.Sprintf(strings.TrimSpace(`Hello,

This address accepts up to %d messages per sender per UTC day.

You have reached that limit for %s. Please try again tomorrow.

Thanks.`), dailyMessageLimit, usage.Day)
	htmlBody, err := formatReplyHTMLBody(body, emailMessage{}, "")
	if err != nil {
		return err
	}
	return w.sendEmail(ctx, []emailAddress{to}, "Daily message limit reached", body, htmlBody, nil, emailMessage{}, footer)
}

func (w *Watcher) sendProfileRequest(ctx context.Context, to emailAddress, footer emailFooterStats) error {
	body := strings.TrimSpace(`Hello,

I keep a small local profile for people who email this address so replies can handle local context correctly.

Could you reply with:

- Your ZIP code
- Your time zone, for example America/New_York or UTC-05:00

Thanks.`)
	htmlBody, err := formatReplyHTMLBody(body, emailMessage{}, "")
	if err != nil {
		return err
	}
	return w.sendEmail(ctx, []emailAddress{to}, "A quick setup question", body, htmlBody, nil, emailMessage{}, footer)
}

func (w *Watcher) updateCorrespondentProfiles(ctx context.Context, msg emailMessage, body string) error {
	if w.store == nil {
		return nil
	}
	update := extractCorrespondentProfileUpdate(body)
	if update.ZipCode == "" && update.TimeZone == "" {
		return nil
	}
	for _, from := range msg.From {
		email := strings.ToLower(strings.TrimSpace(from.Email))
		if email == "" {
			continue
		}
		if err := w.store.UpdateProfile(ctx, email, update); err != nil {
			return err
		}
		w.logf("correspondent profile updated from email body: email=%s zip_present=%t timezone_present=%t", email, update.ZipCode != "", update.TimeZone != "")
	}
	return nil
}

func (w *Watcher) sendReply(ctx context.Context, original emailMessage, body string, originalBody string, attachments []emailAttachment, footer emailFooterStats) error {
	to := original.From
	if len(to) == 0 {
		return fmt.Errorf("original email has no From address")
	}

	subject := replySubject(original.Subject)
	replyBody := formatReplyBody(body, original, originalBody)
	replyHTMLBody, err := formatReplyHTMLBody(body, original, originalBody)
	if err != nil {
		return err
	}
	replyAttachments, err := w.replyAttachments(ctx, attachments)
	if err != nil {
		return err
	}
	return w.sendEmail(ctx, to, subject, replyBody, replyHTMLBody, replyAttachments, original, footer)
}

func (w *Watcher) sendEmail(ctx context.Context, to []emailAddress, subject string, textBody string, htmlBody string, attachments []map[string]any, original emailMessage, footer emailFooterStats) error {
	if w.store != nil {
		totalTokens, err := w.store.RecordAccountTokenUsage(ctx, footer.TokensUsed)
		if err != nil {
			return err
		}
		footer.TotalTokensEver = totalTokens
		total, err := w.store.NextOutboundEmailTotal(ctx)
		if err != nil {
			return err
		}
		footer.TotalEmailsEver = total
	}
	if footer.DailyMessageLimit == 0 {
		footer.DailyMessageLimit = dailyMessageLimit
	}
	textBody = appendResponseFooterText(textBody, footer)
	htmlBody = appendResponseFooterHTML(htmlBody, footer)

	createEmail := map[string]any{
		"from":     []emailAddress{{Email: w.creds.Username}},
		"to":       to,
		"subject":  subject,
		"textBody": []map[string]any{{"partId": "text", "type": "text/plain"}},
		"htmlBody": []map[string]any{{"partId": "html", "type": "text/html"}},
		"bodyValues": map[string]any{
			"text": map[string]any{"charset": "utf-8", "value": textBody},
			"html": map[string]any{"charset": "utf-8", "value": htmlBody},
		},
		"mailboxIds": map[string]bool{w.draftsID: true},
		"keywords":   map[string]bool{"$draft": true},
	}
	if len(attachments) > 0 {
		createEmail["attachments"] = attachments
	}
	if len(original.MessageID) > 0 {
		createEmail["header:In-Reply-To:asMessageIds"] = original.MessageID
	}
	if references := replyReferences(original.References, original.MessageID); len(references) > 0 {
		createEmail["header:References:asMessageIds"] = references
	}

	envelope, err := w.client.Call(ctx, []methodCall{
		{"Email/set", map[string]any{
			"accountId": w.accountID,
			"create":    map[string]any{"reply": createEmail},
		}, "emailSet"},
		{"EmailSubmission/set", map[string]any{
			"accountId":             w.accountID,
			"onSuccessDestroyEmail": []string{"#submission"},
			"create": map[string]any{
				"submission": map[string]any{
					"emailId":    "#reply",
					"identityId": w.identityID,
				},
			},
		}, "submissionSet"},
	})
	if err != nil {
		return err
	}

	for _, response := range envelope.MethodResponses {
		name, args, err := decodeMethodResponse(response)
		if err != nil {
			return err
		}
		if name == "error" {
			return fmt.Errorf("JMAP send reply error: %s", string(args))
		}
		w.logf("auto-reply JMAP response: method=%s bytes=%d", name, len(args))
	}
	w.logf("auto-reply sent: original_id=%s to=%q subject=%q", original.ID, formatFrom(to), subject)
	return nil
}

func (w *Watcher) replyAttachments(ctx context.Context, attachments []emailAttachment) ([]map[string]any, error) {
	if len(attachments) == 0 {
		return nil, nil
	}

	parts := make([]map[string]any, 0, len(attachments))
	for _, attachment := range attachments {
		name := attachmentName(attachment)
		contentType := attachmentType(attachment)
		blobID := strings.TrimSpace(attachment.BlobID)
		if blobID == "" {
			uploaded, err := w.client.Upload(ctx, w.accountID, name, contentType, attachment.Data)
			if err != nil {
				return nil, err
			}
			blobID = uploaded.BlobID
		}
		if blobID == "" {
			return nil, fmt.Errorf("attachment %q has no blobId after upload", name)
		}
		parts = append(parts, map[string]any{
			"blobId":      blobID,
			"type":        contentType,
			"name":        name,
			"disposition": "attachment",
		})
	}
	return parts, nil
}

func replySubject(subject string) string {
	subject = strings.TrimSpace(subject)
	if strings.HasPrefix(strings.ToLower(subject), "re:") {
		return subject
	}
	return "Re: " + subject
}

func replyReferences(existing []string, messageIDs []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(messageIDs))
	references := make([]string, 0, len(existing)+len(messageIDs))
	for _, id := range append(existing, messageIDs...) {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		references = append(references, id)
	}
	return references
}

func (w *Watcher) deleteEmail(ctx context.Context, id string) error {
	envelope, err := w.client.Call(ctx, []methodCall{
		{"Email/set", map[string]any{
			"accountId": w.accountID,
			"destroy":   []string{id},
		}, "deleteOriginal"},
	})
	if err != nil {
		return err
	}

	for _, response := range envelope.MethodResponses {
		name, args, err := decodeMethodResponse(response)
		if err != nil {
			return err
		}
		if name == "error" {
			return fmt.Errorf("JMAP delete original email error: %s", string(args))
		}
		w.logf("original email delete response: method=%s bytes=%d", name, len(args))
	}
	w.logf("original email deleted after auto-reply: id=%s", id)
	return nil
}

func (w *Watcher) logf(format string, args ...any) {
	logf(w.config.LogOutput, format, args...)
}

func decodeMethodResponse(response methodResponse) (string, json.RawMessage, error) {
	if len(response) < 2 {
		return "", nil, fmt.Errorf("invalid JMAP method response")
	}
	var name string
	if err := json.Unmarshal(response[0], &name); err != nil {
		return "", nil, err
	}
	return name, response[1], nil
}

func selectMailboxID(mailboxes []mailbox, desired string) string {
	desired = strings.ToLower(strings.TrimSpace(desired))
	for _, mailbox := range mailboxes {
		if desired == "inbox" && strings.EqualFold(mailbox.Role, "inbox") {
			return mailbox.ID
		}
	}
	for _, mailbox := range mailboxes {
		if strings.EqualFold(mailbox.Name, desired) || strings.EqualFold(mailbox.ID, desired) {
			return mailbox.ID
		}
	}
	return ""
}

func formatFrom(addresses []emailAddress) string {
	if len(addresses) == 0 {
		return ""
	}
	parts := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if address.Name == "" {
			parts = append(parts, address.Email)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s <%s>", address.Name, address.Email))
	}
	return strings.Join(parts, ", ")
}
