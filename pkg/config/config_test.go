package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	path := writeTempFile(t, `{
  "jmap": {
    "session_endpoint": "https://api.example/session",
    "legacy_basic_auth_session_endpoint": "https://legacy.example/jmap"
  }
}`)

	config, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if config.JMAP.SessionEndpoint != "https://api.example/session" {
		t.Fatalf("SessionEndpoint = %q", config.JMAP.SessionEndpoint)
	}
	if config.JMAP.LegacyBasicAuthSessionEndpoint != "https://legacy.example/jmap" {
		t.Fatalf("LegacyBasicAuthSessionEndpoint = %q", config.JMAP.LegacyBasicAuthSessionEndpoint)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := writeTempFile(t, `{
  "jmap": {
    "session_endpoint": "https://api.example/session",
    "legacy_basic_auth_session_endpoint": "https://legacy.example/jmap"
  },
  "extra": true
}`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Load error = %v, want unknown field error", err)
	}
}

func TestLoadRequiresHTTPSJMAPEndpoint(t *testing.T) {
	path := writeTempFile(t, `{
  "jmap": {
    "session_endpoint": "http://api.example/session",
    "legacy_basic_auth_session_endpoint": "https://legacy.example/jmap"
  }
}`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "jmap.session_endpoint must use https") {
		t.Fatalf("Load error = %v, want https validation error", err)
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}
