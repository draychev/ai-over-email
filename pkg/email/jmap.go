package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	appconfig "ai-over-email/pkg/config"
)

const (
	capCore       = "urn:ietf:params:jmap:core"
	capMail       = "urn:ietf:params:jmap:mail"
	capSubmission = "urn:ietf:params:jmap:submission"
)

type authMode int

const (
	authBasic authMode = iota
	authBearer
)

type jmapClient struct {
	httpClient  *http.Client
	eventClient *http.Client
	creds       Credentials
	auth        authMode
	session     Session
	logOutput   io.Writer
}

type Session struct {
	Accounts        map[string]Account `json:"accounts"`
	PrimaryAccounts map[string]string  `json:"primaryAccounts"`
	APIURL          string             `json:"apiUrl"`
	EventSourceURL  string             `json:"eventSourceUrl"`
	DownloadURL     string             `json:"downloadUrl"`
	UploadURL       string             `json:"uploadUrl"`
}

type Account struct {
	Name string `json:"name"`
}

type requestEnvelope struct {
	Using       []string     `json:"using"`
	MethodCalls []methodCall `json:"methodCalls"`
}

type methodCall []any

type responseEnvelope struct {
	MethodResponses []methodResponse `json:"methodResponses"`
}

type uploadResponse struct {
	AccountID string `json:"accountId"`
	BlobID    string `json:"blobId"`
	Type      string `json:"type"`
	Size      int    `json:"size"`
}

type methodResponse []json.RawMessage

func newJMAPClient(creds Credentials, logOutput io.Writer) *jmapClient {
	transport := &http.Transport{
		MaxIdleConns:        4,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
	}
	return &jmapClient{
		httpClient: &http.Client{
			Timeout:   20 * time.Second,
			Transport: transport,
		},
		eventClient: &http.Client{
			Transport: transport,
		},
		creds:     creds,
		logOutput: logOutput,
	}
}

func (c *jmapClient) FetchSession(ctx context.Context, config appconfig.ConfigStruct) error {
	var attempts []string

	if c.creds.Token != "" {
		c.auth = authBearer
		c.logf("attempting JMAP session with bearer token: endpoint=%s", config.JMAP.SessionEndpoint)
		if err := c.fetchSessionAt(ctx, config.JMAP.SessionEndpoint); err == nil {
			c.logSession(config.JMAP.SessionEndpoint)
			return nil
		} else {
			c.logf("bearer JMAP session attempt failed: endpoint=%s err=%v", config.JMAP.SessionEndpoint, err)
			attempts = append(attempts, err.Error())
		}
	}

	if c.creds.Username != "" && c.creds.Password != "" {
		c.auth = authBasic
		c.logf("attempting JMAP session with Basic auth: endpoint=%s username_present=%t", config.JMAP.LegacyBasicAuthSessionEndpoint, c.creds.Username != "")
		if err := c.fetchSessionAt(ctx, config.JMAP.LegacyBasicAuthSessionEndpoint); err == nil {
			c.logSession(config.JMAP.LegacyBasicAuthSessionEndpoint)
			return nil
		} else {
			c.logf("Basic auth JMAP session attempt failed: endpoint=%s err=%v", config.JMAP.LegacyBasicAuthSessionEndpoint, err)
			attempts = append(attempts, err.Error())
		}
	}

	return fmt.Errorf("could not open JMAP session; Fastmail's current JMAP API requires a JMAP API token in AI_OVER_EMAIL_FASTMAIL_TOKEN; attempts: %s", strings.Join(attempts, " | "))
}

