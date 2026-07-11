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
  },
  "openai": {
    "default_model": "gpt-default",
    "default_reasoning_effort": "low",
    "powerful_model": "gpt-powerful",
    "powerful_reasoning_effort": "medium",
    "powerful_senders": ["Power User <power@example.com>"]
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
	settings := config.OpenAISettingsForSenders([]string{"power@example.com"})
	if settings.Model != "gpt-powerful" || settings.ReasoningEffort != "medium" {
		t.Fatalf("powerful settings = %#v", settings)
	}
}

func TestOpenAISettingsForSendersUsesDefaults(t *testing.T) {
	cfg := ConfigStruct{}

	settings := cfg.OpenAISettingsForSenders([]string{"sender@example.com"})

	if settings.Model != DefaultOpenAIModel || settings.ReasoningEffort != DefaultOpenAIReasoningEffort {
		t.Fatalf("settings = %#v, want defaults", settings)
	}
}

func TestOpenAISettingsForSendersMatchesCaseInsensitivePowerfulSender(t *testing.T) {
	cfg := ConfigStruct{
		OpenAI: OpenAIConfig{
			DefaultModel:            "gpt-default",
			DefaultReasoningEffort:  "low",
			PowerfulModel:           "gpt-powerful",
			PowerfulReasoningEffort: "medium",
			PowerfulSenders:         []string{"Power User <power@example.com>"},
		},
	}

	settings := cfg.OpenAISettingsForSenders([]string{" POWER@EXAMPLE.COM "})

	if settings.Model != "gpt-powerful" || settings.ReasoningEffort != "medium" {
		t.Fatalf("settings = %#v, want powerful settings", settings)
	}
}

func TestLoadRejectsInvalidPowerfulSender(t *testing.T) {
	path := writeTempFile(t, `{
  "jmap": {
    "session_endpoint": "https://api.example/session",
    "legacy_basic_auth_session_endpoint": "https://legacy.example/jmap"
  },
  "openai": {
    "powerful_senders": ["not an email"]
  }
}`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "openai.powerful_senders") {
		t.Fatalf("Load error = %v, want powerful_senders validation error", err)
	}
}

func TestLoadRejectsInvalidReasoningEffort(t *testing.T) {
	path := writeTempFile(t, `{
  "jmap": {
    "session_endpoint": "https://api.example/session",
    "legacy_basic_auth_session_endpoint": "https://legacy.example/jmap"
  },
  "openai": {
    "powerful_reasoning_effort": "extreme"
  }
}`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "openai.powerful_reasoning_effort") {
		t.Fatalf("Load error = %v, want reasoning validation error", err)
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
