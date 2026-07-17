package usenet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"os"
	"sort"
	"strings"
	"time"

	appconfig "ai-over-email/pkg/config"
	"ai-over-email/pkg/email"
)

type Config struct {
	EnvPath    string
	ConfigPath string
	Output     io.Writer
	LogOutput  io.Writer
}

type Watcher struct {
	config    Config
	appConfig appconfig.ConfigStruct
	usenet    appconfig.UsenetConfig
	creds     Credentials
	openai    *email.OpenAIClient
}

func NewWatcher(config Config) (*Watcher, error) {
	if config.EnvPath == "" {
		config.EnvPath = ".env"
	}
	if config.ConfigPath == "" {
		config.ConfigPath = "config.json"
	}
	if config.Output == nil {
		config.Output = os.Stdout
	}
	if config.LogOutput == nil {
		config.LogOutput = os.Stderr
	}
	appCfg, err := appconfig.Load(config.ConfigPath)
	if err != nil {
		return nil, err
	}
	usenetCfg := appCfg.Usenet.Normalized()
	if usenetCfg.Host == "" || usenetCfg.Group == "" {
		return nil, fmt.Errorf("usenet.host and usenet.group must be configured")
	}
	creds, err := LoadCredentials(config.EnvPath)
	if err != nil {
		return nil, err
	}
	return &Watcher{
		config:    config,
		appConfig: appCfg,
		usenet:    usenetCfg,
		creds:     creds,
		openai:    email.NewOpenAIClient(creds.OpenAIAPIToken, usenetCfg.FromAddress, creds.BraveSearchAPIToken, config.LogOutput),
	}, nil
}

func (w *Watcher) Run(ctx context.Context) error {
	interval, err := time.ParseDuration(w.usenet.PollInterval)
	if err != nil {
		return fmt.Errorf("parse usenet.poll_interval: %w", err)
	}
	if interval <= 0 {
		interval = time.Minute
	}
	w.logf("usenetwatch starting: host=%s port=%d security=%s group=%s state=%s poll_interval=%s", w.usenet.Host, w.usenet.Port, w.usenet.Security, w.usenet.Group, w.usenet.StatePath, interval)
	if err := w.Poll(ctx); err != nil {
		w.logf("initial poll failed: %v", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := w.Poll(ctx); err != nil {
				w.logf("poll failed: %v", err)
			}
		}
	}
}

func (w *Watcher) Poll(ctx context.Context) error {
	st, err := loadState(w.usenet.StatePath)
	if err != nil {
		return err
	}
	client, err := dialNNTP(w.usenet.Host, w.usenet.Port, w.usenet.Security, w.usenet.TLSServerName, w.usenet.TLSCertSHA256, 30*time.Second)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.Auth(w.creds.Username, w.creds.Password); err != nil {
		return err
	}
	status, err := client.Group(w.usenet.Group)
	if err != nil {
		return err
	}
	w.logf("selected group: name=%s low=%d high=%d count=%d last_seen=%d", status.Name, status.Low, status.High, status.Count, st.LastSeenNumber)
	start := status.Low
	if st.LastSeenNumber > 0 && st.LastSeenNumber+1 > start {
		start = st.LastSeenNumber + 1
	}
	for number := start; number <= status.High; number++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		article, err := client.ArticleByNumber(number)
		if errors.Is(err, errArticleMissing) {
			st.LastSeenNumber = max(st.LastSeenNumber, number)
			continue
		}
		if err != nil {
			return err
		}
		if article.MessageID == "" {
			w.logf("skipping article without Message-ID: number=%d subject=%q", number, article.Subject)
			st.LastSeenNumber = max(st.LastSeenNumber, number)
			continue
		}
		if isLeafnodePlaceholder(article) {
			w.logf("skipping Leafnode placeholder article: number=%d message_id=%s", number, article.MessageID)
			st.LastSeenNumber = max(st.LastSeenNumber, number)
			continue
		}
		if st.Replied[article.MessageID] != "" {
			st.LastSeenNumber = max(st.LastSeenNumber, number)
			continue
		}
		if w.isOwnArticle(article) {
			w.logf("skipping own article: number=%d message_id=%s", number, article.MessageID)
			st.LastSeenNumber = max(st.LastSeenNumber, number)
			continue
		}
		already, err := w.threadAlreadyAnswered(client, article)
		if err != nil {
			return err
		}
		if already {
			w.logf("skipping article with existing AI follow-up in thread: number=%d message_id=%s", number, article.MessageID)
			st.LastSeenNumber = max(st.LastSeenNumber, number)
			continue
		}
		if err := w.answerArticle(ctx, client, article, st); err != nil {
			return err
		}
		st.LastSeenNumber = max(st.LastSeenNumber, number)
		if err := saveState(w.usenet.StatePath, st); err != nil {
			return err
		}
	}
	st.LastSeenNumber = max(st.LastSeenNumber, status.High)
	return saveState(w.usenet.StatePath, st)
}

