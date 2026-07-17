package email

import "testing"

func TestExtractBodyText(t *testing.T) {
	parts := []emailBodyPart{{PartID: "one"}, {PartID: "two"}}
	values := map[string]emailBodyValue{
		"one": {Value: "Hello"},
		"two": {Value: "World"},
	}

	got := extractBodyText(parts, values)
	if got != "Hello\n\nWorld" {
		t.Fatalf("extractBodyText() = %q", got)
	}
}

func TestTruncatePreview(t *testing.T) {
	got := truncatePreview("a  b   c", 4)
	if got != "a b…" {
		t.Fatalf("truncatePreview() = %q", got)
	}
}

func TestMailboxNamesForIDsSorted(t *testing.T) {
	inspector := &Inspector{
		mailboxes: []mailbox{
			{ID: "x", Name: "Inbox"},
			{ID: "y", Name: "Archive"},
		},
	}

	got := inspector.mailboxNamesForIDs(map[string]bool{"x": true, "y": true})
	if len(got) != 2 || got[0] != "Archive" || got[1] != "Inbox" {
		t.Fatalf("mailboxNamesForIDs() = %#v", got)
	}
}