func (c *jmapClient) fetchSessionAt(ctx context.Context, endpoint string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	c.authorize(req)

	start := time.Now()
	c.logf("JMAP session request: method=%s url=%s auth=%s", req.Method, req.URL.Redacted(), c.auth.String())
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("get JMAP session: %w", err)
	}
	defer resp.Body.Close()
	c.logf("JMAP session response: status=%s content_type=%s duration=%s", resp.Status, resp.Header.Get("Content-Type"), time.Since(start).Round(time.Millisecond))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("get JMAP session at %s: %s: %s", endpoint, resp.Status, strings.TrimSpace(string(body)))
	}
	if contentType := resp.Header.Get("Content-Type"); contentType != "" && !strings.Contains(strings.ToLower(contentType), "json") {
		return fmt.Errorf("get JMAP session at %s: expected JSON session, got %s", endpoint, contentType)
	}

	if err := json.NewDecoder(resp.Body).Decode(&c.session); err != nil {
		return fmt.Errorf("decode JMAP session: %w", err)
	}
	if c.session.APIURL == "" {
		return fmt.Errorf("JMAP session from %s did not include apiUrl", endpoint)
	}
	if c.session.EventSourceURL == "" {
		return fmt.Errorf("JMAP session from %s did not include eventSourceUrl", endpoint)
	}

	return nil
}

func (c *jmapClient) AccountID() (string, error) {
	if id := c.session.PrimaryAccounts[capMail]; id != "" {
		return id, nil
	}
	for id := range c.session.Accounts {
		return id, nil
	}
	return "", fmt.Errorf("JMAP session did not include a usable account")
}

