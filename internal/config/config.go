package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Load(envPath string) (map[string]string, error) {
	config := map[string]string{}

	if envPath != "" {
		path := envPath
		if !filepath.IsAbs(path) {
			if cwd, err := os.Getwd(); err == nil {
				path = filepath.Join(cwd, envPath)
			}
		}
		if data, err := os.ReadFile(path); err == nil {
			parsed := parseEnv(string(data))
			for k, v := range parsed {
				config[k] = v
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("failed to read env file: %w", err)
		}
	}

	for _, key := range []string{
		"SERVER",
		"USERNAME",
		"PASSWORD",
		"SSL",
		"SMTP_SERVER",
		"SMTP_PORT",
		"FROM_EMAIL",
	} {
		if val := os.Getenv(key); val != "" {
			config[key] = val
		}
	}

	return config, nil
}

func Value(config map[string]string, key, fallback string) string {
	if val := config[key]; val != "" {
		return val
	}
	return fallback
}

func parseEnv(content string) map[string]string {
	result := map[string]string{}
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "="); idx != -1 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			val = strings.TrimSuffix(val, ",")
			val = strings.TrimSpace(val)
			val = strings.Trim(val, "\"'")
			if key != "" {
				result[key] = val
			}
		}
	}
	return result
}
