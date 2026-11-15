package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// Purpose is one reason personal data is used, such as marketing_email.
type Purpose struct {
	Code        string    `json:"code"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	Archived    bool      `json:"archived"`
}

// CreatePurpose adds a purpose. The code cannot be changed later, because
// consent records refer to it.
func (s *Store) CreatePurpose(ctx context.Context, code, title, description string) (Purpose, error) {
	var err error
	if !isCode(code, 64) {
		return Purpose{}, invalid("code", "use lowercase letters, digits, _ or -, starting with a letter")
	}
	if title, err = text("title", title, 1, 120); err != nil {
		return Purpose{}, err
	}
	if description, err = text("description", description, 1, 1000); err != nil {
		return Purpose{}, err
	}
	p := Purpose{Code: code, Title: title, Description: description, CreatedAt: s.now().UTC()}
	_, err = s.db.ExecContext(ctx, "INSERT INTO purposes (code, title, description, created_at) VALUES (?, ?, ?, ?)",
		p.Code, p.Title, p.Description, stamp(p.CreatedAt))
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return Purpose{}, invalid("code", "is already in use")
	}
	return p, err
}

// Purposes lists every purpose, oldest first.
func (s *Store) Purposes(ctx context.Context) ([]Purpose, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT code, title, description, created_at, archived_at IS NOT NULL FROM purposes ORDER BY created_at, code")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Purpose{}
	for rows.Next() {
		var p Purpose
		var at string
		if err := rows.Scan(&p.Code, &p.Title, &p.Description, &at, &p.Archived); err != nil {
			return nil, err
		}
		p.CreatedAt = parse(at)
		out = append(out, p)
	}
	return out, rows.Err()
}

// Notice is one published version of a privacy notice in one language.
// Versions are never changed; publishing again adds the next version.
type Notice struct {
	Slug      string    `json:"slug"`
	Version   int       `json:"version"`
	Lang      string    `json:"lang"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// NoticeText is the wording of a notice in one language.
type NoticeText struct {
	Lang  string `json:"lang"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// PublishNotice publishes the next version of a notice in one or more
// languages. English ("en") is required, as the fallback.
func (s *Store) PublishNotice(ctx context.Context, slug string, texts []NoticeText) (int, error) {
	if !isCode(slug, 64) {
		return 0, invalid("slug", "use lowercase letters, digits, _ or -, starting with a letter")
	}
	seen := map[string]bool{}
	for i := range texts {
		t := &texts[i]
		var err error
		if !isCode(t.Lang, 10) || seen[t.Lang] {
			return 0, invalid("lang", "give each language once, as a code such as en or hi")
		}
		seen[t.Lang] = true
		if t.Title, err = text("title", t.Title, 1, 200); err != nil {
			return 0, err
		}
		if t.Body, err = text("body", t.Body, 1, 20000); err != nil {
			return 0, err
		}
	}
	if !seen["en"] {
		return 0, invalid("lang", "include English (en); it is shown when a language is missing")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var version int
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(version), 0) + 1 FROM notices WHERE slug = ?", slug).Scan(&version); err != nil {
		return 0, err
	}
	at := stamp(s.now())
	for _, t := range texts {
		if _, err := tx.ExecContext(ctx, "INSERT INTO notices (slug, version, lang, title, body, created_at) VALUES (?, ?, ?, ?, ?, ?)",
			slug, version, t.Lang, t.Title, t.Body, at); err != nil {
			return 0, err
		}
	}
	return version, tx.Commit()
}

type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// latestNotice returns the newest version of a notice in lang, falling
// back to English.
func latestNotice(ctx context.Context, q querier, slug, lang string) (Notice, error) {
	n := Notice{Slug: slug}
	var at string
	err := q.QueryRowContext(ctx, `SELECT version, lang, title, body, created_at FROM notices
		WHERE slug = ?1 AND version = (SELECT max(version) FROM notices WHERE slug = ?1) AND lang IN (?2, 'en')
		ORDER BY lang = ?2 DESC LIMIT 1`, slug, lang).Scan(&n.Version, &n.Lang, &n.Title, &n.Body, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return n, ErrNotFound
	}
	n.CreatedAt = parse(at)
	return n, err
}

// Notice returns the newest version of a notice in lang, or in English
// when that language was not published.
func (s *Store) Notice(ctx context.Context, slug, lang string) (Notice, error) {
	return latestNotice(ctx, s.db, slug, lang)
}

// Notices lists every version of every notice, newest first.
func (s *Store) Notices(ctx context.Context) ([]Notice, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT slug, version, lang, title, body, created_at FROM notices ORDER BY slug, version DESC, lang")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Notice{}
	for rows.Next() {
		var n Notice
		var at string
		if err := rows.Scan(&n.Slug, &n.Version, &n.Lang, &n.Title, &n.Body, &at); err != nil {
			return nil, err
		}
		n.CreatedAt = parse(at)
		out = append(out, n)
	}
	return out, rows.Err()
}

// FormPurpose is a purpose offered on a form. A required purpose must be
// ticked for the form to be submitted; use it only for what the service
// cannot work without.
type FormPurpose struct {
	Purpose     string `json:"purpose"`
	Required    bool   `json:"required"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// Form is a consent form: one notice and the purposes a person can agree to.
type Form struct {
	ID              string        `json:"id"`
	Title           string        `json:"title"`
	NoticeSlug      string        `json:"notice_slug"`
	IdentifierLabel string        `json:"identifier_label"`
	SuccessMessage  string        `json:"success_message"`
	Active          bool          `json:"active"`
	Purposes        []FormPurpose `json:"purposes"`
	CreatedAt       time.Time     `json:"created_at"`
}

// CreateForm adds a form. Its notice and purposes must exist.
func (s *Store) CreateForm(ctx context.Context, f Form) (Form, error) {
	var err error
	if !isCode(f.ID, 64) {
		return Form{}, invalid("id", "use lowercase letters, digits, _ or -, starting with a letter")
	}
	if f.Title, err = text("title", f.Title, 1, 120); err != nil {
		return Form{}, err
	}
	if f.IdentifierLabel == "" {
		f.IdentifierLabel = "Email address"
	}
	if f.IdentifierLabel, err = text("identifier_label", f.IdentifierLabel, 1, 60); err != nil {
		return Form{}, err
	}
	if f.SuccessMessage == "" {
		f.SuccessMessage = "Thank you. Your choices have been saved."
	}
	if f.SuccessMessage, err = text("success_message", f.SuccessMessage, 1, 300); err != nil {
		return Form{}, err
	}
	if len(f.Purposes) == 0 || len(f.Purposes) > 20 {
		return Form{}, invalid("purposes", "choose 1 to 20 purposes")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Form{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := latestNotice(ctx, tx, f.NoticeSlug, "en"); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Form{}, invalid("notice_slug", "publish this notice first")
		}
		return Form{}, err
	}
	f.Active, f.CreatedAt = true, s.now().UTC()
	if _, err := tx.ExecContext(ctx, `INSERT INTO forms (id, title, notice_slug, identifier_label, success_message, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, f.ID, f.Title, f.NoticeSlug, f.IdentifierLabel, f.SuccessMessage, stamp(f.CreatedAt)); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Form{}, invalid("id", "is already in use")
		}
		return Form{}, err
	}
	seen := map[string]bool{}
	for i, p := range f.Purposes {
		var archived bool
		err := tx.QueryRowContext(ctx, "SELECT archived_at IS NOT NULL FROM purposes WHERE code = ?", p.Purpose).Scan(&archived)
		if errors.Is(err, sql.ErrNoRows) || archived || seen[p.Purpose] {
			return Form{}, invalid("purposes", "each purpose must exist, be active and appear once: "+p.Purpose)
		}
		if err != nil {
			return Form{}, err
		}
		seen[p.Purpose] = true
		if _, err := tx.ExecContext(ctx, "INSERT INTO form_purposes (form_id, purpose, required, position) VALUES (?, ?, ?, ?)",
			f.ID, p.Purpose, p.Required, i); err != nil {
			return Form{}, err
		}
	}
	return f, tx.Commit()
}

// Form returns one form with the titles and descriptions of its purposes.
func (s *Store) Form(ctx context.Context, id string) (Form, error) {
	f := Form{ID: id, Purposes: []FormPurpose{}}
	var at string
	err := s.db.QueryRowContext(ctx, "SELECT title, notice_slug, identifier_label, success_message, active, created_at FROM forms WHERE id = ?", id).
		Scan(&f.Title, &f.NoticeSlug, &f.IdentifierLabel, &f.SuccessMessage, &f.Active, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	if err != nil {
		return f, err
	}
	f.CreatedAt = parse(at)
	rows, err := s.db.QueryContext(ctx, `SELECT fp.purpose, fp.required, p.title, p.description FROM form_purposes fp
		JOIN purposes p ON p.code = fp.purpose WHERE fp.form_id = ? ORDER BY fp.position`, id)
	if err != nil {
		return f, err
	}
	defer rows.Close()
	for rows.Next() {
		var p FormPurpose
		if err := rows.Scan(&p.Purpose, &p.Required, &p.Title, &p.Description); err != nil {
			return f, err
		}
		f.Purposes = append(f.Purposes, p)
	}
	return f, rows.Err()
}

// Forms lists every form, newest first.
func (s *Store) Forms(ctx context.Context) ([]Form, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM forms ORDER BY created_at DESC, id")
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	out := []Form{}
	for _, id := range ids {
		f, err := s.Form(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

const keyPrefix = "smt_"

func keyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// CreateAPIKey makes an admin API key. The key is returned once and only
// its hash is stored.
func (s *Store) CreateAPIKey(ctx context.Context, name string) (id, key string, err error) {
	if name, err = text("name", name, 1, 80); err != nil {
		return "", "", err
	}
	raw := make([]byte, 28)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	id, key = hex.EncodeToString(raw[:4]), keyPrefix+hex.EncodeToString(raw[4:])
	_, err = s.db.ExecContext(ctx, "INSERT INTO api_keys (id, name, key_hash, created_at) VALUES (?, ?, ?, ?)", id, name, keyHash(key), stamp(s.now()))
	return id, key, err
}

// CheckAPIKey reports whether key is a live admin key.
func (s *Store) CheckAPIKey(ctx context.Context, key string) (bool, error) {
	if !strings.HasPrefix(key, keyPrefix) {
		return false, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM api_keys WHERE key_hash = ? AND revoked_at IS NULL", keyHash(key)).Scan(&n)
	return n == 1, err
}

// RevokeAPIKey switches a key off.
func (s *Store) RevokeAPIKey(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE api_keys SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL", stamp(s.now()), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
