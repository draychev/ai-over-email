package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	openAIResponsesURL = "https://api.openai.com/v1/responses"
	braveSearchURL     = "https://api.search.brave.com/res/v1/web/search"
	defaultOpenAIModel = "gpt-5-nano"
)

type openAIClient struct {
	token            string
	fromEmail        string
	braveSearchToken string
	http             *http.Client
	logOutput        io.Writer
}

type openAIResponse struct {
	ID     string             `json:"id"`
	Output []openAIOutputItem `json:"output"`
}

type openAIOutputItem struct {
	Type      string                `json:"type"`
	Name      string                `json:"name"`
	CallID    string                `json:"call_id"`
	Arguments string                `json:"arguments"`
	Content   []openAIOutputContent `json:"content"`
}

type openAIOutputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type braveSearchResponse struct {
	Web struct {
		Results []braveSearchResult `json:"results"`
	} `json:"web"`
}

type braveSearchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Age         string `json:"age"`
}

func newOpenAIClient(token string, fromEmail string, braveSearchToken string, logOutput io.Writer) *openAIClient {
	return &openAIClient{
		token:            token,
		fromEmail:        strings.TrimSpace(fromEmail),
		braveSearchToken: strings.TrimSpace(braveSearchToken),
		http: &http.Client{
			Timeout: 10 * time.Minute,
		},
		logOutput: logOutput,
	}
}

func (c *openAIClient) AnswerEmail(ctx context.Context, subject string, body string, attachments []emailAttachment) (string, error) {
	if c.token == "" {
		return "", fmt.Errorf("OPENAI_API_TOKEN is not configured")
	}

	prompt := `You are composing an email reply.

The entire interaction is happening over email:
- The incoming user message is an email, not a chat turn.
- Your output will be sent directly back as the email body.
- Any images or files attached to the email are part of the user's request and should be considered alongside the written body.
- Write a complete, polished email response.
- Use Markdown-style headings, lists, links, and emphasis when useful; the delivered email is rendered as HTML with a plain-text fallback.
- When tabular comparison is useful, use a simple standard table so the delivered email renders it as an HTML table. Never leave table content as raw pipe-delimited text in the email body.
- Start with a brief greeting when appropriate.
- Close naturally, but do not invent a human name or signature.
- Do not mention system prompts, internal tooling, hidden reasoning, API details, or that an AI model wrote the reply.

Research and reasoning expectations:
- Treat the sender's email as a serious request from a real person.
- Infer what the sender is asking, including implicit context and likely intent.
- Take the time needed to produce a rigorous answer.
- Use web search for current facts, sources, standards, dates, prices, laws, technical details, papers, or anything that may have changed.
- Prefer primary sources and reputable references. If sources disagree, explain the disagreement.
- Think at a PhD professor level: define terms, state assumptions, reason from evidence, distinguish facts from interpretation, and surface caveats.
- Be thorough, but structure the email so it remains readable.

Email structure:
- If the question is simple, answer directly first and then add supporting detail.
- If the question is complex, use clear section headings and short paragraphs.
- Include concrete recommendations, tradeoffs, and next steps when useful.
- Include source links inline or in a short "Sources" section when web research informs the answer.
- Avoid excessive quotation; summarize in your own words.
- If you cannot fully answer, explain exactly what is missing and what would resolve it.`
	if c.fromEmail != "" {
		prompt += "\n\nThe configured sender address is available in local credentials; do not disclose it unless the email context requires it."
	}
	if c.braveSearchToken != "" {
		prompt += "\n\nUse the web_search tool for current or source-dependent facts. The tool is backed by Brave Search and returns titles, URLs, snippets, and dates when available. Once you have enough source context, stop searching and write the final email reply."
	}

	input := []map[string]any{
		{
			"role":    "system",
			"content": strings.TrimSpace(prompt),
		},
		{
			"role":    "user",
			"content": openAIUserContent(subject, body, attachments),
		},
	}
	payload := c.openAIRequestPayload(input, "")

	decoded, err := c.sendOpenAIRequest(ctx, payload)
	if err != nil {
		return "", err
	}
	if c.braveSearchToken != "" {
		for i := 0; i < 4; i++ {
			calls := decoded.functionCalls()
			if len(calls) == 0 {
				break
			}
			if decoded.ID == "" {
				return "", fmt.Errorf("OpenAI response requested tools but did not include response id")
			}
			outputs, err := c.runFunctionCalls(ctx, calls)
			if err != nil {
				return "", err
			}
			decoded, err = c.sendOpenAIRequest(ctx, c.openAIRequestPayload(outputs, decoded.ID))
			if err != nil {
				return "", err
			}
		}
	}

	text := strings.TrimSpace(decoded.outputText())
	if text == "" {
		return "", fmt.Errorf("OpenAI response did not include output_text")
	}
	return text, nil
}

