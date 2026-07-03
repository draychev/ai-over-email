package email

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
)

const maxAttachmentBytes = 25 * 1024 * 1024

func (w *Watcher) fetchAttachments(ctx context.Context, parts []emailBodyPart) ([]emailAttachment, error) {
	attachments := make([]emailAttachment, 0, len(parts))
	for _, part := range parts {
		blobID := strings.TrimSpace(part.BlobID)
		if blobID == "" {
			continue
		}
		attachment := emailAttachment{
			Name:   part.Name,
			Type:   part.Type,
			BlobID: blobID,
			Size:   part.Size,
		}
		data, err := w.client.Download(ctx, w.accountID, blobID, attachmentName(attachment), attachmentType(attachment))
		if err != nil {
			return nil, err
		}
		attachment.Data = data
		if attachment.Size == 0 {
			attachment.Size = len(data)
		}
		attachments = append(attachments, attachment)
	}
	return attachments, nil
}

func extractDecryptedAttachments(plaintext string) []emailAttachment {
	trimmed := strings.TrimSpace(plaintext)
	if trimmed == "" {
		return nil
	}
	msg, err := mail.ReadMessage(strings.NewReader(trimmed))
	if err != nil || msg.Header.Get("Content-Type") == "" {
		return nil
	}

	var attachments []emailAttachment
	extractMIMEAttachments(msg.Header, msg.Body, &attachments)
	return attachments
}

func extractMIMEAttachments(header mail.Header, body io.Reader, attachments *[]emailAttachment) {
	mediaType, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil {
		mediaType = "text/plain"
	}
	mediaType = strings.ToLower(mediaType)

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return
		}
		reader := multipart.NewReader(body, boundary)
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				return
			}
			if err != nil {
				return
			}
			extractMIMEAttachments(mail.Header(part.Header), part, attachments)
			_ = part.Close()
		}
	}

	if !isAttachmentPart(header, mediaType) || isPGPControlPart(mediaType) {
		return
	}
	data, err := readMIMEBody(header, io.LimitReader(body, maxAttachmentBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxAttachmentBytes {
		return
	}
	attachment := emailAttachment{
		Name: attachmentName(emailAttachment{
			Name: attachmentNameFromHeader(header, mediaType, len(*attachments)+1),
			Type: mediaType,
		}),
		Type: mediaType,
		Data: data,
		Size: len(data),
	}
	*attachments = append(*attachments, attachment)
}

func isAttachmentPart(header mail.Header, mediaType string) bool {
	_, dispositionParams, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
	disposition, _, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
	disposition = strings.ToLower(disposition)
	if disposition == "attachment" {
		return true
	}
	if disposition == "inline" && (dispositionParams["filename"] != "" || strings.HasPrefix(mediaType, "image/")) {
		return true
	}
	if _, params, err := mime.ParseMediaType(header.Get("Content-Type")); err == nil && params["name"] != "" {
		return true
	}
	return strings.HasPrefix(mediaType, "image/")
}

func isPGPControlPart(mediaType string) bool {
	switch mediaType {
	case "application/pgp-signature", "application/pgp-encrypted", "application/pgp-keys":
		return true
	default:
		return false
	}
}

func attachmentNameFromHeader(header mail.Header, mediaType string, index int) string {
	if _, params, err := mime.ParseMediaType(header.Get("Content-Disposition")); err == nil {
		if name := strings.TrimSpace(params["filename"]); name != "" {
			return name
		}
	}
	if _, params, err := mime.ParseMediaType(header.Get("Content-Type")); err == nil {
		if name := strings.TrimSpace(params["name"]); name != "" {
			return name
		}
	}
	exts, _ := mime.ExtensionsByType(mediaType)
	ext := ".bin"
	if len(exts) > 0 {
		ext = exts[0]
	}
	return fmt.Sprintf("attachment-%d%s", index, ext)
}

func attachmentName(attachment emailAttachment) string {
	name := strings.TrimSpace(attachment.Name)
	if name != "" {
		return name
	}
	exts, _ := mime.ExtensionsByType(attachmentType(attachment))
	ext := ".bin"
	if len(exts) > 0 {
		ext = exts[0]
	}
	return "attachment" + ext
}

func attachmentType(attachment emailAttachment) string {
	contentType := strings.ToLower(strings.TrimSpace(attachment.Type))
	if contentType == "" {
		return "application/octet-stream"
	}
	if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
		return strings.ToLower(mediaType)
	}
	return contentType
}

func attachmentDataURL(attachment emailAttachment) string {
	return "data:" + attachmentType(attachment) + ";base64," + base64.StdEncoding.EncodeToString(attachment.Data)
}
