package email

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

const defaultEnvPath = ".env"

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

func LoadCredentials(envPath string) (Credentials, error) {
	values, err := loadEnvironment(envPath)
	if err != nil {
		return Credentials{}, err
	}

	creds := Credentials{
		Username:            first(values, "AI_OVER_EMAIL_USERNAME"),
		Password:            first(values, "AI_OVER_EMAIL_FASTMAIL_PASSWORD"),
		Token:               first(values, "AI_OVER_EMAIL_FASTMAIL_TOKEN"),
		OpenAIAPIToken:      first(values, "AI_OVER_EMAIL_OPENAI_API_KEY"),
		BraveSearchAPIToken: first(values, "AI_OVER_EMAIL_BRAVE_API_KEY"),
		Mailbox:             first(values, "AI_OVER_EMAIL_MAILBOX"),
		PublicEmail:         first(values, "AI_OVER_EMAIL_PUBLIC_EMAIL"),
		PlaintextAllowlist:  splitList(first(values, "AI_OVER_EMAIL_PLAINTEXT_ALLOWLIST")),
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
		return Credentials{}, errors.New("credentials must include AI_OVER_EMAIL_FASTMAIL_TOKEN or AI_OVER_EMAIL_FASTMAIL_PASSWORD")
	}
	if creds.Token == "" && creds.Username == "" {
		return Credentials{}, errors.New("credentials must include AI_OVER_EMAIL_USERNAME when using AI_OVER_EMAIL_FASTMAIL_PASSWORD")
	}

	return creds, nil
}

func loadEnvironment(envPath string) (map[string]string, error) {
	if envPath == "" {
		envPath = defaultEnvPath
	}

	values := make(map[string]string)
	if envPath != "-" {
		if fileValues, err := loadKeyValueFile(envPath); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		} else {
			for key, value := range fileValues {
				values[key] = value
			}
		}
	}

	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if value == "" {
			continue
		}
		values[strings.ToUpper(strings.TrimSpace(key))] = value
	}

	return values, nil
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

func loadKeyValueFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read environment file %s: %w", path, err)
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
			return nil, fmt.Errorf("invalid environment line %q: expected key=value", line)
		}
		values[strings.ToUpper(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan environment file: %w", err)
	}

	return values, nil
}

func first(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := values[strings.ToUpper(key)]; value != "" {
			return value
		}
	}
	return ""
}
