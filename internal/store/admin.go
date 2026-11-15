package store

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // TOTP is defined on HMAC-SHA1 (RFC 6238)
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"
)

// ErrDenied is returned for a wrong password, code or session.
var ErrDenied = errors.New("denied")

// ErrCodeNeeded is returned when the password is right and a two-step
// code is still required.
var ErrCodeNeeded = errors.New("two-step code needed")

// SessionTTL is how long a sign-in lasts.
const SessionTTL = 12 * time.Hour

func (s *Store) hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, s.passwordRounds, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", s.passwordRounds, hex.EncodeToString(salt), hex.EncodeToString(key)), nil
}

func checkPassword(stored, password string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	rounds, err1 := strconv.Atoi(parts[1])
	salt, err2 := hex.DecodeString(parts[2])
	want, err3 := hex.DecodeString(parts[3])
	if err1 != nil || err2 != nil || err3 != nil || rounds < 1 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, rounds, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

func checkNewPassword(p string) error {
	if n := len([]rune(p)); n < 12 || n > 200 {
		return invalid("password", "must be 12 to 200 characters")
	}
	return nil
}

// IsSetup reports whether the first-run setup has been done.
func (s *Store) IsSetup(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM admins").Scan(&n)
	return n > 0, err
}

// Setup creates the administrator. It works once. accepted must be true:
// the person setting up confirms they are responsible for this
// installation.
func (s *Store) Setup(ctx context.Context, organisation, email, password string, accepted bool) error {
	var err error
	if !accepted {
		return invalid("accept", "confirm that you are responsible for this installation")
	}
	if organisation, err = text("organisation", organisation, 1, 120); err != nil {
		return err
	}
	addr, err := mail.ParseAddress(strings.TrimSpace(email))
	if err != nil || len(addr.Address) > 254 {
		return invalid("email", "must be an email address")
	}
	if err := checkNewPassword(password); err != nil {
		return err
	}
	hash, err := s.hashPassword(password)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM admins").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return invalid("setup", "has already been done")
	}
	at := stamp(s.now())
	if _, err := tx.ExecContext(ctx, "INSERT INTO admins (id, email, pass_hash, created_at) VALUES (1, ?, ?, ?)",
		strings.ToLower(addr.Address), hash, at); err != nil {
		return err
	}
	for k, v := range map[string]string{"organisation": organisation, "responsibility_accepted_at": at} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT (key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Admin describes the administrator account.
type Admin struct {
	Email        string `json:"email"`
	Organisation string `json:"organisation"`
	TwoStep      bool   `json:"two_step"`
}

// Admin returns the administrator account.
func (s *Store) Admin(ctx context.Context) (Admin, error) {
	var a Admin
	err := s.db.QueryRowContext(ctx, "SELECT email, totp_on FROM admins WHERE id = 1").Scan(&a.Email, &a.TwoStep)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	a.Organisation, _, err = s.Setting(ctx, "organisation")
	return a, err
}

// SignIn checks the email, password and, when two-step sign-in is on, the
// code. It returns a session token to keep in a cookie.
func (s *Store) SignIn(ctx context.Context, email, password, code string) (string, error) {
	var stored, hash string
	var on bool
	err := s.db.QueryRowContext(ctx, "SELECT email, pass_hash, totp_on FROM admins WHERE id = 1").Scan(&stored, &hash, &on)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrDenied
	}
	if err != nil {
		return "", err
	}
	// Always do the slow check, so a wrong email takes as long as a wrong
	// password.
	okPass := checkPassword(hash, password)
	if !okPass || subtle.ConstantTimeCompare([]byte(stored), []byte(strings.ToLower(strings.TrimSpace(email)))) != 1 {
		return "", ErrDenied
	}
	if on {
		if strings.TrimSpace(code) == "" {
			return "", ErrCodeNeeded
		}
		if err := s.useCode(ctx, code); err != nil {
			return "", err
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	if _, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at < ?", stamp(s.now())); err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO sessions (token_hash, expires_at) VALUES (?, ?)", keyHash(token), stamp(s.now().Add(SessionTTL)))
	return token, err
}

// CheckSession reports whether a session token is live.
func (s *Store) CheckSession(ctx context.Context, token string) (bool, error) {
	if len(token) != 64 {
		return false, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM sessions WHERE token_hash = ? AND expires_at > ?", keyHash(token), stamp(s.now())).Scan(&n)
	return n == 1, err
}

// SignOut ends one session.
func (s *Store) SignOut(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", keyHash(token))
	return err
}

// ChangePassword sets a new password and ends every session.
func (s *Store) ChangePassword(ctx context.Context, current, next string) error {
	var hash string
	if err := s.db.QueryRowContext(ctx, "SELECT pass_hash FROM admins WHERE id = 1").Scan(&hash); err != nil {
		return err
	}
	if !checkPassword(hash, current) {
		return invalid("current", "password is not right")
	}
	if err := checkNewPassword(next); err != nil {
		return err
	}
	fresh, err := s.hashPassword(next)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE admins SET pass_hash = ? WHERE id = 1", fresh); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "DELETE FROM sessions")
	return err
}

// ResetPassword sets a new password without the old one. Only the command
// line calls it, for an administrator who is locked out; it also switches
// two-step sign-in off.
func (s *Store) ResetPassword(ctx context.Context, next string) error {
	if err := checkNewPassword(next); err != nil {
		return err
	}
	fresh, err := s.hashPassword(next)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, "UPDATE admins SET pass_hash = ?, totp_on = 0, totp_secret = '' WHERE id = 1", fresh)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, err = s.db.ExecContext(ctx, "DELETE FROM sessions")
	return err
}

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// totp returns the six-digit code for a secret and a 30-second step.
func totp(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step)) //nolint:gosec // step is a positive time counter
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1000000)
}

