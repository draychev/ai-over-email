package email

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type correspondentStore struct {
	db *sql.DB
}

type correspondentRegistration struct {
	New                  bool
	ZipPresent           bool
	TimezonePresent      bool
	ProfileRequestNeeded bool
}

type correspondentProfileUpdate struct {
	ZipCode  string
	TimeZone string
}

var (
	zipCodePattern  = regexp.MustCompile(`\b\d{5}(?:-\d{4})?\b`)
	utcZonePattern  = regexp.MustCompile(`(?i)\b(?:UTC|GMT)\s*([+-])\s*(\d{1,2})(?::?(\d{2}))?\b`)
	ianaZonePattern = regexp.MustCompile(`\b(?:Africa|America|Antarctica|Arctic|Asia|Atlantic|Australia|Europe|Indian|Pacific)/[A-Za-z0-9_+\-]+(?:/[A-Za-z0-9_+\-]+)?\b`)
)

func openCorrespondentStore(path string) (*correspondentStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = filepath.Join(".tmp", "correspondents.sqlite3")
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create correspondent db dir: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open correspondent db: %w", err)
	}
	store := &correspondentStore{db: db}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *correspondentStore) migrate(ctx context.Context) error {
	statements := []string{
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA journal_mode = WAL`,
		`CREATE TABLE IF NOT EXISTS correspondents (
			email TEXT PRIMARY KEY,
			display_name TEXT NOT NULL DEFAULT '',
			zip_code TEXT NOT NULL DEFAULT '',
			time_zone TEXT NOT NULL DEFAULT '',
			time_zone_source TEXT NOT NULL DEFAULT '',
			profile_request_sent_at TEXT NOT NULL DEFAULT '',
			first_seen_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate correspondent db: %w", err)
		}
	}
	return nil
}

