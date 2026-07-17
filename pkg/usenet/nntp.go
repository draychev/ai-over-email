package usenet

import (
	"bufio"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

var errArticleMissing = errors.New("article missing")

type nntpClient struct {
	conn net.Conn
	text *textproto.Conn
}

type article struct {
	Number     int
	MessageID  string
	Subject    string
	From       string
	References []string
	Body       string
	RawHeader  mail.Header
}

type groupStatus struct {
	Count int
	Low   int
	High  int
	Name  string
}

func dialNNTP(host string, port int, security string, serverName string, certSHA256 string, timeout time.Duration) (*nntpClient, error) {
	if security == "none" {
		return dialPlainNNTP(host, port, timeout)
	}
	return dialTLSNNTP(host, port, serverName, certSHA256, timeout)
}

func dialPlainNNTP(host string, port int, timeout time.Duration) (*nntpClient, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connect NNTP: %w", err)
	}
	return newNNTPClient(conn)
}

func dialTLSNNTP(host string, port int, serverName string, certSHA256 string, timeout time.Duration) (*nntpClient, error) {
	if serverName == "" {
		serverName = host
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: timeout}
	tlsConfig := &tls.Config{
		ServerName: serverName,
		MinVersion: tls.VersionTLS12,
	}
	if certSHA256 != "" {
		want := normalizeHex(certSHA256)
		tlsConfig.InsecureSkipVerify = true
		tlsConfig.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("server did not provide a certificate")
			}
			sum := sha256.Sum256(rawCerts[0])
			got := hex.EncodeToString(sum[:])
			if got != want {
				return fmt.Errorf("server certificate fingerprint mismatch")
			}
			return nil
		}
	}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("connect NNTP TLS: %w", err)
	}
	return newNNTPClient(conn)
}

func newNNTPClient(conn net.Conn) (*nntpClient, error) {
	client := &nntpClient{conn: conn, text: textproto.NewConn(conn)}
	code, line, err := client.readCodeLine()
	if err != nil {
		conn.Close()
		return nil, err
	}
	if code != 200 && code != 201 {
		conn.Close()
		return nil, fmt.Errorf("NNTP greeting: %d %s", code, line)
	}
	return client, nil
}

func (c *nntpClient) Close() error {
	_, _, _ = c.command("QUIT")
	if c.text != nil {
		return c.text.Close()
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *nntpClient) Auth(username, password string) error {
	code, line, err := c.command("AUTHINFO USER %s", username)
	if err != nil {
		return err
	}
	switch code {
	case 281:
		return nil
	case 381:
	default:
		return fmt.Errorf("AUTHINFO USER: %d %s", code, line)
	}
	code, line, err = c.command("AUTHINFO PASS %s", password)
	if err != nil {
		return err
	}
	if code != 281 {
		return fmt.Errorf("AUTHINFO PASS: %d %s", code, line)
	}
	return nil
}

func (c *nntpClient) Group(name string) (groupStatus, error) {
	code, line, err := c.command("GROUP %s", name)
	if err != nil {
		return groupStatus{}, err
	}
	if code != 211 {
		return groupStatus{}, fmt.Errorf("GROUP %s: %d %s", name, code, line)
	}
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return groupStatus{}, fmt.Errorf("GROUP %s returned malformed response: %q", name, line)
	}
	count, _ := strconv.Atoi(fields[0])
	low, _ := strconv.Atoi(fields[1])
	high, _ := strconv.Atoi(fields[2])
	return groupStatus{Count: count, Low: low, High: high, Name: fields[3]}, nil
}

func (c *nntpClient) ArticleByNumber(number int) (article, error) {
	code, line, err := c.command("ARTICLE %d", number)
	if err != nil {
		return article{}, err
	}
	if code == 423 || code == 430 {
		return article{}, errArticleMissing
	}
	if code != 220 {
		return article{}, fmt.Errorf("ARTICLE %d: %d %s", number, code, line)
	}
	lines, err := c.text.ReadDotLines()
	if err != nil {
		return article{}, fmt.Errorf("read ARTICLE %d: %w", number, err)
	}
	return parseArticle(number, strings.Join(lines, "\r\n"))
}