// BeginTwoStep makes a new authenticator secret and returns it for the
// person to add to their app. It takes effect after ConfirmTwoStep.
func (s *Store) BeginTwoStep(ctx context.Context) (secret, uri string, err error) {
	a, err := s.Admin(ctx)
	if err != nil {
		return "", "", err
	}
	if a.TwoStep {
		return "", "", invalid("two_step", "is already on")
	}
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	secret = b32.EncodeToString(raw)
	if _, err := s.db.ExecContext(ctx, "UPDATE admins SET totp_secret = ? WHERE id = 1", secret); err != nil {
		return "", "", err
	}
	return secret, "otpauth://totp/Sammati:" + a.Email + "?secret=" + secret + "&issuer=Sammati", nil
}

// useCode accepts a current code once. The step before and after are
// allowed for clock drift; a code is never accepted twice.
func (s *Store) useCode(ctx context.Context, code string) error {
	var secret string
	var last int64
	if err := s.db.QueryRowContext(ctx, "SELECT totp_secret, totp_last FROM admins WHERE id = 1").Scan(&secret, &last); err != nil {
		return err
	}
	key, err := b32.DecodeString(secret)
	if err != nil || len(key) == 0 {
		return ErrDenied
	}
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	now := s.now().Unix() / 30
	for _, step := range []int64{now, now - 1, now + 1} {
		if step > last && subtle.ConstantTimeCompare([]byte(totp(key, step)), []byte(code)) == 1 {
			_, err := s.db.ExecContext(ctx, "UPDATE admins SET totp_last = ? WHERE id = 1", step)
			return err
		}
	}
	return ErrDenied
}

// ConfirmTwoStep switches two-step sign-in on once a code from the app is
// right.
func (s *Store) ConfirmTwoStep(ctx context.Context, code string) error {
	if err := s.useCode(ctx, code); err != nil {
		if errors.Is(err, ErrDenied) {
			return invalid("code", "is not right; check the time on your phone and try the next code")
		}
		return err
	}
	_, err := s.db.ExecContext(ctx, "UPDATE admins SET totp_on = 1 WHERE id = 1")
	return err
}

// DisableTwoStep switches two-step sign-in off, given the password.
func (s *Store) DisableTwoStep(ctx context.Context, password string) error {
	var hash string
	if err := s.db.QueryRowContext(ctx, "SELECT pass_hash FROM admins WHERE id = 1").Scan(&hash); err != nil {
		return err
	}
	if !checkPassword(hash, password) {
		return invalid("password", "is not right")
	}
	_, err := s.db.ExecContext(ctx, "UPDATE admins SET totp_on = 0, totp_secret = '' WHERE id = 1")
	return err
}

// APIKey describes a stored key. The key itself is never kept.
type APIKey struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Revoked   bool      `json:"revoked"`
}

// APIKeys lists every key, newest first.
func (s *Store) APIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, created_at, revoked_at IS NOT NULL FROM api_keys ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		var k APIKey
		var at string
		if err := rows.Scan(&k.ID, &k.Name, &at, &k.Revoked); err != nil {
			return nil, err
		}
		k.CreatedAt = parse(at)
		out = append(out, k)
	}
	return out, rows.Err()
}

// ArchivePurpose stops a purpose being added to new forms. Existing forms
// and records keep it.
func (s *Store) ArchivePurpose(ctx context.Context, code string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE purposes SET archived_at = ? WHERE code = ? AND archived_at IS NULL", stamp(s.now()), code)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetFormActive switches a form on or off. A form that is off cannot be
// shown or submitted.
func (s *Store) SetFormActive(ctx context.Context, id string, active bool) error {
	res, err := s.db.ExecContext(ctx, "UPDATE forms SET active = ? WHERE id = ?", active, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Stats are the numbers on the overview screen.
type Stats struct {
	Purposes     int `json:"purposes"`
	Forms        int `json:"forms"`
	Records      int `json:"records"`
	People       int `json:"people"`
	Grants30     int `json:"grants_30_days"`
	Withdrawn30  int `json:"withdrawals_30_days"`
	HooksPending int `json:"webhooks_pending"`
	HooksFailed  int `json:"webhooks_failed"`
}

// Stats counts what is in the database.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var st Stats
	since := stamp(s.now().AddDate(0, 0, -30))
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM purposes WHERE archived_at IS NULL),
		(SELECT count(*) FROM forms WHERE active = 1),
		(SELECT count(*) FROM records),
		(SELECT count(DISTINCT subject) FROM records),
		(SELECT count(*) FROM records WHERE action = 'grant' AND at >= ?1),
		(SELECT count(*) FROM records WHERE action = 'withdraw' AND at >= ?1),
		(SELECT count(*) FROM deliveries WHERE status = 'pending'),
		(SELECT count(*) FROM deliveries WHERE status = 'failed')`, since).
		Scan(&st.Purposes, &st.Forms, &st.Records, &st.People, &st.Grants30, &st.Withdrawn30, &st.HooksPending, &st.HooksFailed)
	return st, err
}
