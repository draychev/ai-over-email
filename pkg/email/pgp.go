package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"os/exec"
	"strings"
	"time"
)

const (
	pgpArmorBegin = "-----BEGIN PGP MESSAGE-----"
	pgpArmorEnd   = "-----END PGP MESSAGE-----"
)

func (w *Watcher) decryptVerifiedEmail(ctx context.Context, msg emailMessage) (string, string, []emailAttachment, string, error) {
	raw := []byte(nil)
	if msg.BlobID != "" {
		var err error
		raw, err = w.client.Download(ctx, w.accountID, msg.BlobID, "message.eml", "message/rfc822")
		if err != nil {
			return "", "", nil, "", err
		}
	}

	payload, ok := extractPGPEncryptedPayload(raw, extractEmailBody(msg))
	if !ok {
		if plaintextSenderAllowed(msg.From, w.creds.PlaintextAllowlist) {
			body := extractEmailBody(msg)
			attachments, err := w.fetchAttachments(ctx, msg.Attachments)
			if err != nil {
				return "", "", nil, "", err
			}
			w.logf("plaintext sender accepted by allowlist: from=%q body_bytes=%d attachments=%d", formatFrom(msg.From), len(body), len(attachments))
			return body, "", attachments, "", nil
		}
		return "", "", nil, "not_encrypted", nil
	}

	plaintext, rejectReason, err := decryptSignedPGP(ctx, payload, senderEmails(msg.From))
	if err != nil {
		return "", "", nil, "", err
	}
	if rejectReason != "" {
		return "", "", nil, rejectReason, nil
	}
	body := extractDecryptedText(plaintext)
	subject := extractDecryptedSubject(plaintext)
	attachments := extractDecryptedAttachments(plaintext)
	w.logf("PGP decrypt accepted: decrypted_bytes=%d extracted_body_bytes=%d protected_subject_present=%t attachments=%d", len(plaintext), len(body), subject != "", len(attachments))
	return body, subject, attachments, "", nil
}

func plaintextSenderAllowed(addresses []emailAddress, allowlist []string) bool {
	allowed := make(map[string]struct{}, len(allowlist))
	for _, email := range allowlist {
		email = strings.ToLower(strings.TrimSpace(email))
		if email != "" {
			allowed[email] = struct{}{}
		}
	}
	for _, email := range senderEmails(addresses) {
		if _, ok := allowed[email]; ok {
			return true
		}
	}
	return false
}

func extractPGPEncryptedPayload(raw []byte, fallbackText string) ([]byte, bool) {
	if armor := extractPGPArmor(fallbackText); armor != "" {
		return []byte(armor), true
	}
	if len(raw) == 0 {
		return nil, false
	}
	if armor := extractPGPArmor(string(raw)); armor != "" {
		return []byte(armor), true
	}

	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, false
	}
	return extractPGPPart(msg.Header, msg.Body)
}

func extractPGPPart(header mail.Header, body io.Reader) ([]byte, bool) {
	mediaType, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil {
		mediaType = "text/plain"
	}
	mediaType = strings.ToLower(mediaType)

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return nil, false
		}
		reader := multipart.NewReader(body, boundary)
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				return nil, false
			}
			if err != nil {
				return nil, false
			}
			payload, ok := extractPGPPart(mail.Header(part.Header), part)
			_ = part.Close()
			if ok {
				return payload, true
			}
		}
	}

	data, err := readMIMEBody(header, body)
	if err != nil {
		return nil, false
	}
	if armor := extractPGPArmor(string(data)); armor != "" {
		return []byte(armor), true
	}
	if mediaType == "application/octet-stream" || mediaType == "application/pgp-message" {
		if len(bytes.TrimSpace(data)) > 0 && !bytes.Equal(bytes.TrimSpace(data), []byte("Version: 1")) {
			return data, true
		}
	}
	return nil, false
}

