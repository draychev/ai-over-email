package email

import (
	"strings"
	"testing"
)

func TestExtractPGPEncryptedPayloadInlineArmor(t *testing.T) {
	payload, ok := extractPGPEncryptedPayload(nil, "hello\n-----BEGIN PGP MESSAGE-----\nabc\n-----END PGP MESSAGE-----\nbye")
	if !ok {
		t.Fatal("expected inline armored payload")
	}
	if got := string(payload); got != "-----BEGIN PGP MESSAGE-----\nabc\n-----END PGP MESSAGE-----" {
		t.Fatalf("payload = %q", got)
	}
}

func TestExtractPGPEncryptedPayloadPGPMIME(t *testing.T) {
	raw := []byte("From: " + testAddress("sender", "mail.test") + "\r\n" +
		"Content-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"; boundary=\"b\"\r\n" +
		"\r\n" +
		"--b\r\n" +
		"Content-Type: application/pgp-encrypted\r\n" +
		"\r\n" +
		"Version: 1\r\n" +
		"--b\r\n" +
		"Content-Type: application/octet-stream\r\n" +
		"\r\n" +
		"-----BEGIN PGP MESSAGE-----\r\nabc\r\n-----END PGP MESSAGE-----\r\n" +
		"--b--\r\n")

	payload, ok := extractPGPEncryptedPayload(raw, "")
	if !ok {
		t.Fatal("expected PGP/MIME encrypted payload")
	}
	if got := string(payload); got != "-----BEGIN PGP MESSAGE-----\r\nabc\r\n-----END PGP MESSAGE-----" {
		t.Fatalf("payload = %q", got)
	}
}

func TestPGPRequiredReplyReason(t *testing.T) {
	recipient := testAddress("recipient", "mail.test")
	reply := pgpRequiredReply("not_signed", recipient)
	if !containsAll(reply, "did not include a valid OpenPGP signature", "keys.openpgp.org", recipient) {
		t.Fatalf("reply did not include expected guidance: %q", reply)
	}
}

func TestPGPRequiredReplyExplainsUnverifiableSignature(t *testing.T) {
	reply := pgpRequiredReply("signature_not_verifiable", "")
	if !containsAll(reply, "retrieve the sender's public key", "email address on the signing key", "From address", "publish and verify") {
		t.Fatalf("reply did not explain unverifiable signature: %q", reply)
	}
}

