package email

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCredentialsUsernamePasswordFromEnvFile(t *testing.T) {
	clearCredentialEnv(t)
	username := testAddress("user", "mail.test")
	path := writeTempFile(t, "AI_OVER_EMAIL_USERNAME="+username+"\nAI_OVER_EMAIL_FASTMAIL_PASSWORD=app-pass\n")

	creds, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials returned error: %v", err)
	}

	if creds.Username != username {
		t.Fatalf("Username = %q", creds.Username)
	}
	if creds.Password != "app-pass" {
		t.Fatalf("Password = %q", creds.Password)
	}
	if creds.Mailbox != "inbox" {
		t.Fatalf("Mailbox = %q", creds.Mailbox)
	}
	if creds.PublicEmail != username {
		t.Fatalf("PublicEmail = %q", creds.PublicEmail)
	}
}

func TestLoadCredentialsTokenFromEnvFile(t *testing.T) {
	clearCredentialEnv(t)
	path := writeTempFile(t, "AI_OVER_EMAIL_FASTMAIL_TOKEN=test-token\nAI_OVER_EMAIL_MAILBOX=Alerts\n")

	creds, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials returned error: %v", err)
	}

	if creds.Token != "test-token" {
		t.Fatalf("Token = %q", creds.Token)
	}
	if creds.Mailbox != "Alerts" {
		t.Fatalf("Mailbox = %q", creds.Mailbox)
	}
}

func TestLoadCredentialsPlaintextAllowlistFromEnvFile(t *testing.T) {
	clearCredentialEnv(t)
	first := testAddress("first", "mail.test")
	second := testAddress("second", "mail.test")
	path := writeTempFile(t, "AI_OVER_EMAIL_FASTMAIL_TOKEN=test-token\nAI_OVER_EMAIL_PLAINTEXT_ALLOWLIST="+first+", "+second+"\n")

	creds, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials returned error: %v", err)
	}

	want := []string{first, second}
	if strings.Join(creds.PlaintextAllowlist, ",") != strings.Join(want, ",") {
		t.Fatalf("PlaintextAllowlist = %#v, want %#v", creds.PlaintextAllowlist, want)
	}
}

func TestLoadCredentialsBraveSearchAPITokenFromEnvFile(t *testing.T) {
	clearCredentialEnv(t)
	path := writeTempFile(t, "AI_OVER_EMAIL_FASTMAIL_TOKEN=test-token\nAI_OVER_EMAIL_BRAVE_API_KEY=brave-token\n")

	creds, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials returned error: %v", err)
	}

	if creds.BraveSearchAPIToken != "brave-token" {
		t.Fatalf("BraveSearchAPIToken = %q", creds.BraveSearchAPIToken)
	}
}

func TestLoadCredentialsEnvironmentOverridesEnvFile(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv("AI_OVER_EMAIL_FASTMAIL_TOKEN", "env-token")
	path := writeTempFile(t, "AI_OVER_EMAIL_FASTMAIL_TOKEN=file-token\n")

	creds, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials returned error: %v", err)
	}

	if creds.Token != "env-token" {
		t.Fatalf("Token = %q", creds.Token)
	}
}

func TestLoadCredentialsWithoutEnvFile(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv("AI_OVER_EMAIL_FASTMAIL_TOKEN", "env-token")

	creds, err := LoadCredentials(filepath.Join(t.TempDir(), ".env"))
	if err != nil {
		t.Fatalf("LoadCredentials returned error: %v", err)
	}

	if creds.Token != "env-token" {
		t.Fatalf("Token = %q", creds.Token)
	}
}

func clearCredentialEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"AI_OVER_EMAIL_USERNAME",
		"AI_OVER_EMAIL_FASTMAIL_PASSWORD",
		"AI_OVER_EMAIL_FASTMAIL_TOKEN",
		"AI_OVER_EMAIL_OPENAI_API_KEY",
		"AI_OVER_EMAIL_BRAVE_API_KEY",
		"AI_OVER_EMAIL_MAILBOX",
		"AI_OVER_EMAIL_PUBLIC_EMAIL",
		"AI_OVER_EMAIL_PLAINTEXT_ALLOWLIST",
	} {
		t.Setenv(key, "")
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func testAddress(local, domain string) string {
	return local + "@" + domain
}