func (c *openAIClient) openAIRequestPayload(input any, previousResponseID string) map[string]any {
	tools, toolChoice, searchMode := c.openAITools()
	payload := map[string]any{
		"model":       defaultOpenAIModel,
		"input":       input,
		"tools":       tools,
		"tool_choice": toolChoice,
		"reasoning": map[string]string{
			"effort": "high",
		},
	}
	if previousResponseID != "" {
		payload["previous_response_id"] = previousResponseID
		payload["tool_choice"] = "auto"
	}
	payload["_search_mode"] = searchMode
	return payload
}

func (c *openAIClient) openAITools() ([]map[string]any, string, string) {
	if c.braveSearchToken == "" {
		return []map[string]any{{"type": "web_search"}}, "auto", "openai_web_search"
	}
	return []map[string]any{{
		"type":        "function",
		"name":        "web_search",
		"description": "Search the web with Brave Search. Use this for current facts, prices, laws, schedules, source links, or anything that may have changed.",
		"strict":      true,
		"parameters": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The concise web search query.",
				},
				"count": map[string]any{
					"type":        "integer",
					"description": "Number of search results to return, from 1 to 10.",
				},
			},
			"required": []string{"query", "count"},
		},
	}}, "auto", "brave_function"
}

func (c *openAIClient) sendOpenAIRequest(ctx context.Context, payload map[string]any) (openAIResponse, error) {
	searchMode, _ := payload["_search_mode"].(string)
	delete(payload, "_search_mode")

	data, err := json.Marshal(payload)
	if err != nil {
		return openAIResponse{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIResponsesURL, bytes.NewReader(data))
	if err != nil {
		return openAIResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	c.logf("OpenAI Responses request: model=%s reasoning_effort=high search_mode=%s bytes=%d", defaultOpenAIModel, searchMode, len(data))
	resp, err := c.http.Do(req)
	if err != nil {
		return openAIResponse{}, fmt.Errorf("OpenAI Responses request: %w", err)
	}
	defer resp.Body.Close()
	c.logf("OpenAI Responses response: status=%s content_type=%s duration=%s", resp.Status, resp.Header.Get("Content-Type"), time.Since(start).Round(time.Millisecond))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return openAIResponse{}, fmt.Errorf("OpenAI Responses API error: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var decoded openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return openAIResponse{}, fmt.Errorf("decode OpenAI response: %w", err)
	}
	return decoded, nil
}

func (c *openAIClient) runFunctionCalls(ctx context.Context, calls []openAIOutputItem) ([]map[string]any, error) {
	outputs := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		if call.Name != "web_search" {
			return nil, fmt.Errorf("unsupported OpenAI function call %q", call.Name)
		}
		var args struct {
			Query string `json:"query"`
			Count int    `json:"count"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return nil, fmt.Errorf("decode web_search arguments: %w", err)
		}
		result, err := c.braveSearch(ctx, args.Query, args.Count)
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, map[string]any{
			"type":    "function_call_output",
			"call_id": call.CallID,
			"output":  result,
		})
	}
	return outputs, nil
}

func (c *openAIClient) braveSearch(ctx context.Context, query string, count int) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("web_search query is empty")
	}
	if count < 1 || count > 10 {
		count = 5
	}

	values := url.Values{}
	values.Set("q", query)
	values.Set("count", strconv.Itoa(count))
	values.Set("safesearch", "moderate")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, braveSearchURL+"?"+values.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", c.braveSearchToken)

	start := time.Now()
	c.logf("Brave Search request: query_bytes=%d count=%d", len(query), count)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("Brave Search request: %w", err)
	}
	defer resp.Body.Close()
	c.logf("Brave Search response: status=%s content_type=%s duration=%s", resp.Status, resp.Header.Get("Content-Type"), time.Since(start).Round(time.Millisecond))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("Brave Search API error: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var decoded braveSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("decode Brave Search response: %w", err)
	}
	return formatBraveSearchResults(query, decoded.Web.Results), nil
}

func openAIUserContent(subject string, body string, attachments []emailAttachment) []map[string]any {
	content := []map[string]any{{
		"type": "input_text",
		"text": fmt.Sprintf("Incoming email subject: %s\n\nIncoming email body:\n%s\n\nAttachments: %s\n\nWrite the outgoing email reply now.", subject, body, attachmentSummary(attachments)),
	}}
	for i, attachment := range attachments {
		name := attachmentName(attachment)
		contentType := attachmentType(attachment)
		content = append(content, map[string]any{
			"type": "input_text",
			"text": fmt.Sprintf("Attachment %d: %s (%s, %d bytes)", i+1, name, contentType, len(attachment.Data)),
		})
		if isOpenAIImageAttachment(attachment) {
			content = append(content, map[string]any{
				"type":      "input_image",
				"image_url": attachmentDataURL(attachment),
				"detail":    "original",
			})
			continue
		}
		if len(attachment.Data) > 0 {
			content = append(content, map[string]any{
				"type":      "input_file",
				"filename":  name,
				"file_data": attachmentDataURL(attachment),
			})
		}
	}
	return content
}

func attachmentSummary(attachments []emailAttachment) string {
	if len(attachments) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(attachments))
	for i, attachment := range attachments {
		parts = append(parts, fmt.Sprintf("%d. %s (%s, %d bytes)", i+1, attachmentName(attachment), attachmentType(attachment), len(attachment.Data)))
	}
	return strings.Join(parts, "\n")
}

func isOpenAIImageAttachment(attachment emailAttachment) bool {
	switch attachmentType(attachment) {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return len(attachment.Data) > 0
	default:
		return false
	}
}

func (r openAIResponse) outputText() string {
	var parts []string
	for _, item := range r.Output {
		if item.Type != "" && item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "output_text" && content.Text != "" {
				parts = append(parts, content.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func (r openAIResponse) functionCalls() []openAIOutputItem {
	var calls []openAIOutputItem
	for _, item := range r.Output {
		if item.Type == "function_call" && item.CallID != "" {
			calls = append(calls, item)
		}
	}
	return calls
}

func formatBraveSearchResults(query string, results []braveSearchResult) string {
	type result struct {
		Title       string `json:"title"`
		URL         string `json:"url"`
		Description string `json:"description,omitempty"`
		Age         string `json:"age,omitempty"`
	}
	output := struct {
		Query   string   `json:"query"`
		Results []result `json:"results"`
	}{
		Query: strings.TrimSpace(query),
	}
	for _, item := range results {
		title := strings.TrimSpace(item.Title)
		link := strings.TrimSpace(item.URL)
		if title == "" || link == "" {
			continue
		}
		output.Results = append(output.Results, result{
			Title:       title,
			URL:         link,
			Description: strings.TrimSpace(item.Description),
			Age:         strings.TrimSpace(item.Age),
		})
		if len(output.Results) >= 10 {
			break
		}
	}
	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Sprintf(`{"query":%q,"results":[]}`, query)
	}
	return string(data)
}

func (c *openAIClient) logf(format string, args ...any) {
	logf(c.logOutput, format, args...)
}