func TestPlaintextSenderAllowed(t *testing.T) {
	first := testAddress("first", "mail.test")
	second := testAddress("second", "mail.test")
	tests := []struct {
		name      string
		addresses []emailAddress
		allowlist []string
		want      bool
	}{
		{
			name:      "allows first configured address",
			addresses: []emailAddress{{Email: " " + strings.ToUpper(first) + " "}},
			allowlist: []string{first},
			want:      true,
		},
		{
			name:      "allows second configured address",
			addresses: []emailAddress{{Email: second}},
			allowlist: []string{first, second},
			want:      true,
		},
		{
			name:      "rejects other sender",
			addresses: []emailAddress{{Email: testAddress("other", "mail.test")}},
			allowlist: []string{first},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := plaintextSenderAllowed(tt.addresses, tt.allowlist); got != tt.want {
				t.Fatalf("plaintextSenderAllowed = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestDecryptVerifiedEmailAcceptsAllowlistedPlaintext(t *testing.T) {
	sender := testAddress("allowed", "mail.test")
	w := &Watcher{creds: Credentials{PlaintextAllowlist: []string{sender}}}
	msg := emailMessage{
		From:     []emailAddress{{Email: sender}},
		TextBody: []emailBodyPart{{PartID: "text", Type: "text/plain"}},
		BodyValues: map[string]emailBodyValue{
			"text": {Value: "plain request"},
		},
	}

	body, subject, attachments, rejectReason, err := w.decryptVerifiedEmail(nil, msg)
	if err != nil {
		t.Fatal(err)
	}
	if body != "plain request" {
		t.Fatalf("body = %q, want plain request", body)
	}
	if subject != "" {
		t.Fatalf("subject = %q, want empty", subject)
	}
	if len(attachments) != 0 {
		t.Fatalf("attachments = %#v, want empty", attachments)
	}
	if rejectReason != "" {
		t.Fatalf("rejectReason = %q, want empty", rejectReason)
	}
}

func TestDecryptVerifiedEmailRejectsOtherPlaintext(t *testing.T) {
	w := &Watcher{}
	msg := emailMessage{
		From:     []emailAddress{{Email: testAddress("other", "mail.test")}},
		TextBody: []emailBodyPart{{PartID: "text", Type: "text/plain"}},
		BodyValues: map[string]emailBodyValue{
			"text": {Value: "plain request"},
		},
	}

	_, _, _, rejectReason, err := w.decryptVerifiedEmail(nil, msg)
	if err != nil {
		t.Fatal(err)
	}
	if rejectReason != "not_encrypted" {
		t.Fatalf("rejectReason = %q, want not_encrypted", rejectReason)
	}
}

func TestExtractDecryptedTextPlaintext(t *testing.T) {
	got := extractDecryptedText("  do the thing\n")
	if got != "do the thing" {
		t.Fatalf("extractDecryptedText = %q", got)
	}
}

func TestExtractDecryptedTextNestedMultipartSigned(t *testing.T) {
	plaintext := "Content-Type: multipart/signed; protocol=\"application/pgp-signature\"; boundary=\"sig\"\r\n" +
		"Subject: =?utf-8?q?Normal_subject_test?=\r\n" +
		"\r\n" +
		"--sig\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n" +
		"\r\n" +
		"Please summarize this page.=0AThanks.\r\n" +
		"--sig\r\n" +
		"Content-Type: application/pgp-signature\r\n" +
		"\r\n" +
		"ignored-signature\r\n" +
		"--sig--\r\n"

	got := extractDecryptedText(plaintext)
	if got != "Please summarize this page.\nThanks." {
		t.Fatalf("extractDecryptedText = %q", got)
	}
	if got := extractDecryptedSubject(plaintext); got != "Normal subject test" {
		t.Fatalf("extractDecryptedSubject = %q", got)
	}
}

func TestExtractDecryptedSubjectMissingSubject(t *testing.T) {
	plaintext := "Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Body only\r\n"

	if got := extractDecryptedSubject(plaintext); got != "" {
		t.Fatalf("extractDecryptedSubject = %q", got)
	}
}

func TestExtractDecryptedTextMultipartAlternative(t *testing.T) {
	plaintext := "Content-Type: multipart/alternative; boundary=\"alt\"\r\n" +
		"\r\n" +
		"--alt\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<p>HTML fallback</p>\r\n" +
		"--alt\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Plain body\r\n" +
		"--alt--\r\n"

	got := extractDecryptedText(plaintext)
	if got != "Plain body" {
		t.Fatalf("extractDecryptedText = %q", got)
	}
}

func TestExtractDecryptedTextSkipsPGPPlaceholder(t *testing.T) {
	plaintext := "Content-Type: multipart/mixed; boundary=\"mixed\"\r\n" +
		"\r\n" +
		"--mixed\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"This is an OpenPGP/MIME encrypted message (RFC 4880 and 3156).\r\n" +
		"--mixed\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Real instruction body\r\n" +
		"--mixed--\r\n"

	got := extractDecryptedText(plaintext)
	if got != "Real instruction body" {
		t.Fatalf("extractDecryptedText = %q", got)
	}
}

func TestExtractDecryptedTextReturnsEmptyForPlaceholderOnlyMIME(t *testing.T) {
	plaintext := "Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"This is an OpenPGP/MIME encrypted message (RFC 4880 and 3156).\r\n"

	got := extractDecryptedText(plaintext)
	if got != "" {
		t.Fatalf("extractDecryptedText = %q", got)
	}
}

func TestSenderEmailsNormalizesAndDedupes(t *testing.T) {
	alice := testAddress("alice", "mail.test")
	bob := testAddress("bob", "mail.test")
	got := senderEmails([]emailAddress{
		{Name: "A", Email: " " + strings.ToUpper(alice) + " "},
		{Name: "Empty"},
		{Name: "Duplicate", Email: alice},
		{Name: "B", Email: bob},
	})
	want := []string{alice, bob}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("senderEmails = %#v, want %#v", got, want)
	}
}

func containsAll(text string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(text, needle) {
			return false
		}
	}
	return true
}
