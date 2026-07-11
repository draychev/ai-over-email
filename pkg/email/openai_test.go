package email

import (
	appconfig "ai-over-email/pkg/config"
	"strings"
	"testing"
)

func TestOpenAIResponseOutputText(t *testing.T) {
	response := openAIResponse{
		Output: []openAIOutputItem{
			{Type: "reasoning"},
			{Type: "message", Content: []openAIOutputContent{
				{Type: "output_text", Text: "hello"},
				{Type: "output_text", Text: "world"},
			}},
		},
	}

	if got := response.outputText(); got != "hello\nworld" {
		t.Fatalf("outputText() = %q", got)
	}
}

func TestOpenAIUsageAddsTokenCounts(t *testing.T) {
	first := openAIUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15, ReasoningTokens: 2, CachedInputTokens: 3}
	second := openAIUsage{InputTokens: 7, OutputTokens: 4, TotalTokens: 11, ReasoningTokens: 1, CachedInputTokens: 2}

	got := first.add(second)
	if got.InputTokens != 17 || got.OutputTokens != 9 || got.TotalTokens != 26 || got.ReasoningTokens != 3 || got.CachedInputTokens != 5 {
		t.Fatalf("add = %#v", got)
	}
}

func TestOpenAIResponseFunctionCalls(t *testing.T) {
	response := openAIResponse{
		Output: []openAIOutputItem{
			{Type: "message"},
			{Type: "function_call", Name: "web_search", CallID: "call_1", Arguments: `{"query":"x","count":3}`},
		},
	}

	calls := response.functionCalls()
	if len(calls) != 1 {
		t.Fatalf("functionCalls length = %d", len(calls))
	}
	if calls[0].Name != "web_search" || calls[0].CallID != "call_1" {
		t.Fatalf("function call = %#v", calls[0])
	}
}

func TestOpenAIResponseToolsUsed(t *testing.T) {
	response := openAIResponse{
		Output: []openAIOutputItem{
			{Type: "function_call", Name: "web_search", CallID: "call_1"},
			{Type: "web_search_call"},
			{Type: "function_call", Name: "web_search", CallID: "call_2"},
		},
	}

	got := response.toolsUsed()
	if len(got) != 1 || got[0] != "web_search" {
		t.Fatalf("toolsUsed = %#v, want web_search once", got)
	}
}

func TestOpenAIUserContentTreatsSubjectAsContextOnly(t *testing.T) {
	content := openAIUserContent("Status request", "", nil)

	text, _ := content[0]["text"].(string)
	for _, want := range []string{"subject for context only", "Do not copy or restate the subject line"} {
		if !strings.Contains(text, want) {
			t.Fatalf("user content missing %q: %q", want, text)
		}
	}
	if !strings.Contains(text, "Incoming email body, including any forwarded message") {
		t.Fatalf("empty body was not kept separate from subject: %q", text)
	}
}

func TestOpenAIUserContentEmphasizesForwardedAndThreadContext(t *testing.T) {
	content := openAIUserContent("Fwd: please review", "Please answer this.\n\n---------- Forwarded message ---------\nImportant forwarded context.", nil)

	text, _ := content[0]["text"].(string)
	for _, want := range []string{
		"including any forwarded message, quoted previous thread",
		"read and use the full body above",
		"forwarded or quoted material",
		"prior thread context",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("user content missing %q: %q", want, text)
		}
	}
	if !strings.Contains(text, "Important forwarded context.") {
		t.Fatalf("user content did not include forwarded body: %q", text)
	}
}

func TestOpenAIToolsUseHostedSearchWithoutBraveToken(t *testing.T) {
	client := &openAIClient{}

	tools, choice, mode := client.openAITools()
	if choice != "auto" || mode != "openai_web_search" {
		t.Fatalf("choice/mode = %q/%q", choice, mode)
	}
	if len(tools) != 1 || tools[0]["type"] != "web_search" {
		t.Fatalf("tools = %#v", tools)
	}
}

func TestOpenAIToolsUseBraveFunctionWithToken(t *testing.T) {
	client := &openAIClient{braveSearchToken: "token"}

	tools, choice, mode := client.openAITools()
	if choice != "auto" || mode != "brave_function" {
		t.Fatalf("choice/mode = %q/%q", choice, mode)
	}
	if len(tools) != 1 || tools[0]["type"] != "function" || tools[0]["name"] != "web_search" {
		t.Fatalf("tools = %#v", tools)
	}
}

func TestFinalOpenAIRequestPayloadOmitsTools(t *testing.T) {
	client := &openAIClient{braveSearchToken: "token"}

	payload := client.finalOpenAIRequestPayload([]map[string]any{{"type": "function_call_output"}}, "resp_1", appconfig.OpenAIModelSettings{
		Model:           "gpt-powerful",
		ReasoningEffort: "medium",
	})
	if _, ok := payload["tools"]; ok {
		t.Fatalf("final payload unexpectedly included tools: %#v", payload)
	}
	if payload["model"] != "gpt-powerful" {
		t.Fatalf("model = %#v", payload["model"])
	}
	reasoning, ok := payload["reasoning"].(map[string]string)
	if !ok || reasoning["effort"] != "medium" {
		t.Fatalf("reasoning = %#v", payload["reasoning"])
	}
	if payload["previous_response_id"] != "resp_1" {
		t.Fatalf("previous_response_id = %#v", payload["previous_response_id"])
	}
	if payload["_search_mode"] != "brave_function_final" {
		t.Fatalf("_search_mode = %#v", payload["_search_mode"])
	}
	input, ok := payload["input"].([]map[string]any)
	if !ok {
		t.Fatalf("input type = %T", payload["input"])
	}
	if len(input) != 2 {
		t.Fatalf("input length = %d, want 2: %#v", len(input), input)
	}
	instruction, _ := input[1]["content"].(string)
	for _, want := range []string{"Do not output raw JSON", "Write the final outgoing email reply now"} {
		if !strings.Contains(instruction, want) {
			t.Fatalf("final instruction missing %q: %q", want, instruction)
		}
	}
}

