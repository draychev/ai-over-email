package email

import "testing"

func TestExtractDecryptedAttachments(t *testing.T) {
	plaintext := "Content-Type: multipart/mixed; boundary=\"mixed\"\r\n" +
		"\r\n" +
		"--mixed\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Body text\r\n" +
		"--mixed\r\n" +
		"Content-Type: image/png; name=\"photo.png\"\r\n" +
		"Content-Disposition: attachment; filename=\"photo.png\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		"AQID\r\n" +
		"--mixed--\r\n"

	attachments := extractDecryptedAttachments(plaintext)
	if len(attachments) != 1 {
		t.Fatalf("attachments length = %d, want 1: %#v", len(attachments), attachments)
	}
	if attachments[0].Name != "photo.png" {
		t.Fatalf("name = %q, want photo.png", attachments[0].Name)
	}
	if attachments[0].Type != "image/png" {
		t.Fatalf("type = %q, want image/png", attachments[0].Type)
	}
	if string(attachments[0].Data) != "\x01\x02\x03" {
		t.Fatalf("data = %#v", attachments[0].Data)
	}
}

func TestExtractDecryptedAttachmentsSkipsPGPSignature(t *testing.T) {
	plaintext := "Content-Type: multipart/signed; protocol=\"application/pgp-signature\"; boundary=\"sig\"\r\n" +
		"\r\n" +
		"--sig\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Body text\r\n" +
		"--sig\r\n" +
		"Content-Type: application/pgp-signature; name=\"signature.asc\"\r\n" +
		"Content-Disposition: attachment; filename=\"signature.asc\"\r\n" +
		"\r\n" +
		"ignored\r\n" +
		"--sig--\r\n"

	if attachments := extractDecryptedAttachments(plaintext); len(attachments) != 0 {
		t.Fatalf("attachments = %#v, want empty", attachments)
	}
}
