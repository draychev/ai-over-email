package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"ai-over-email/fetch"
	"ai-over-email/internal/config"
	"ai-over-email/send"
)

type rpcRequest struct {
	JSONRPC string           `json:"jsonrpc"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params"`
	ID      *json.RawMessage `json:"id"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Result  interface{}      `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

type toolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type initializeResult struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    map[string]interface{} `json:"capabilities"`
	ServerInfo      map[string]string      `json:"serverInfo"`
}

type toolResult struct {
	Content []map[string]string `json:"content"`
	IsError bool                `json:"isError,omitempty"`
}

func main() {
	reader := bufio.NewScanner(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()

	for reader.Scan() {
		line := reader.Bytes()
		if len(line) == 0 {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			writeResponse(writer, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}

		switch req.Method {
		case "initialize":
			res := initializeResult{
				ProtocolVersion: "2024-11-05",
				Capabilities: map[string]interface{}{
					"tools": map[string]interface{}{},
				},
				ServerInfo: map[string]string{
					"name":    "gmail",
					"version": "0.1.0",
				},
			}
			writeResponse(writer, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: res})
		case "initialized":
			// notification, no response
		case "ping":
			writeResponse(writer, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{}})
		case "tools/list":
			writeResponse(writer, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"tools": toolsList()}})
		case "tools/call":
			result, err := handleToolCall(req.Params)
			if err != nil {
				writeResponse(writer, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32000, Message: err.Error()}})
				continue
			}
			writeResponse(writer, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
		default:
			writeResponse(writer, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found"}})
		}
	}
}

func toolsList() []tool {
	return []tool{
		{
			Name:        "list_emails",
			Description: "List emails from a mailbox (recent by default or all).",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"n": map[string]interface{}{
						"type":        "integer",
						"description": "Number of most recent emails to return.",
						"default":     10,
					},
					"all": map[string]interface{}{
						"type":        "boolean",
						"description": "Return all emails in the mailbox.",
						"default":     false,
					},
					"mailbox": map[string]interface{}{
						"type":        "string",
						"description": "Mailbox name (default INBOX).",
						"default":     "INBOX",
					},
				},
			},
		},
		{
			Name:        "search_emails",
			Description: "Search emails by a rough query over from/subject/body.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query text.",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Max number of results to return (default 50).",
						"default":     50,
					},
					"mailbox": map[string]interface{}{
						"type":        "string",
						"description": "Mailbox name (default INBOX).",
						"default":     "INBOX",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "send_email",
			Description: "Send an email.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"to": map[string]interface{}{
						"type":        "string",
						"description": "Recipient email address.",
					},
					"subject": map[string]interface{}{
						"type":        "string",
						"description": "Email subject.",
					},
					"body": map[string]interface{}{
						"type":        "string",
						"description": "Email body contents.",
					},
				},
				"required": []string{"to", "subject", "body"},
			},
		},
		{
			Name:        "delete_emails",
			Description: "Move emails to DELETED_BY_MCP (does not permanently delete).",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"uids": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "integer"},
						"description": "List of email UIDs to move.",
					},
					"mailbox": map[string]interface{}{
						"type":        "string",
						"description": "Mailbox name (default INBOX).",
						"default":     "INBOX",
					},
				},
				"required": []string{"uids"},
			},
		},
	}
}

func handleToolCall(raw json.RawMessage) (toolResult, error) {
	var params toolCallParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return toolResult{}, fmt.Errorf("invalid tool call params")
	}

	switch params.Name {
	case "list_emails":
		return handleListEmails(params.Arguments)
	case "search_emails":
		return handleSearchEmails(params.Arguments)
	case "send_email":
		return handleSendEmail(params.Arguments)
	case "delete_emails":
		return handleDeleteEmails(params.Arguments)
	default:
		return toolResult{}, fmt.Errorf("unknown tool: %s", params.Name)
	}
}