func (c *nntpClient) ArticleByMessageID(messageID string) (article, error) {
	code, line, err := c.command("ARTICLE %s", messageID)
	if err != nil {
		return article{}, err
	}
	if code == 423 || code == 430 {
		return article{}, errArticleMissing
	}
	if code != 220 {
		return article{}, fmt.Errorf("ARTICLE %s: %d %s", messageID, code, line)
	}
	fields := strings.Fields(line)
	number := 0
	if len(fields) > 0 {
		number, _ = strconv.Atoi(fields[0])
	}
	lines, err := c.text.ReadDotLines()
	if err != nil {
		return article{}, fmt.Errorf("read ARTICLE %s: %w", messageID, err)
	}
	return parseArticle(number, strings.Join(lines, "\r\n"))
}

func (c *nntpClient) HasMessageID(messageID string) (bool, error) {
	code, line, err := c.command("STAT %s", messageID)
	if err != nil {
		return false, err
	}
	switch code {
	case 223:
		return true, nil
	case 430:
		return false, nil
	default:
		return false, fmt.Errorf("STAT %s: %d %s", messageID, code, line)
	}
}

func (c *nntpClient) Post(raw string) error {
	code, line, err := c.command("POST")
	if err != nil {
		return err
	}
	if code != 340 {
		return fmt.Errorf("POST: %d %s", code, line)
	}
	writer := c.text.DotWriter()
	if _, err := io.WriteString(writer, raw); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write POST article: %w", err)
	}
	if !strings.HasSuffix(raw, "\n") {
		if _, err := io.WriteString(writer, "\r\n"); err != nil {
			_ = writer.Close()
			return fmt.Errorf("write POST article newline: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish POST article: %w", err)
	}
	code, line, err = c.readCodeLine()
	if err != nil {
		return err
	}
	if code != 240 {
		return fmt.Errorf("POST finish: %d %s", code, line)
	}
	return nil
}

func (c *nntpClient) command(format string, args ...any) (int, string, error) {
	if err := c.text.PrintfLine(format, args...); err != nil {
		return 0, "", fmt.Errorf("write NNTP command: %w", err)
	}
	return c.readCodeLine()
}

func (c *nntpClient) readCodeLine() (int, string, error) {
	line, err := c.text.ReadLine()
	if err != nil {
		return 0, "", fmt.Errorf("read NNTP response: %w", err)
	}
	if len(line) < 3 {
		return 0, line, fmt.Errorf("short NNTP response: %q", line)
	}
	code, err := strconv.Atoi(line[:3])
	if err != nil {
		return 0, line, fmt.Errorf("bad NNTP response code %q: %w", line, err)
	}
	message := strings.TrimSpace(line[3:])
	return code, message, nil
}

func parseArticle(number int, raw string) (article, error) {
	msg, err := mail.ReadMessage(bufio.NewReader(strings.NewReader(raw)))
	if err != nil {
		return article{}, fmt.Errorf("parse article: %w", err)
	}
	body, err := io.ReadAll(msg.Body)
	if err != nil {
		return article{}, fmt.Errorf("read article body: %w", err)
	}
	return article{
		Number:     number,
		MessageID:  strings.TrimSpace(msg.Header.Get("Message-ID")),
		Subject:    strings.TrimSpace(msg.Header.Get("Subject")),
		From:       strings.TrimSpace(msg.Header.Get("From")),
		References: parseReferences(msg.Header.Get("References")),
		Body:       strings.TrimSpace(string(body)),
		RawHeader:  msg.Header,
	}, nil
}

func parseReferences(value string) []string {
	fields := strings.Fields(value)
	result := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		result = append(result, field)
	}
	return result
}

func normalizeHex(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "sha256 fingerprint=")
	value = strings.TrimPrefix(value, "sha256=")
	value = strings.ReplaceAll(value, ":", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}
