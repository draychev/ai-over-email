package usenet

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

type Credentials struct {
	Username            string
	Password            string
	OpenAIAPIToken      string
	BraveSearchAPIToken string
}

func LoadCredentials(envPath string) (Credentials, error) {
	values, err := loadEnvironment(envPath)
	if err != nil {
		return Credentials{}, err
	}
	creds := Credentials{
		Username:            first(values, "AI_OVER_USENET_USERNAME", "AI_OVER_EMAIL_USENET_USERNAME"),
		Password:            first(values, "AI_OVER_USENET_PASSWORD", "AI_OVER_EMAIL_USENET_PASSWORD"),
		OpenAIAPIToken:      first(values, "AI_OVER_EMAIL_OPENAI_API_KEY"),
		BraveSearchAPIToken: first(values, "AI_OVER_EMAIL_BRAVE_API_KEY"),
	}
	if creds.Username == "" {
		return Credentials{}, errors.New("credentials must include AI_OVER_USENET_USERNAME")
	}
	if creds.Password == "" {
		return Credentials{}, errors.New("credentials must include AI_OVER_USENET_PASSWORD")
	}
	if creds.OpenAIAPIToken == "" {
		return Credentials{}, errors.New("credentials must include AI_OVER_EMAIL_OPENAI_API_KEY")
	}
	return creds, nil
}

func loadEnvironment(envPath string) (map[string]string, error) {
	if envPath == "" {
		envPath = ".env"
	}
	values := make(map[string]string)
	if envPath != "-" {
		fileValues, err := loadKeyValueFile(envPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		for key, value := range fileValues {
			values[key] = value
		}
	}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || value == "" {
			continue
		}
		values[strings.ToUpper(strings.TrimSpace(key))] = value
	}
	return values, nil
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
