package email

import (
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

	payload := client.finalOpenAIRequestPayload([]map[string]any{{"type": "function_call_output"}}, "resp_1")
	if _, ok := payload["tools"]; ok {
		t.Fatalf("final payload unexpectedly included tools: %#v", payload)
	}
	if payload["previous_response_id"] != "resp_1" {
		t.Fatalf("previous_response_id = %#v", payload["previous_response_id"])
	}
	if payload["_search_mode"] != "brave_function_final" {
		t.Fatalf("_search_mode = %#v", payload["_search_mode"])
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
