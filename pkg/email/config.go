package email

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	defaultSessionEndpoint = "https://api.fastmail.com/jmap/session"
	legacySessionEndpoint  = "https://jmap.fastmail.com/.well-known/jmap"
)

type Credentials struct {
	Username            string
	Password            string
	Token               string
	OpenAIAPIToken      string
	BraveSearchAPIToken string
	Mailbox             string
	PublicEmail         string
	PlaintextAllowlist  []string
}

type Settings struct {
	JMAPSessionEndpoint       string
	JMAPLegacySessionEndpoint string
}

func LoadCredentials(path string) (Credentials, error) {
	values, err := loadKeyValueFile(path)
	if err != nil {
		return Credentials{}, err
	}

	creds := Credentials{
		Username:            first(values, "username", "user", "email"),
		Password:            first(values, "password", "app_password", "app-password"),
		Token:               first(values, "token", "api_token", "api-token", "bearer"),
		OpenAIAPIToken:      first(values, "openai_api_token", "openai-token", "openai_token"),
		BraveSearchAPIToken: first(values, "bravesearchapitoken", "brave_search_api_token", "brave-search-api-token", "brave_token", "brave-token"),
		Mailbox:             first(values, "mailbox", "folder"),
		PublicEmail:         first(values, "publicemail", "public_email", "public-email", "pgpemail", "pgp_email", "pgp-email"),
		PlaintextAllowlist:  splitList(first(values, "plaintextallowlist", "plaintext_allowlist", "plaintext-allowlist", "allowlist")),
	}
	if creds.Token == "" && looksLikeFastmailAPIToken(creds.Password) {
		creds.Token = creds.Password
		creds.Password = ""
	}
	if creds.Mailbox == "" {
		creds.Mailbox = "inbox"
	}
	if creds.PublicEmail == "" {
		creds.PublicEmail = creds.Username
	}
	if creds.Token == "" && creds.Password == "" {
		return Credentials{}, errors.New("creds.txt must contain Password=... or Token=...")
	}
	if creds.Token == "" && creds.Username == "" {
		return Credentials{}, errors.New("creds.txt must contain Username=... when using Password=...")
	}

	return creds, nil
}

func splitList(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t'
	})
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		result = append(result, part)
	}
	return result
}

func looksLikeFastmailAPIToken(value string) bool {
	return strings.HasPrefix(value, "fmu"+"1-") || strings.HasPrefix(value, "fmu"+"2-")
}

func LoadSettings(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, fmt.Errorf("read settings: %w", err)
	}

	settings := Settings{
		JMAPSessionEndpoint:       findMarkdownSetting(string(data), "JMAP session endpoint"),
		JMAPLegacySessionEndpoint: findMarkdownSetting(string(data), "JMAP legacy Basic auth session endpoint"),
	}
	if settings.JMAPSessionEndpoint == "" {
		settings.JMAPSessionEndpoint = defaultSessionEndpoint
	}
	if settings.JMAPLegacySessionEndpoint == "" {
		settings.JMAPLegacySessionEndpoint = legacySessionEndpoint
	}

	return settings, nil
}

func loadKeyValueFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read credentials: %w", err)
	}
	defer file.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid credentials line %q: expected key=value", line)
		}
		values[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan credentials: %w", err)
	}

	return values, nil
}

func first(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := values[strings.ToLower(key)]; value != "" {
			return value
		}
	}
	return ""
}

func findMarkdownSetting(markdown, label string) string {
	pattern := regexp.MustCompile(`(?im)^\|\s*` + regexp.QuoteMeta(label) + `\s*\|\s*` + "`([^`]+)`" + `\s*\|`)
	match := pattern.FindStringSubmatch(markdown)
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}
