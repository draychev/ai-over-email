package send

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
)

type Config struct {
	Server   string
	Port     string
	Username string
	Password string
	From     string
}

func Send(to, subject, body string, cfg Config) error {
	if cfg.Server == "" || cfg.Username == "" || cfg.Password == "" {
		return fmt.Errorf("SMTP_SERVER, USERNAME, and PASSWORD must be set")
	}
	if cfg.Port == "" {
		cfg.Port = "587"
	}
	from := cfg.From
	if from == "" {
		from = cfg.Username
	}

	addr := fmt.Sprintf("%s:%s", cfg.Server, cfg.Port)

	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("smtp dial failed: %w", err)
	}
	defer c.Close()

	if ok, _ := c.Extension("STARTTLS"); ok {
		tlsConfig := &tls.Config{ServerName: cfg.Server}
		if err := c.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("starttls failed: %w", err)
		}
	}

	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Server)
	if err := c.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth failed: %w", err)
	}

	if err := c.Mail(from); err != nil {
		return fmt.Errorf("mail from failed: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt to failed: %w", err)
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("data failed: %w", err)
	}

	msg := buildMessage(from, to, subject, body)
	if _, err := w.Write([]byte(msg)); err != nil {
		_ = w.Close()
		return fmt.Errorf("write body failed: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close body failed: %w", err)
	}

	return c.Quit()
}

func buildMessage(from, to, subject, body string) string {
	safeSubject := strings.ReplaceAll(subject, "\n", " ")
	safeSubject = strings.ReplaceAll(safeSubject, "\r", " ")

	lines := []string{
		"From: " + from,
		"To: " + to,
		"Subject: " + safeSubject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		body,
		"",
	}

	return strings.Join(lines, "\r\n")
}