func readMIMEBody(header mail.Header, body io.Reader) ([]byte, error) {
	encoding := strings.ToLower(strings.TrimSpace(header.Get("Content-Transfer-Encoding")))
	switch encoding {
	case "base64":
		return io.ReadAll(io.LimitReader(base64.NewDecoder(base64.StdEncoding, body), 25*1024*1024))
	case "quoted-printable":
		return io.ReadAll(io.LimitReader(quotedprintable.NewReader(body), 25*1024*1024))
	default:
		return io.ReadAll(io.LimitReader(body, 25*1024*1024))
	}
}

func extractPGPArmor(text string) string {
	start := strings.Index(text, pgpArmorBegin)
	if start == -1 {
		return ""
	}
	end := strings.Index(text[start:], pgpArmorEnd)
	if end == -1 {
		return ""
	}
	end += start + len(pgpArmorEnd)
	return strings.TrimSpace(text[start:end])
}

func decryptSignedPGP(ctx context.Context, payload []byte, signerEmails []string) (string, string, error) {
	plaintext, status, err := runGPGDecrypt(ctx, payload)
	if err != nil && signatureNeedsPublicKey(status) && len(signerEmails) > 0 {
		if locateSigningKeys(ctx, signerEmails) == nil {
			plaintext, status, err = runGPGDecrypt(ctx, payload)
		}
	}
	return interpretGPGDecrypt(plaintext, status, err)
}

func runGPGDecrypt(ctx context.Context, payload []byte) (string, string, error) {
	gpgCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(
		gpgCtx,
		"gpg",
		"--batch",
		"--yes",
		"--keyserver",
		"hkps://keys.openpgp.org",
		"--auto-key-retrieve",
		"--status-fd=2",
		"--decrypt",
	)
	cmd.Stdin = bytes.NewReader(payload)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	status := stderr.String()
	if gpgCtx.Err() != nil {
		return "", status, gpgCtx.Err()
	}
	return stdout.String(), status, err
}

func interpretGPGDecrypt(plaintext string, status string, err error) (string, string, error) {
	if err != nil {
		if signatureNeedsPublicKey(status) {
			return "", "signature_not_verifiable", nil
		}
		if strings.Contains(status, "NO_SECKEY") {
			return "", "not_encrypted_to_recipient", nil
		}
		return "", "pgp_decrypt_failed", nil
	}
	if !strings.Contains(status, "GOODSIG") && !strings.Contains(status, "VALIDSIG") {
		return "", "not_signed", nil
	}

	return strings.TrimSpace(plaintext), "", nil
}

func extractDecryptedText(plaintext string) string {
	trimmed := strings.TrimSpace(plaintext)
	if trimmed == "" {
		return ""
	}

	msg, err := mail.ReadMessage(strings.NewReader(trimmed))
	if err != nil || msg.Header.Get("Content-Type") == "" {
		return trimmed
	}

	if text, ok := extractTextMIMEPart(msg.Header, msg.Body); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func extractDecryptedSubject(plaintext string) string {
	trimmed := strings.TrimSpace(plaintext)
	if trimmed == "" {
		return ""
	}

	msg, err := mail.ReadMessage(strings.NewReader(trimmed))
	if err != nil {
		return ""
	}
	subject := strings.TrimSpace(msg.Header.Get("Subject"))
	if subject == "" {
		return ""
	}
	decoded, err := new(mime.WordDecoder).DecodeHeader(subject)
	if err != nil {
		return subject
	}
	return strings.TrimSpace(decoded)
}

func extractTextMIMEPart(header mail.Header, body io.Reader) (string, bool) {
	mediaType, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil {
		mediaType = "text/plain"
	}
	mediaType = strings.ToLower(mediaType)

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return "", false
		}
		reader := multipart.NewReader(body, boundary)
		var htmlFallback string
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				if strings.TrimSpace(htmlFallback) != "" {
					return htmlFallback, true
				}
				return "", false
			}
			if err != nil {
				return "", false
			}
			text, ok := extractTextMIMEPart(mail.Header(part.Header), part)
			_ = part.Close()
			if !ok {
				continue
			}
			partMediaType, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
			partMediaType = strings.ToLower(partMediaType)
			if partMediaType == "text/plain" {
				return text, true
			}
			if strings.TrimSpace(htmlFallback) == "" {
				htmlFallback = text
			}
		}
	}

	if mediaType == "text/plain" {
		data, err := readMIMEBody(header, body)
		if err != nil {
			return "", false
		}
		text := string(data)
		if isPGPPlaceholderText(text) {
			return "", false
		}
		return text, true
	}
	if mediaType == "text/html" {
		data, err := readMIMEBody(header, body)
		if err != nil {
			return "", false
		}
		text := stripHTML(string(data))
		if isPGPPlaceholderText(text) {
			return "", false
		}
		return text, true
	}
	return "", false
}

