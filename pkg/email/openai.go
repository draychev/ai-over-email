package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	openAIResponsesURL = "https://api.openai.com/v1/responses"
	defaultOpenAIModel = "gpt-5-nano"
)

type openAIClient struct {
	token     string
	fromEmail string
	http      *http.Client
	logOutput io.Writer
}

type openAIResponse struct {
	Output []openAIOutputItem `json:"output"`
}

type openAIOutputItem struct {
	Type    string                `json:"type"`
	Content []openAIOutputContent `json:"content"`
}

type openAIOutputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func newOpenAIClient(token string, fromEmail string, logOutput io.Writer) *openAIClient {
	return &openAIClient{
		token:     token,
		fromEmail: strings.TrimSpace(fromEmail),
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
	payload := map[string]any{
		"model": defaultOpenAIModel,
		"input": input,
		"tools": []map[string]any{
			{"type": "web_search"},
		},
		"tool_choice": "required",
		"reasoning": map[string]string{
			"effort": "high",
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIResponsesURL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	c.logf("OpenAI Responses request: model=%s reasoning_effort=high web_search=required bytes=%d", defaultOpenAIModel, len(data))
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("OpenAI Responses request: %w", err)
	}
	defer resp.Body.Close()
	c.logf("OpenAI Responses response: status=%s content_type=%s duration=%s", resp.Status, resp.Header.Get("Content-Type"), time.Since(start).Round(time.Millisecond))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return "", fmt.Errorf("OpenAI Responses API error: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var decoded openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("decode OpenAI response: %w", err)
	}

	text := strings.TrimSpace(decoded.outputText())
	if text == "" {
		return "", fmt.Errorf("OpenAI response did not include output_text")
	}
	return text, nil
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

func (c *openAIClient) logf(format string, args ...any) {
	logf(c.logOutput, format, args...)
}