func (w *Watcher) answerArticle(ctx context.Context, client *nntpClient, current article, st state) error {
	thread, err := w.threadContext(client, current)
	if err != nil {
		return err
	}
	modelSettings := w.appConfig.OpenAISettingsForSenders(nil)
	w.logf("calling OpenAI for Usenet article: number=%d message_id=%s model=%s reasoning_effort=%s thread_articles=%d", current.Number, current.MessageID, modelSettings.Model, modelSettings.ReasoningEffort, len(thread))
	answer, err := w.openai.AnswerUsenetPost(ctx, w.usenet.Group, email.UsenetPostPrompt{
		Subject:       current.Subject,
		Author:        current.From,
		MessageID:     current.MessageID,
		Body:          current.Body,
		ThreadContext: formatThreadContext(thread),
	}, modelSettings)
	if err != nil {
		return err
	}
	raw, postedID, err := w.formatFollowup(current, answer.Text)
	if err != nil {
		return err
	}
	if err := client.Post(raw); err != nil {
		return err
	}
	st.Replied[current.MessageID] = postedID
	w.logf("posted Usenet follow-up: source_message_id=%s reply_message_id=%s total_tokens=%d", current.MessageID, postedID, answer.Usage.TotalTokens)
	fmt.Fprintf(w.config.Output, "POSTED\t%s\t%s\n", current.MessageID, postedID)
	return nil
}

func (w *Watcher) threadContext(client *nntpClient, current article) ([]article, error) {
	seen := map[string]article{}
	for _, ref := range current.References {
		ancestor, err := client.ArticleByMessageID(ref)
		if errors.Is(err, errArticleMissing) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if ancestor.MessageID != "" {
			seen[ancestor.MessageID] = ancestor
		}
	}
	status, err := client.Group(w.usenet.Group)
	if err != nil {
		return nil, err
	}
	start := current.Number - w.usenet.MaxThreadArticles
	if start < status.Low {
		start = status.Low
	}
	currentRefs := referenceSet(current)
	for number := start; number <= current.Number; number++ {
		candidate, err := client.ArticleByNumber(number)
		if errors.Is(err, errArticleMissing) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if candidate.MessageID == "" {
			continue
		}
		if candidate.MessageID == current.MessageID || referencesOverlap(currentRefs, candidate) {
			seen[candidate.MessageID] = candidate
		}
	}
	seen[current.MessageID] = current
	thread := make([]article, 0, len(seen))
	for _, item := range seen {
		thread = append(thread, item)
	}
	sort.Slice(thread, func(i, j int) bool {
		return thread[i].Number < thread[j].Number
	})
	return thread, nil
}

func (w *Watcher) threadAlreadyAnswered(client *nntpClient, current article) (bool, error) {
	thread, err := w.threadContext(client, current)
	if err != nil {
		return false, err
	}
	for _, item := range thread {
		if item.MessageID == current.MessageID {
			continue
		}
		if w.isOwnArticle(item) && containsReference(item.References, current.MessageID) {
			return true, nil
		}
	}
	return false, nil
}