func (s *correspondentStore) Register(ctx context.Context, email string, displayName string, derivedTimeZone string) (correspondentRegistration, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	displayName = strings.TrimSpace(displayName)
	derivedTimeZone = strings.TrimSpace(derivedTimeZone)
	if email == "" {
		return correspondentRegistration{}, fmt.Errorf("correspondent email is empty")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return correspondentRegistration{}, err
	}
	defer tx.Rollback()

	var existing struct {
		DisplayName          string
		ZipCode              string
		TimeZone             string
		ProfileRequestSentAt string
	}
	err = tx.QueryRowContext(ctx, `SELECT display_name, zip_code, time_zone, profile_request_sent_at FROM correspondents WHERE email = ?`, email).
		Scan(&existing.DisplayName, &existing.ZipCode, &existing.TimeZone, &existing.ProfileRequestSentAt)
	if err == sql.ErrNoRows {
		timeZoneSource := ""
		if derivedTimeZone != "" {
			timeZoneSource = "email_header"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO correspondents (
			email, display_name, zip_code, time_zone, time_zone_source, profile_request_sent_at, first_seen_at, last_seen_at, updated_at
		) VALUES (?, ?, '', ?, ?, '', ?, ?, ?)`, email, displayName, derivedTimeZone, timeZoneSource, now, now, now); err != nil {
			return correspondentRegistration{}, err
		}
		if err := tx.Commit(); err != nil {
			return correspondentRegistration{}, err
		}
		return correspondentRegistration{
			New:                  true,
			ZipPresent:           false,
			TimezonePresent:      derivedTimeZone != "",
			ProfileRequestNeeded: true,
		}, nil
	}
	if err != nil {
		return correspondentRegistration{}, err
	}

	nextDisplayName := existing.DisplayName
	if nextDisplayName == "" && displayName != "" {
		nextDisplayName = displayName
	}
	nextTimeZone := existing.TimeZone
	nextTimeZoneSource := ""
	if nextTimeZone == "" && derivedTimeZone != "" {
		nextTimeZone = derivedTimeZone
		nextTimeZoneSource = "email_header"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE correspondents
		SET display_name = ?,
			time_zone = CASE WHEN time_zone = '' THEN ? ELSE time_zone END,
			time_zone_source = CASE WHEN time_zone_source = '' AND ? != '' THEN ? ELSE time_zone_source END,
			last_seen_at = ?,
			updated_at = ?
		WHERE email = ?`, nextDisplayName, nextTimeZone, nextTimeZoneSource, nextTimeZoneSource, now, now, email); err != nil {
		return correspondentRegistration{}, err
	}
	if err := tx.Commit(); err != nil {
		return correspondentRegistration{}, err
	}

	zipPresent := strings.TrimSpace(existing.ZipCode) != ""
	timezonePresent := strings.TrimSpace(nextTimeZone) != ""
	return correspondentRegistration{
		New:                  false,
		ZipPresent:           zipPresent,
		TimezonePresent:      timezonePresent,
		ProfileRequestNeeded: (!zipPresent || !timezonePresent) && strings.TrimSpace(existing.ProfileRequestSentAt) == "",
	}, nil
}

func (s *correspondentStore) MarkProfileRequestSent(ctx context.Context, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return fmt.Errorf("correspondent email is empty")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE correspondents
		SET profile_request_sent_at = CASE WHEN profile_request_sent_at = '' THEN ? ELSE profile_request_sent_at END,
			updated_at = ?
		WHERE email = ?`, now, now, email)
	return err
}

func (s *correspondentStore) UpdateProfile(ctx context.Context, email string, update correspondentProfileUpdate) error {
	email = strings.ToLower(strings.TrimSpace(email))
	update.ZipCode = strings.TrimSpace(update.ZipCode)
	update.TimeZone = strings.TrimSpace(update.TimeZone)
	if email == "" {
		return fmt.Errorf("correspondent email is empty")
	}
	if update.ZipCode == "" && update.TimeZone == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE correspondents
		SET zip_code = CASE WHEN ? != '' THEN ? ELSE zip_code END,
			time_zone = CASE WHEN ? != '' THEN ? ELSE time_zone END,
			time_zone_source = CASE WHEN ? != '' THEN 'email_body' ELSE time_zone_source END,
			updated_at = ?
		WHERE email = ?`, update.ZipCode, update.ZipCode, update.TimeZone, update.TimeZone, update.TimeZone, now, email)
	return err
}

func extractCorrespondentProfileUpdate(text string) correspondentProfileUpdate {
	text = strings.TrimSpace(text)
	if text == "" {
		return correspondentProfileUpdate{}
	}
	update := correspondentProfileUpdate{
		ZipCode: zipCodePattern.FindString(text),
	}
	if zone := ianaZonePattern.FindString(text); zone != "" {
		update.TimeZone = zone
		return update
	}
	if match := utcZonePattern.FindStringSubmatch(text); len(match) > 0 {
		hours := match[2]
		if len(hours) == 1 {
			hours = "0" + hours
		}
		minutes := match[3]
		if minutes == "" {
			minutes = "00"
		}
		update.TimeZone = "UTC" + match[1] + hours + ":" + minutes
	}
	return update
}

func deriveTimezoneFromEmailHeaders(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	if zone := timezoneFromDateHeader(msg.Header.Get("Date")); zone != "" {
		return zone
	}
	for _, received := range msg.Header["Received"] {
		if _, dateText, ok := strings.Cut(received, ";"); ok {
			if zone := timezoneFromDateHeader(strings.TrimSpace(dateText)); zone != "" {
				return zone
			}
		}
	}
	return ""
}

func timezoneFromDateHeader(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := mail.ParseDate(value)
	if err != nil {
		return ""
	}
	_, offset := parsed.Zone()
	return formatUTCOffset(offset)
}

func formatUTCOffset(offsetSeconds int) string {
	sign := "+"
	if offsetSeconds < 0 {
		sign = "-"
		offsetSeconds = -offsetSeconds
	}
	hours := offsetSeconds / 3600
	minutes := (offsetSeconds % 3600) / 60
	return fmt.Sprintf("UTC%s%02d:%02d", sign, hours, minutes)
}
