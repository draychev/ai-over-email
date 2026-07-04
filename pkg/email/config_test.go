package email

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCredentialsUsernamePassword(t *testing.T) {
	username := testAddress("user", "mail.test")
	path := writeTempFile(t, "Username="+username+"\nPassword=app-pass\n")

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

func TestLoadCredentialsToken(t *testing.T) {
	path := writeTempFile(t, "Token=test-token\nMailbox=Alerts\n")

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

func TestLoadCredentialsPlaintextAllowlist(t *testing.T) {
	first := testAddress("first", "mail.test")
	second := testAddress("second", "mail.test")
	path := writeTempFile(t, "Token=test-token\nPlaintextAllowlist="+first+", "+second+"\n")

	creds, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials returned error: %v", err)
	}

	want := []string{first, second}
	if strings.Join(creds.PlaintextAllowlist, ",") != strings.Join(want, ",") {
		t.Fatalf("PlaintextAllowlist = %#v, want %#v", creds.PlaintextAllowlist, want)
	}
}

func TestLoadCredentialsBraveSearchAPIToken(t *testing.T) {
	path := writeTempFile(t, "Token=test-token\nBraveSearchAPIToken=brave-token\n")

	creds, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials returned error: %v", err)
	}

	if creds.BraveSearchAPIToken != "brave-token" {
		t.Fatalf("BraveSearchAPIToken = %q", creds.BraveSearchAPIToken)
	}
}

func TestLoadSettingsJMAPEndpoints(t *testing.T) {
	path := writeTempFile(t, "| Setting | Value | Notes |\n| --- | --- | --- |\n| JMAP session endpoint | `https://api.example/session` | current |\n| JMAP legacy Basic auth session endpoint | `https://legacy.example/jmap` | legacy |\n")

	settings, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings returned error: %v", err)
	}

	if settings.JMAPSessionEndpoint != "https://api.example/session" {
		t.Fatalf("JMAPSessionEndpoint = %q", settings.JMAPSessionEndpoint)
	}
	if settings.JMAPLegacySessionEndpoint != "https://legacy.example/jmap" {
		t.Fatalf("JMAPLegacySessionEndpoint = %q", settings.JMAPLegacySessionEndpoint)
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
