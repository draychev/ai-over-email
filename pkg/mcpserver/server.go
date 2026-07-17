package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"ai-over-email/pkg/email"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	serverName    = "fastmail-mcp"
	serverVersion = "0.1.0"
)

func New(inspector *email.Inspector) *server.MCPServer {
	s := server.NewMCPServer(
		serverName,
		serverVersion,
		server.WithToolCapabilities(false),
	)

	s.AddTool(
		mcp.NewTool("list_mailboxes",
			mcp.WithDescription("List Fastmail mailboxes available through JMAP."),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			mailboxes, err := inspector.ListMailboxes(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonResult(mailboxes)
		},
	)

	s.AddTool(
		mcp.NewTool("search_messages",
			mcp.WithDescription("Search Fastmail messages with mailbox and simple header/body filters."),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithString("mailbox", mcp.Description("Mailbox name or role, for example inbox, archive, sent, drafts.")),
			mcp.WithString("from", mcp.Description("Filter by sender text.")),
			mcp.WithString("subject", mcp.Description("Filter by subject text.")),
			mcp.WithString("text", mcp.Description("Full-text search across message content.")),
			mcp.WithString("received_after", mcp.Description("JMAP date-time lower bound, for example 2026-07-17T00:00:00Z.")),
			mcp.WithString("received_before", mcp.Description("JMAP date-time upper bound, for example 2026-07-18T00:00:00Z.")),
			mcp.WithBoolean("unread", mcp.Description("When true, only return unread messages.")),
			mcp.WithNumber("limit", mcp.Description("Maximum number of messages to return. Defaults to 25.")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			opts := email.SearchOptions{
				MailboxName:    req.GetString("mailbox", ""),
				From:           req.GetString("from", ""),
				Subject:        req.GetString("subject", ""),
				Text:           req.GetString("text", ""),
				ReceivedAfter:  req.GetString("received_after", ""),
				ReceivedBefore: req.GetString("received_before", ""),
				Unread:         req.GetBool("unread", false),
				Limit:          req.GetInt("limit", 25),
			}
			messages, err := inspector.SearchMessages(ctx, opts)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonResult(messages)
		},
	)

	s.AddTool(
		mcp.NewTool("get_message",
			mcp.WithDescription("Fetch one Fastmail message by JMAP id, including text body and attachments."),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithString("id", mcp.Required(), mcp.Description("The JMAP message id to fetch.")),
			mcp.WithBoolean("include_raw_rfc822", mcp.Description("When true, also fetch the raw RFC822 source for the message.")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			id, err := req.RequireString("id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			message, err := inspector.GetMessage(ctx, id, req.GetBool("include_raw_rfc822", false))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonResult(message)
		},
	)

	return s
}

func RunStdio(ctx context.Context) error {
	inspector, err := email.NewInspector(email.Config{
		EnvPath:      ".env",
		ConfigPath:   "config.json",
		DatabasePath: ".tmp/correspondents.sqlite3",
		Output:       os.Stdout,
		LogOutput:    os.Stderr,
	})
	if err != nil {
		return err
	}

	s := New(inspector)
	return server.ServeStdio(s, server.WithStdioContextFunc(func(context.Context) context.Context {
		return ctx
	}))
}

func jsonResult(value any) (*mcp.CallToolResult, error) {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultStructured(value, strings.TrimSpace(string(payload))), nil
}