func (w *Watcher) isOwnArticle(article article) bool {
	from := strings.ToLower(article.From)
	address := strings.ToLower(w.usenet.FromAddress)
	if address != "" && strings.Contains(from, address) {
		return true
	}
	return strings.EqualFold(article.RawHeader.Get("X-AI-Over-Usenet"), "true") ||
		strings.Contains(strings.ToLower(article.RawHeader.Get("User-Agent")), "ai-over-usenet")
}

func (w *Watcher) formatFollowup(original article, body string) (string, string, error) {
	from := mail.Address{Name: w.usenet.FromName, Address: w.usenet.FromAddress}
	messageID := newMessageID(w.usenet.FromAddress)
	subject := replySubject(original.Subject)
	references := append([]string{}, original.References...)
	references = append(references, original.MessageID)
	var out strings.Builder
	headers := []struct {
		key   string
		value string
	}{
		{"From", from.String()},
		{"Newsgroups", w.usenet.Group},
		{"Subject", subject},
		{"Message-ID", messageID},
		{"Date", time.Now().UTC().Format(time.RFC1123Z)},
		{"References", strings.Join(references, " ")},
		{"In-Reply-To", original.MessageID},
		{"User-Agent", "ai-over-usenet/1"},
		{"X-AI-Over-Usenet", "true"},
		{"MIME-Version", "1.0"},
		{"Content-Type", `text/plain; charset="UTF-8"`},
		{"Content-Transfer-Encoding", "8bit"},
	}
	for _, header := range headers {
		if strings.TrimSpace(header.value) == "" {
			continue
		}
		out.WriteString(header.key)
		out.WriteString(": ")
		out.WriteString(sanitizeHeader(header.value))
		out.WriteString("\r\n")
	}
	out.WriteString("\r\n")
	out.WriteString(normalizeBody(body))
	out.WriteString("\r\n")
	return out.String(), messageID, nil
}

func formatThreadContext(thread []article) string {
	var out strings.Builder
	for i, item := range thread {
		if i > 0 {
			out.WriteString("\n\n---\n\n")
		}
		out.WriteString(fmt.Sprintf("Article %d\nMessage-ID: %s\nFrom: %s\nSubject: %s\n\n%s", item.Number, item.MessageID, item.From, item.Subject, item.Body))
	}
	return out.String()
}

func referenceSet(article article) map[string]struct{} {
	refs := make(map[string]struct{}, len(article.References)+1)
	if article.MessageID != "" {
		refs[article.MessageID] = struct{}{}
	}
	for _, ref := range article.References {
		refs[ref] = struct{}{}
	}
	return refs
}

func referencesOverlap(refs map[string]struct{}, candidate article) bool {
	if _, ok := refs[candidate.MessageID]; ok {
		return true
	}
	for _, ref := range candidate.References {
		if _, ok := refs[ref]; ok {
			return true
		}
	}
	return false
}

func containsReference(refs []string, messageID string) bool {
	for _, ref := range refs {
		if ref == messageID {
			return true
		}
	}
	return false
}

func isLeafnodePlaceholder(article article) bool {
	return strings.HasPrefix(article.MessageID, "<leafnode:placeholder:") ||
		strings.Contains(strings.ToLower(article.Subject), "leafnode placeholder for group")
}

func replySubject(subject string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(subject)), "re:") {
		return subject
	}
	if strings.TrimSpace(subject) == "" {
		return "Re: misc.pegasus post"
	}
	return "Re: " + subject
}

func sanitizeHeader(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

func normalizeBody(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n")
}

func newMessageID(fromAddress string) string {
	domain := "localhost"
	if _, after, ok := strings.Cut(fromAddress, "@"); ok && strings.TrimSpace(after) != "" {
		domain = strings.TrimSpace(after)
	}
	return fmt.Sprintf("<%d.%d.ai-over-usenet@%s>", time.Now().UnixNano(), os.Getpid(), domain)
}

func (w *Watcher) logf(format string, args ...any) {
	if w.config.LogOutput == nil {
		return
	}
	fmt.Fprintf(w.config.LogOutput, "%s usenetwatch: %s\n", time.Now().UTC().Format(time.RFC3339Nano), fmt.Sprintf(format, args...))
}
