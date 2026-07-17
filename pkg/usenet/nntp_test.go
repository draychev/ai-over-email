package usenet

import (
	"strings"
	"testing"
)

func TestParseArticle(t *testing.T) {
	raw := "Message-ID: <one@example.com>\r\nSubject: Test\r\nFrom: Sender <sender@example.com>\r\nReferences: <root@example.com> <parent@example.com>\r\n\r\nBody text\r\n"

	article, err := parseArticle(7, raw)
	if err != nil {
		t.Fatal(err)
	}
	if article.Number != 7 || article.MessageID != "<one@example.com>" || article.Subject != "Test" {
		t.Fatalf("article = %#v", article)
	}
	if article.Body != "Body text" {
		t.Fatalf("Body = %q", article.Body)
	}
	if strings.Join(article.References, " ") != "<root@example.com> <parent@example.com>" {
		t.Fatalf("References = %#v", article.References)
	}
}

func TestParseReferencesDedupes(t *testing.T) {
	got := parseReferences(" <one@example.com> <two@example.com> <one@example.com> ")
	want := "<one@example.com> <two@example.com>"
	if strings.Join(got, " ") != want {
		t.Fatalf("parseReferences = %#v, want %s", got, want)
	}
}
