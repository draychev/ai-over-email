package email

import (
	"bytes"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

func extractEmailBody(msg emailMessage) string {
	for _, part := range msg.TextBody {
		if value := msg.BodyValues[part.PartID].Value; strings.TrimSpace(value) != "" {
			return value
		}
	}
	for _, value := range msg.BodyValues {
		if strings.TrimSpace(value.Value) != "" {
			return value.Value
		}
	}
	return ""
}

func selectIdentityID(identities []identity, wantedEmail string) string {
	for _, identity := range identities {
		if strings.EqualFold(identity.Email, wantedEmail) {
			return identity.ID
		}
	}
	if len(identities) > 0 {
		return identities[0].ID
	}
	return ""
}

func formatReplyBody(reply string, original emailMessage, originalBody string) string {
	reply = strings.TrimRight(strings.TrimSpace(reply), "\r\n")
	originalBody = strings.TrimSpace(originalBody)
	if originalBody == "" {
		return reply
	}
	if reply == "" {
		return quotedOriginalMessage(original, originalBody)
	}
	return reply + "\n\n" + quotedOriginalMessage(original, originalBody)
}

func formatReplyHTMLBody(reply string, original emailMessage, originalBody string) (string, error) {
	reply = strings.TrimSpace(reply)
	originalBody = strings.TrimSpace(originalBody)

	var out strings.Builder
	if reply != "" {
		rendered, err := renderMarkdownHTML(reply)
		if err != nil {
			return "", err
		}
		out.WriteString(rendered)
	}
	if originalBody != "" {
		if strings.TrimSpace(out.String()) != "" {
			out.WriteString("\n")
		}
		out.WriteString(quotedOriginalHTMLMessage(original, originalBody))
	}
	return strings.TrimSpace(out.String()), nil
}

func renderMarkdownHTML(markdown string) (string, error) {
	var buf bytes.Buffer
	md := goldmark.New(goldmark.WithExtensions(extension.Table))
	if err := md.Convert([]byte(markdown), &buf); err != nil {
		return "", fmt.Errorf("render markdown html: %w", err)
	}
	return strings.TrimSpace(buf.String()), nil
}

func quotedOriginalMessage(original emailMessage, body string) string {
	header := "On " + replyQuoteDate(original) + ", " + replyQuoteSender(original) + " wrote:"
	return header + "\n" + quoteEmailText(body)
}

func quotedOriginalHTMLMessage(original emailMessage, body string) string {
	header := html.EscapeString("On " + replyQuoteDate(original) + ", " + replyQuoteSender(original) + " wrote:")
	return "<p>" + header + "</p>\n<blockquote type=\"cite\">\n" + htmlEmailText(body) + "\n</blockquote>"
}

func replyQuoteDate(original emailMessage) string {
	for _, value := range []string{original.SentAt, original.ReceivedAt} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			continue
		}
		return t.UTC().Format("Mon, 02 Jan 2006 15:04 UTC")
	}
	return "the original message"
}

func replyQuoteSender(original emailMessage) string {
	sender := strings.TrimSpace(formatFrom(original.From))
	if sender == "" {
		return "the sender"
	}
	return sender
}

func quoteEmailText(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[i] = ">"
			continue
		}
		lines[i] = "> " + line
	}
	return strings.Join(lines, "\n")
}

func htmlEmailText(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	escaped := html.EscapeString(body)
	return strings.ReplaceAll(escaped, "\n", "<br>\n")
}
