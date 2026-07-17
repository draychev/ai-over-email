package usenet

import (
	"strings"
	"testing"

	appconfig "ai-over-email/pkg/config"
)

func TestFormatFollowupIncludesThreadHeaders(t *testing.T) {
	w := &Watcher{usenet: appconfig.UsenetConfig{
		Group:       "misc.pegasus",
		FromName:    "Pegasus AI",
		FromAddress: "pegasus-ai@example.com",
	}}
	original := article{
		MessageID:  "<post@example.com>",
		Subject:    "Question",
		References: []string{"<root@example.com>"},
	}

	raw, messageID, err := w.formatFollowup(original, "Answer body")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"From: \"Pegasus AI\" <pegasus-ai@example.com>",
		"Newsgroups: misc.pegasus",
		"Subject: Re: Question",
		"References: <root@example.com> <post@example.com>",
		"In-Reply-To: <post@example.com>",
		"X-AI-Over-Usenet: true",
		"\r\n\r\nAnswer body\r\n",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("follow-up missing %q in:\n%s", want, raw)
		}
	}
	if messageID == "" || !strings.Contains(raw, "Message-ID: "+messageID) {
		t.Fatalf("message ID not included: %q\n%s", messageID, raw)
	}
}

func TestFormatThreadContextIncludesFullArticles(t *testing.T) {
	thread := []article{
		{Number: 1, MessageID: "<one@example.com>", From: "One", Subject: "Root", Body: "Root body"},
		{Number: 2, MessageID: "<two@example.com>", From: "Two", Subject: "Re: Root", Body: "Follow-up body"},
	}

	got := formatThreadContext(thread)
	for _, want := range []string{"Article 1", "Message-ID: <one@example.com>", "Root body", "---", "Follow-up body"} {
		if !strings.Contains(got, want) {
			t.Fatalf("thread context missing %q in %q", want, got)
		}
	}
}

func TestIsLeafnodePlaceholder(t *testing.T) {
	if !isLeafnodePlaceholder(article{MessageID: "<leafnode:placeholder:misc.pegasus@example.com>"}) {
		t.Fatalf("placeholder Message-ID was not detected")
	}
	if !isLeafnodePlaceholder(article{Subject: "Leafnode placeholder for group misc.pegasus"}) {
		t.Fatalf("placeholder subject was not detected")
	}
	if isLeafnodePlaceholder(article{MessageID: "<real@example.com>", Subject: "Real post"}) {
		t.Fatalf("real article detected as placeholder")
	}
}
