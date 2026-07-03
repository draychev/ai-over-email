package email

import "testing"

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
