package email

import (
	"strings"
	"testing"
)

func TestSkipAutoReplyReasonSkipsSelfSender(t *testing.T) {
	self := testAddress("self", "mail.test")
	w := &Watcher{creds: Credentials{Username: self}}
	msg := emailMessage{From: []emailAddress{{Email: self}}}

	if got := w.skipAutoReplyReason(msg); got != "self_sender" {
		t.Fatalf("skipAutoReplyReason = %q, want self_sender", got)
	}
}

func TestSkipAutoReplyReasonSkipsAutomatedSenders(t *testing.T) {
	w := &Watcher{creds: Credentials{Username: testAddress("self", "mail.test")}}
	tests := []string{
		testAddress("keyserver", "keys.openpgp.org"),
		testAddress("no-reply", "mail.test"),
		testAddress("noreply", "mail.test"),
		testAddress("do-not-reply", "mail.test"),
		testAddress("mailer-daemon", "mail.test"),
		testAddress("postmaster", "mail.test"),
	}

	for _, address := range tests {
		msg := emailMessage{From: []emailAddress{{Email: address}}}
		if got := w.skipAutoReplyReason(msg); got != "automated_sender" {
			t.Fatalf("skipAutoReplyReason(%q) = %q, want automated_sender", address, got)
		}
	}
}

func TestSkipAutoReplyReasonAllowsNormalSender(t *testing.T) {
	w := &Watcher{creds: Credentials{Username: testAddress("self", "mail.test")}}
	msg := emailMessage{From: []emailAddress{{Email: testAddress("sender", "mail.test")}}}

	if got := w.skipAutoReplyReason(msg); got != "" {
		t.Fatalf("skipAutoReplyReason = %q, want empty", got)
	}
}

func TestReplySubjectAddsReplyPrefix(t *testing.T) {
	if got := replySubject("Normal subject"); got != "Re: Normal subject" {
		t.Fatalf("replySubject = %q", got)
	}
}

func TestReplySubjectPreservesExistingReplyPrefix(t *testing.T) {
	if got := replySubject("re: Normal subject"); got != "re: Normal subject" {
		t.Fatalf("replySubject = %q", got)
	}
}

func TestReplyReferencesCombinesAndDedupesMessageIDs(t *testing.T) {
	parent := "<" + testAddress("parent", "mail.test") + ">"
	original := "<" + testAddress("original", "mail.test") + ">"
	newID := "<" + testAddress("new", "mail.test") + ">"
	got := replyReferences([]string{parent, original}, []string{original, newID})
	want := []string{parent, original, newID}
	if len(got) != len(want) {
		t.Fatalf("replyReferences length = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("replyReferences[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFormatReplyBodyQuotesOriginalMessage(t *testing.T) {
	sender := testAddress("sender", "mail.test")
	original := emailMessage{
		From:   []emailAddress{{Name: "Sender", Email: sender}},
		SentAt: "2026-06-29T21:06:46Z",
	}

	got := formatReplyBody("Hello,\n\nDone.", original, "Please do this.\n\nThanks.")
	want := "Hello,\n\nDone.\n\nOn Mon, 29 Jun 2026 21:06 UTC, Sender <" + sender + "> wrote:\n> Please do this.\n>\n> Thanks."
	if got != want {
		t.Fatalf("formatReplyBody = %q, want %q", got, want)
	}
}

func TestFormatReplyBodyOmitsQuoteWhenOriginalBodyEmpty(t *testing.T) {
	original := emailMessage{From: []emailAddress{{Email: testAddress("sender", "mail.test")}}}

	got := formatReplyBody("Hello,\n", original, "   ")
	want := "Hello,"
	if got != want {
		t.Fatalf("formatReplyBody = %q, want %q", got, want)
	}
}

func TestFormatReplyHTMLBodyRendersMarkdownAndQuotesOriginal(t *testing.T) {
	sender := testAddress("sender", "mail.test")
	original := emailMessage{
		From:   []emailAddress{{Name: "Sender", Email: sender}},
		SentAt: "2026-06-29T21:06:46Z",
	}

	got, err := formatReplyHTMLBody("Hello **there**.\n\n- one\n- two", original, "Please <check> this.")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<p>Hello <strong>there</strong>.</p>",
		"<li>one</li>",
		"<blockquote type=\"cite\">",
		"On Mon, 29 Jun 2026 21:06 UTC, Sender &lt;" + sender + "&gt; wrote:",
		"Please &lt;check&gt; this.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatReplyHTMLBody missing %q in %q", want, got)
		}
	}
}

func TestFormatReplyHTMLBodyRendersMarkdownTablesAsHTMLTables(t *testing.T) {
	original := emailMessage{}

	got, err := formatReplyHTMLBody("| Category | ESV | GNT |\n|---|---|---|\n| Style | Formal | Conversational |", original, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<table>",
		"<th>Category</th>",
		"<td>Formal</td>",
		"<td>Conversational</td>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatReplyHTMLBody missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "| Category |") {
		t.Fatalf("formatReplyHTMLBody leaked raw markdown table in %q", got)
	}
}

func TestFormatReplyHTMLBodyOmitsQuoteWhenOriginalBodyEmpty(t *testing.T) {
	original := emailMessage{From: []emailAddress{{Email: testAddress("sender", "mail.test")}}}

	got, err := formatReplyHTMLBody("Hello **there**.", original, "   ")
	if err != nil {
		t.Fatal(err)
	}
	want := "<p>Hello <strong>there</strong>.</p>"
	if got != want {
		t.Fatalf("formatReplyHTMLBody = %q, want %q", got, want)
	}
}

func TestReplyQuoteDateFallsBackToReceivedAt(t *testing.T) {
	original := emailMessage{ReceivedAt: "2026-06-29T21:06:46.123Z"}

	got := replyQuoteDate(original)
	want := "Mon, 29 Jun 2026 21:06 UTC"
	if got != want {
		t.Fatalf("replyQuoteDate = %q, want %q", got, want)
	}
}

func TestAppendResponseFooterText(t *testing.T) {
	got := appendResponseFooterText("Answer.", emailFooterStats{
		TokensUsed:        123,
		TotalEmailsEver:   45,
		RemainingToday:    6,
		DailyMessageLimit: 10,
	})

	for _, want := range []string{
		"Answer.\n\n---\n",
		"Tokens used for this email: 123",
		"Total emails sent by this service: 45",
		"Messages remaining today: 6 of 10",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("footer text missing %q in %q", want, got)
		}
	}
}

func TestAppendResponseFooterHTML(t *testing.T) {
	got := appendResponseFooterHTML("<p>Answer.</p>", emailFooterStats{
		TokensUsed:        123,
		TotalEmailsEver:   45,
		RemainingToday:    6,
		DailyMessageLimit: 10,
	})

	for _, want := range []string{
		"<p>Answer.</p>\n<hr>",
		"Tokens used for this email: 123",
		"Total emails sent by this service: 45",
		"Messages remaining today: 6 of 10",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("footer HTML missing %q in %q", want, got)
		}
	}
}