func (c *jmapClient) Call(ctx context.Context, calls []methodCall) (responseEnvelope, error) {
	body, err := json.Marshal(requestEnvelope{
		Using:       []string{capCore, capMail, capSubmission},
		MethodCalls: calls,
	})
	if err != nil {
		return responseEnvelope{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.session.APIURL, bytes.NewReader(body))
	if err != nil {
		return responseEnvelope{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	c.authorize(req)

	start := time.Now()
	c.logf("JMAP API request: url=%s methods=%s bytes=%d", req.URL.Redacted(), methodCallNames(calls), len(body))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return responseEnvelope{}, fmt.Errorf("JMAP API call: %w", err)
	}
	defer resp.Body.Close()
	c.logf("JMAP API response: status=%s content_type=%s duration=%s", resp.Status, resp.Header.Get("Content-Type"), time.Since(start).Round(time.Millisecond))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return responseEnvelope{}, fmt.Errorf("JMAP API call: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var envelope responseEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return responseEnvelope{}, fmt.Errorf("decode JMAP API response: %w", err)
	}
	c.logf("JMAP API decoded response: methods=%s", methodResponseNames(envelope.MethodResponses))
	return envelope, nil
}

func (c *jmapClient) Download(ctx context.Context, accountID string, blobID string, name string, contentType string) ([]byte, error) {
	if c.session.DownloadURL == "" {
		return nil, fmt.Errorf("JMAP session did not include downloadUrl")
	}
	downloadURL := expandURLTemplate(c.session.DownloadURL, map[string]string{
		"accountId": accountID,
		"blobId":    blobID,
		"name":      name,
		"type":      contentType,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", contentType)
	c.authorize(req)

	start := time.Now()
	c.logf("JMAP download request: url=%s blob_id=%s", req.URL.Redacted(), blobID)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("JMAP download: %w", err)
	}
	defer resp.Body.Close()
	c.logf("JMAP download response: status=%s content_type=%s duration=%s", resp.Status, resp.Header.Get("Content-Type"), time.Since(start).Round(time.Millisecond))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("JMAP download: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 25*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read JMAP download: %w", err)
	}
	return body, nil
}

func (c *jmapClient) Upload(ctx context.Context, accountID string, name string, contentType string, data []byte) (uploadResponse, error) {
	if c.session.UploadURL == "" {
		return uploadResponse{}, fmt.Errorf("JMAP session did not include uploadUrl")
	}
	uploadURL := expandURLTemplate(c.session.UploadURL, map[string]string{
		"accountId": accountID,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(data))
	if err != nil {
		return uploadResponse{}, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	if name != "" {
		req.Header.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	}
	c.authorize(req)

	start := time.Now()
	c.logf("JMAP upload request: url=%s name=%q type=%s bytes=%d", req.URL.Redacted(), name, contentType, len(data))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return uploadResponse{}, fmt.Errorf("JMAP upload: %w", err)
	}
	defer resp.Body.Close()
	c.logf("JMAP upload response: status=%s content_type=%s duration=%s", resp.Status, resp.Header.Get("Content-Type"), time.Since(start).Round(time.Millisecond))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return uploadResponse{}, fmt.Errorf("JMAP upload: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var uploaded uploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&uploaded); err != nil {
		return uploadResponse{}, fmt.Errorf("decode JMAP upload: %w", err)
	}
	return uploaded, nil
}

func (c *jmapClient) NewEventSourceRequest(ctx context.Context, lastEventID string) (*http.Request, error) {
	eventURL := expandEventSourceURL(c.session.EventSourceURL, map[string]string{
		"types":      "Email",
		"closeafter": "no",
		"ping":       "15",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, eventURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	c.authorize(req)
	return req, nil
}

func (c *jmapClient) Do(req *http.Request) (*http.Response, error) {
	c.logf("HTTP request: method=%s url=%s accept=%s", req.Method, req.URL.Redacted(), req.Header.Get("Accept"))
	return c.eventClient.Do(req)
}

func (c *jmapClient) authorize(req *http.Request) {
	if c.auth == authBearer {
		req.Header.Set("Authorization", "Bearer "+c.creds.Token)
		return
	}
	raw := c.creds.Username + ":" + c.creds.Password
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(raw)))
}

func expandEventSourceURL(tmpl string, values map[string]string) string {
	return expandURLTemplate(tmpl, values)
}

func expandURLTemplate(tmpl string, values map[string]string) string {
	result := tmpl
	result = expandQueryTemplate(result, values, "{?", "?")
	result = expandQueryTemplate(result, values, "{&", "&")
	for key, value := range values {
		result = strings.ReplaceAll(result, "{"+key+"}", url.QueryEscape(value))
	}
	return result
}

func expandQueryTemplate(tmpl string, values map[string]string, prefix string, separator string) string {
	start := strings.Index(tmpl, prefix)
	if start == -1 {
		return tmpl
	}
	end := strings.Index(tmpl[start:], "}")
	if end == -1 {
		return tmpl
	}
	end += start

	var parts []string
	for _, key := range strings.Split(tmpl[start+len(prefix):end], ",") {
		key = strings.TrimSpace(key)
		if value, ok := values[key]; ok {
			parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(value))
		}
	}
	replacement := ""
	if len(parts) > 0 {
		replacement = separator + strings.Join(parts, "&")
	}
	return tmpl[:start] + replacement + tmpl[end+1:]
}

func (c *jmapClient) logSession(endpoint string) {
	c.logf("JMAP session established: endpoint=%s api_url=%s event_source_template=%s accounts=%d primary_mail_account=%s", endpoint, c.session.APIURL, c.session.EventSourceURL, len(c.session.Accounts), c.session.PrimaryAccounts[capMail])
}

func (c *jmapClient) logf(format string, args ...any) {
	logf(c.logOutput, format, args...)
}

func methodCallNames(calls []methodCall) string {
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		if len(call) == 0 {
			continue
		}
		name, ok := call[0].(string)
		if !ok {
			continue
		}
		names = append(names, name)
	}
	return strings.Join(names, ",")
}

func methodResponseNames(responses []methodResponse) string {
	names := make([]string, 0, len(responses))
	for _, response := range responses {
		if len(response) == 0 {
			continue
		}
		var name string
		if err := json.Unmarshal(response[0], &name); err == nil {
			names = append(names, name)
		}
	}
	return strings.Join(names, ",")
}

func (a authMode) String() string {
	switch a {
	case authBearer:
		return "bearer"
	case authBasic:
		return "basic"
	default:
		return "unknown"
	}
}