func TestOpenAIRequestPayloadUsesConfiguredModelSettings(t *testing.T) {
	client := &openAIClient{}

	payload := client.openAIRequestPayload([]map[string]any{{"role": "user", "content": "hello"}}, "", appconfig.OpenAIModelSettings{
		Model:           "gpt-powerful",
		ReasoningEffort: "medium",
	})

	if payload["model"] != "gpt-powerful" {
		t.Fatalf("model = %#v", payload["model"])
	}
	reasoning, ok := payload["reasoning"].(map[string]string)
	if !ok || reasoning["effort"] != "medium" {
		t.Fatalf("reasoning = %#v", payload["reasoning"])
	}
}

func TestOpenAIRequestPayloadDefaultsModelSettings(t *testing.T) {
	client := &openAIClient{}

	payload := client.openAIRequestPayload([]map[string]any{{"role": "user", "content": "hello"}}, "", appconfig.OpenAIModelSettings{})

	if payload["model"] != appconfig.DefaultOpenAIModel {
		t.Fatalf("model = %#v", payload["model"])
	}
	reasoning, ok := payload["reasoning"].(map[string]string)
	if !ok || reasoning["effort"] != appconfig.DefaultOpenAIReasoningEffort {
		t.Fatalf("reasoning = %#v", payload["reasoning"])
	}
}

func TestBraveSearchFollowupInputAddsProseInstruction(t *testing.T) {
	input := []map[string]any{{"type": "function_call_output", "output": `{"query":"x"}`}}

	got, ok := braveSearchFollowupInput(input, false).([]map[string]any)
	if !ok {
		t.Fatalf("followup input type mismatch")
	}
	if len(got) != 2 {
		t.Fatalf("followup input length = %d, want 2", len(got))
	}
	instruction, _ := got[1]["content"].(string)
	for _, want := range []string{"Use the web_search JSON results above only as source context", "Do not output raw JSON", "write the final email"} {
		if !strings.Contains(instruction, want) {
			t.Fatalf("followup instruction missing %q: %q", want, instruction)
		}
	}
}

func TestFormatBraveSearchResults(t *testing.T) {
	got := formatBraveSearchResults("latest go release", []braveSearchResult{
		{Title: " Go releases ", URL: " https://go.dev/doc/devel/release ", Description: "Release notes", Age: "1 day ago"},
		{Title: "", URL: "https://example.invalid"},
	})

	for _, want := range []string{"latest go release", "Go releases", "https://go.dev/doc/devel/release", "Release notes"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted Brave results missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "example.invalid") {
		t.Fatalf("formatted Brave results included incomplete result: %s", got)
	}
}

func TestExtractEmailBody(t *testing.T) {
	msg := emailMessage{
		TextBody: []emailBodyPart{{PartID: "part1", Type: "text/plain"}},
		BodyValues: map[string]emailBodyValue{
			"part1": {Value: "body text"},
		},
	}

	if got := extractEmailBody(msg); got != "body text" {
		t.Fatalf("extractEmailBody() = %q", got)
	}
}

func TestOpenAIUserContentIncludesImageAttachment(t *testing.T) {
	content := openAIUserContent("Subject", "Body", []emailAttachment{{
		Name: "photo.png",
		Type: "image/png",
		Data: []byte{1, 2, 3},
	}})

	if len(content) != 3 {
		t.Fatalf("content length = %d, want 3: %#v", len(content), content)
	}
	if content[1]["text"] != "Attachment 1: photo.png (image/png, 3 bytes)" {
		t.Fatalf("attachment label = %#v", content[1]["text"])
	}
	if content[2]["type"] != "input_image" {
		t.Fatalf("attachment type = %#v, want input_image", content[2]["type"])
	}
	if content[2]["image_url"] != "data:image/png;base64,AQID" {
		t.Fatalf("image_url = %#v", content[2]["image_url"])
	}
}

func TestOpenAIUserContentIncludesFileAttachment(t *testing.T) {
	content := openAIUserContent("Subject", "Body", []emailAttachment{{
		Name: "notes.txt",
		Type: "text/plain",
		Data: []byte("hello"),
	}})

	if len(content) != 3 {
		t.Fatalf("content length = %d, want 3: %#v", len(content), content)
	}
	if content[2]["type"] != "input_file" {
		t.Fatalf("attachment type = %#v, want input_file", content[2]["type"])
	}
	if content[2]["filename"] != "notes.txt" {
		t.Fatalf("filename = %#v", content[2]["filename"])
	}
	if content[2]["file_data"] != "data:text/plain;base64,aGVsbG8=" {
		t.Fatalf("file_data = %#v", content[2]["file_data"])
	}
}
