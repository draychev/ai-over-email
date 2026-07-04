package email

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestCorrespondentStoreRegistersNewSenderAndMarksProfileRequest(t *testing.T) {
	ctx := context.Background()
	store := openTestCorrespondentStore(t)

	email := testAddress("sender", "mail.test")
	registered, err := store.Register(ctx, email, "Sender", "UTC-05:00")
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if !registered.New {
		t.Fatalf("New = false, want true")
	}
	if !registered.TimezonePresent {
		t.Fatalf("TimezonePresent = false, want true")
	}
	if !registered.ProfileRequestNeeded {
		t.Fatalf("ProfileRequestNeeded = false, want true because zip is missing")
	}

	if err := store.MarkProfileRequestSent(ctx, email); err != nil {
		t.Fatalf("MarkProfileRequestSent returned error: %v", err)
	}
	registered, err = store.Register(ctx, email, "Sender", "UTC-05:00")
	if err != nil {
		t.Fatalf("second Register returned error: %v", err)
	}
	if registered.New {
		t.Fatalf("second New = true, want false")
	}
	if registered.ProfileRequestNeeded {
		t.Fatalf("ProfileRequestNeeded repeated after mark")
	}
}

func TestCorrespondentStoreBackfillsMissingTimezone(t *testing.T) {
	ctx := context.Background()
	store := openTestCorrespondentStore(t)

	email := testAddress("sender", "mail.test")
	if _, err := store.Register(ctx, email, "", ""); err != nil {
		t.Fatalf("Register without timezone returned error: %v", err)
	}
	if _, err := store.Register(ctx, email, "", "UTC+02:00"); err != nil {
		t.Fatalf("Register with timezone returned error: %v", err)
	}

	var timeZone string
	if err := store.db.QueryRowContext(ctx, `SELECT time_zone FROM correspondents WHERE email = ?`, email).Scan(&timeZone); err != nil {
		t.Fatalf("query time_zone: %v", err)
	}
	if timeZone != "UTC+02:00" {
		t.Fatalf("time_zone = %q, want UTC+02:00", timeZone)
	}
}

func TestCorrespondentStoreUpdatesProfileFromEmailBody(t *testing.T) {
	ctx := context.Background()
	store := openTestCorrespondentStore(t)

	email := testAddress("sender", "mail.test")
	if _, err := store.Register(ctx, email, "", ""); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	update := extractCorrespondentProfileUpdate("My ZIP is 10001 and my time zone is America/New_York.")
	if err := store.UpdateProfile(ctx, email, update); err != nil {
		t.Fatalf("UpdateProfile returned error: %v", err)
	}

	var zipCode, timeZone string
	if err := store.db.QueryRowContext(ctx, `SELECT zip_code, time_zone FROM correspondents WHERE email = ?`, email).Scan(&zipCode, &timeZone); err != nil {
		t.Fatalf("query profile: %v", err)
	}
	if zipCode != "10001" {
		t.Fatalf("zip_code = %q, want 10001", zipCode)
	}
	if timeZone != "America/New_York" {
		t.Fatalf("time_zone = %q, want America/New_York", timeZone)
	}
}

func TestExtractCorrespondentProfileUpdateUTCOffset(t *testing.T) {
	update := extractCorrespondentProfileUpdate("zip 94105, timezone UTC-7")

	if update.ZipCode != "94105" {
		t.Fatalf("ZipCode = %q, want 94105", update.ZipCode)
	}
	if update.TimeZone != "UTC-07:00" {
		t.Fatalf("TimeZone = %q, want UTC-07:00", update.TimeZone)
	}
}

func TestDeriveTimezoneFromEmailHeadersDate(t *testing.T) {
	raw := []byte("From: sender@mail.test\r\nDate: Mon, 29 Jun 2026 14:06:46 -0700\r\n\r\nHello")

	if got := deriveTimezoneFromEmailHeaders(raw); got != "UTC-07:00" {
		t.Fatalf("deriveTimezoneFromEmailHeaders = %q, want UTC-07:00", got)
	}
}

func TestDeriveTimezoneFromEmailHeadersReceivedFallback(t *testing.T) {
	raw := []byte("From: sender@mail.test\r\nReceived: by mx.mail.test; Mon, 29 Jun 2026 23:06:46 +0200\r\n\r\nHello")

	if got := deriveTimezoneFromEmailHeaders(raw); got != "UTC+02:00" {
		t.Fatalf("deriveTimezoneFromEmailHeaders = %q, want UTC+02:00", got)
	}
}

func openTestCorrespondentStore(t *testing.T) *correspondentStore {
	t.Helper()
	store, err := openCorrespondentStore(filepath.Join(t.TempDir(), "correspondents.sqlite3"))
	if err != nil {
		t.Fatalf("openCorrespondentStore returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.db.Close(); err != nil && err != sql.ErrConnDone {
			t.Fatalf("close db: %v", err)
		}
	})
	return store
}
