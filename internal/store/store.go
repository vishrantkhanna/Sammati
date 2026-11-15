// Package store keeps everything Sammati knows in one SQLite file:
// purposes, notices, forms, API keys and the append-only consent record.
package store

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver" // SQLite driver, pure Go
)

// ErrNotFound is returned when a record does not exist.
var ErrNotFound = errors.New("not found")

// InvalidError is a problem with what the caller sent. Its message is safe
// to show.
type InvalidError struct{ Field, Message string }

func (e *InvalidError) Error() string { return e.Field + ": " + e.Message }

func invalid(field, msg string) error { return &InvalidError{Field: field, Message: msg} }

// Store is the database. One writer at a time keeps the record chain in
// order.
type Store struct {
	db  *sql.DB
	now func() time.Time
	// passwordRounds is the PBKDF2 work factor; tests lower it.
	passwordRounds int
	// subjectKey turns a person's identifier into a fingerprint, so the
	// raw email or phone number is never stored.
	subjectKey []byte
}

const schema = `
CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS purposes (
  code        TEXT PRIMARY KEY,
  title       TEXT NOT NULL,
  description TEXT NOT NULL,
  created_at  TEXT NOT NULL,
  archived_at TEXT
);
CREATE TABLE IF NOT EXISTS notices (
  slug       TEXT NOT NULL,
  version    INTEGER NOT NULL,
  lang       TEXT NOT NULL,
  title      TEXT NOT NULL,
  body       TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (slug, version, lang)
);
CREATE TABLE IF NOT EXISTS forms (
  id               TEXT PRIMARY KEY,
  title            TEXT NOT NULL,
  notice_slug      TEXT NOT NULL,
  identifier_label TEXT NOT NULL,
  success_message  TEXT NOT NULL,
  active           INTEGER NOT NULL DEFAULT 1,
  created_at       TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS form_purposes (
  form_id  TEXT NOT NULL REFERENCES forms (id),
  purpose  TEXT NOT NULL REFERENCES purposes (code),
  required INTEGER NOT NULL DEFAULT 0,
  position INTEGER NOT NULL,
  PRIMARY KEY (form_id, purpose)
);
CREATE TABLE IF NOT EXISTS api_keys (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  key_hash   TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL,
  revoked_at TEXT
);
CREATE TABLE IF NOT EXISTS records (
  seq            INTEGER PRIMARY KEY AUTOINCREMENT,
  at             TEXT NOT NULL,
  subject        TEXT NOT NULL,
  action         TEXT NOT NULL CHECK (action IN ('grant', 'withdraw')),
  purpose        TEXT NOT NULL,
  notice_slug    TEXT NOT NULL,
  notice_version INTEGER NOT NULL,
  lang           TEXT NOT NULL,
  form_id        TEXT NOT NULL,
  source         TEXT NOT NULL,
  prev_hash      TEXT NOT NULL,
  hash           TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS records_subject ON records (subject, purpose, seq);
CREATE TRIGGER IF NOT EXISTS records_no_update BEFORE UPDATE ON records
BEGIN SELECT RAISE(ABORT, 'consent records cannot be changed'); END;
CREATE TRIGGER IF NOT EXISTS records_no_delete BEFORE DELETE ON records
BEGIN SELECT RAISE(ABORT, 'consent records cannot be deleted'); END;
CREATE TABLE IF NOT EXISTS admins (
  id          INTEGER PRIMARY KEY CHECK (id = 1),
  email       TEXT NOT NULL,
  pass_hash   TEXT NOT NULL,
  totp_secret TEXT NOT NULL DEFAULT '',
  totp_on     INTEGER NOT NULL DEFAULT 0,
  totp_last   INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  expires_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS webhooks (
  id         TEXT PRIMARY KEY,
  url        TEXT NOT NULL,
  secret     TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS deliveries (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  webhook_id TEXT NOT NULL,
  event      TEXT NOT NULL,
  payload    TEXT NOT NULL,
  attempts   INTEGER NOT NULL DEFAULT 0,
  next_at    TEXT NOT NULL,
  status     TEXT NOT NULL DEFAULT 'pending',
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS deliveries_due ON deliveries (status, next_at);
CREATE TRIGGER IF NOT EXISTS notices_no_update BEFORE UPDATE ON notices
BEGIN SELECT RAISE(ABORT, 'a published notice version cannot be changed'); END;
`

// Open opens (and if needed creates) the database at path.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite3", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(wal)&_pragma=foreign_keys(on)&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	// One connection: SQLite has one writer, and the chain needs order.
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: schema: %w", err)
	}
	s := &Store{db: db, now: time.Now, passwordRounds: 600000}
	key, err := s.secret(ctx, "subject_key")
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	s.subjectKey = key
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// secret returns a random 32-byte key kept in settings, creating it on
// first use. The keys never leave the database file.
func (s *Store) secret(ctx context.Context, name string) ([]byte, error) {
	var v string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", name).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		v = hex.EncodeToString(raw)
		if _, err := s.db.ExecContext(ctx, "INSERT INTO settings (key, value) VALUES (?, ?)", name, v); err != nil {
			return nil, fmt.Errorf("store: save %s: %w", name, err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("store: read %s: %w", name, err)
	}
	return hex.DecodeString(v)
}

// Setting reads one setting; ok is false when it is not set.
func (s *Store) Setting(ctx context.Context, key string) (value string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

// SetSetting saves one setting. Keys ending in _key are reserved.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	if strings.HasSuffix(key, "_key") {
		return invalid("key", "is reserved")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// Subject turns an identifier (an email address, phone number or your own
// user ID) into the fingerprint stored with each record. Identifiers are
// trimmed and lower-cased first, so "A@B.com " and "a@b.com" match.
func (s *Store) Subject(identifier string) (string, error) {
	id := strings.ToLower(strings.TrimSpace(identifier))
	if id == "" || len(id) > 320 {
		return "", invalid("identifier", "must be 1 to 320 characters")
	}
	m := hmac.New(sha256.New, s.subjectKey)
	m.Write([]byte(id))
	return hex.EncodeToString(m.Sum(nil)), nil
}

// ManageToken is what a person presents to see and withdraw their own
// consent. It is the subject fingerprint signed with a second key, so it
// can be checked without being stored and cannot be guessed from an email.
func (s *Store) ManageToken(ctx context.Context, subject string) (string, error) {
	key, err := s.secret(ctx, "manage_key")
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(subject))
	return subject + "." + hex.EncodeToString(m.Sum(nil)[:16]), nil
}

// SubjectFromToken checks a manage token and returns its subject.
func (s *Store) SubjectFromToken(ctx context.Context, token string) (string, error) {
	subject, _, ok := strings.Cut(token, ".")
	if !ok || len(subject) != 64 {
		return "", ErrNotFound
	}
	want, err := s.ManageToken(ctx, subject)
	if err != nil {
		return "", err
	}
	if !hmac.Equal([]byte(want), []byte(token)) {
		return "", ErrNotFound
	}
	return subject, nil
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parse(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}

func isCode(v string, max int) bool {
	if v == "" || len(v) > max {
		return false
	}
	for i, r := range v {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' && i > 0 || (r == '_' || r == '-') && i > 0
		if !ok {
			return false
		}
	}
	return true
}

func text(field, v string, min, max int) (string, error) {
	v = strings.TrimSpace(v)
	if n := len([]rune(v)); n < min || n > max {
		return "", invalid(field, fmt.Sprintf("must be %d to %d characters", min, max))
	}
	return v, nil
}