func isPGPPlaceholderText(text string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(text), " "))
	return normalized == "version: 1" ||
		strings.Contains(normalized, "openpgp/mime encrypted message") ||
		strings.Contains(normalized, "this is an encrypted message") ||
		strings.Contains(normalized, "this is a digitally signed message")
}

func stripHTML(text string) string {
	var out strings.Builder
	inTag := false
	for _, r := range text {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
			out.WriteByte(' ')
		default:
			if !inTag {
				out.WriteRune(r)
			}
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

func signatureNeedsPublicKey(status string) bool {
	return strings.Contains(status, "NO_PUBKEY") || strings.Contains(status, "ERRSIG")
}

func locateSigningKeys(ctx context.Context, emails []string) error {
	var lastErr error
	for _, email := range emails {
		gpgCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		cmd := exec.CommandContext(
			gpgCtx,
			"gpg",
			"--batch",
			"--yes",
			"--keyserver",
			"hkps://keys.openpgp.org",
			"--auto-key-locate",
			"local,wkd,keyserver",
			"--locate-keys",
			email,
		)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		if gpgCtx.Err() != nil {
			err = gpgCtx.Err()
		}
		cancel()
		if err == nil {
			return nil
		}
		lastErr = fmt.Errorf("locate key for %s: %w: %s", email, err, strings.TrimSpace(stderr.String()))
	}
	if lastErr == nil {
		return fmt.Errorf("no sender emails available for key lookup")
	}
	return lastErr
}

func senderEmails(addresses []emailAddress) []string {
	seen := make(map[string]struct{}, len(addresses))
	emails := make([]string, 0, len(addresses))
	for _, address := range addresses {
		email := strings.ToLower(strings.TrimSpace(address.Email))
		if email == "" {
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		emails = append(emails, email)
	}
	return emails
}

func pgpRequiredReply(reason string, recipient string) string {
	detail := "Your message could not be processed because Pegasus only accepts OpenPGP messages that are both encrypted and signed."
	recipient = strings.TrimSpace(recipient)
	keyTarget := "the configured recipient address"
	if recipient != "" {
		keyTarget = recipient
	}
	switch reason {
	case "not_encrypted":
		detail = "Your message was not OpenPGP-encrypted."
	case "not_encrypted_to_recipient":
		detail = "Your message was encrypted, but not to the configured recipient key."
	case "not_signed":
		detail = "Your message was encrypted, but it did not include a valid OpenPGP signature."
	case "signature_not_verifiable":
		detail = `Your message was encrypted and signed, but Pegasus could not verify the signing key.

Pegasus tried to retrieve the sender's public key from keys.openpgp.org and retry verification, but it still could not find a public key that validates this signature. This usually means one of these is true:

- The signing public key has not been published.
- The email address on the signing key has not been verified on keys.openpgp.org.
- The message was signed with a different key than the one associated with the From address.

To fix this, publish and verify your signing key for the same email address you send from, then resend the encrypted and signed message.`
	case "pgp_decrypt_failed":
		detail = "Your message looked like OpenPGP mail, but Pegasus could not decrypt and verify it."
	}

	return fmt.Sprintf(`Hello,

%s

Please resend your request as an OpenPGP message that is encrypted to %s and signed with your OpenPGP key. Pegasus accepts standard PGP/MIME messages and armored inline PGP messages.

The Pegasus public key should be published through keys.openpgp.org for %s.

`, detail, keyTarget, keyTarget)
}