func handleListEmails(args map[string]interface{}) (toolResult, error) {
	configMap, err := config.Load(".env")
	if err != nil {
		return toolResult{}, err
	}

	n := 10
	if raw, ok := args["n"]; ok {
		if val, ok := raw.(float64); ok {
			n = int(val)
		}
	}

	all := false
	if raw, ok := args["all"]; ok {
		if val, ok := raw.(bool); ok {
			all = val
		}
	}

	mailbox := "INBOX"
	if raw, ok := args["mailbox"]; ok {
		if val, ok := raw.(string); ok && val != "" {
			mailbox = val
		}
	}

	cfg := fetch.Config{
		Server:   configMap["SERVER"],
		Username: configMap["USERNAME"],
		Password: configMap["PASSWORD"],
		Mailbox:  mailbox,
	}

	var results []fetch.Email
	if all {
		results, err = fetch.ListAll(cfg)
	} else {
		results, err = fetch.Recent(n, cfg)
	}
	if err != nil {
		return toolResult{}, err
	}

	payload, err := json.Marshal(results)
	if err != nil {
		return toolResult{}, err
	}

	return toolResult{Content: []map[string]string{{"type": "text", "text": string(payload)}}}, nil
}

func handleSearchEmails(args map[string]interface{}) (toolResult, error) {
	configMap, err := config.Load(".env")
	if err != nil {
		return toolResult{}, err
	}

	query, _ := args["query"].(string)
	if query == "" {
		return toolResult{}, fmt.Errorf("query is required")
	}

	limit := 50
	if raw, ok := args["limit"]; ok {
		if val, ok := raw.(float64); ok {
			limit = int(val)
		}
	}

	mailbox := "INBOX"
	if raw, ok := args["mailbox"]; ok {
		if val, ok := raw.(string); ok && val != "" {
			mailbox = val
		}
	}

	results, err := fetch.Search(query, limit, fetch.Config{
		Server:   configMap["SERVER"],
		Username: configMap["USERNAME"],
		Password: configMap["PASSWORD"],
		Mailbox:  mailbox,
	})
	if err != nil {
		return toolResult{}, err
	}

	payload, err := json.Marshal(results)
	if err != nil {
		return toolResult{}, err
	}

	return toolResult{Content: []map[string]string{{"type": "text", "text": string(payload)}}}, nil
}

func handleSendEmail(args map[string]interface{}) (toolResult, error) {
	configMap, err := config.Load(".env")
	if err != nil {
		return toolResult{}, err
	}

	to, _ := args["to"].(string)
	subject, _ := args["subject"].(string)
	body, _ := args["body"].(string)
	if to == "" || subject == "" || body == "" {
		return toolResult{}, fmt.Errorf("to, subject, and body are required")
	}

	cfg := send.Config{
		Server:   config.Value(configMap, "SMTP_SERVER", "smtp.gmail.com"),
		Port:     config.Value(configMap, "SMTP_PORT", "587"),
		Username: configMap["USERNAME"],
		Password: configMap["PASSWORD"],
		From:     config.Value(configMap, "FROM_EMAIL", ""),
	}

	if err := send.Send(to, subject, body, cfg); err != nil {
		return toolResult{}, err
	}

	return toolResult{Content: []map[string]string{{"type": "text", "text": "sent"}}}, nil
}

func handleDeleteEmails(args map[string]interface{}) (toolResult, error) {
	configMap, err := config.Load(".env")
	if err != nil {
		return toolResult{}, err
	}

	rawUids, ok := args["uids"].([]interface{})
	if !ok || len(rawUids) == 0 {
		return toolResult{}, fmt.Errorf("uids is required")
	}

	uids := make([]uint32, 0, len(rawUids))
	for _, raw := range rawUids {
		if val, ok := raw.(float64); ok && val > 0 {
			uids = append(uids, uint32(val))
		}
	}
	if len(uids) == 0 {
		return toolResult{}, fmt.Errorf("no valid uids provided")
	}

	mailbox := "INBOX"
	if raw, ok := args["mailbox"]; ok {
		if val, ok := raw.(string); ok && val != "" {
			mailbox = val
		}
	}

	moved, err := fetch.MoveToMailbox(uids, "DELETED_BY_MCP", fetch.Config{
		Server:   configMap["SERVER"],
		Username: configMap["USERNAME"],
		Password: configMap["PASSWORD"],
		Mailbox:  mailbox,
	})
	if err != nil {
		return toolResult{}, err
	}

	payload, err := json.Marshal(map[string]interface{}{"moved": moved})
	if err != nil {
		return toolResult{}, err
	}

	return toolResult{Content: []map[string]string{{"type": "text", "text": string(payload)}}}, nil
}

func writeResponse(writer *bufio.Writer, resp rpcResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_, _ = writer.Write(append(data, '\n'))
	_ = writer.Flush()
}
