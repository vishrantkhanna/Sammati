package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sammati.db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	s.passwordRounds = 1000
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func seed(t *testing.T, s *Store) {
	t.Helper()
	ctx := t.Context()
	for _, p := range [][3]string{{"account", "Account", "Create and run your account."}, {"marketing_email", "Marketing Email", "Send you product news by email."}} {
		if _, err := s.CreatePurpose(ctx, p[0], p[1], p[2]); err != nil {
			t.Fatal(err)
		}
	}
	if v, err := s.PublishNotice(ctx, "signup", []NoticeText{{"en", "Privacy Notice", "We use your email to run your account."}, {"hi", "गोपनीयता सूचना", "हम आपके ईमेल का उपयोग करते हैं।"}}); err != nil || v != 1 {
		t.Fatal(v, err)
	}
	if _, err := s.CreateForm(ctx, Form{ID: "signup", Title: "Sign Up", NoticeSlug: "signup",
		Purposes: []FormPurpose{{Purpose: "account", Required: true}, {Purpose: "marketing_email"}}}); err != nil {
		t.Fatal(err)
	}
}

func isInvalid(err error) bool {
	var ie *InvalidError
	return errors.As(err, &ie)
}

func TestGrantWithdrawAndState(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	ctx := t.Context()

	if _, err := s.Grant(ctx, "signup", "a@example.com", []string{"marketing_email"}, "en", SourceForm); !isInvalid(err) {
		t.Fatalf("missing required purpose: %v", err)
	}
	if _, err := s.Grant(ctx, "signup", "a@example.com", []string{"account", "other"}, "en", SourceForm); !isInvalid(err) {
		t.Fatalf("purpose not on the form: %v", err)
	}
	recs, err := s.Grant(ctx, "signup", " A@Example.com ", []string{"account", "marketing_email"}, "hi", SourceForm)
	if err != nil || len(recs) != 2 || recs[0].Lang != "hi" || recs[0].NoticeVersion != 1 {
		t.Fatalf("grant = %+v %v", recs, err)
	}
	subject, _ := s.Subject("a@example.com")
	if recs[0].Subject != subject || len(subject) != 64 {
		t.Fatal("identifier was not normalised into one fingerprint")
	}

	// A new notice version is stored with later consent only.
	if v, _ := s.PublishNotice(ctx, "signup", []NoticeText{{"en", "Privacy Notice", "Updated."}}); v != 2 {
		t.Fatal("version", v)
	}
	again, err := s.Grant(ctx, "signup", "b@example.com", []string{"account"}, "hi", SourceForm)
	if err != nil || again[0].NoticeVersion != 2 || again[0].Lang != "en" {
		t.Fatalf("fallback to English on version 2 = %+v %v", again, err)
	}

	w, err := s.Withdraw(ctx, subject, []string{"marketing_email", "marketing_email", "never_given"}, SourceManage)
	if err != nil || len(w) != 1 {
		t.Fatalf("withdraw = %+v %v", w, err)
	}
	if w2, _ := s.Withdraw(ctx, subject, []string{"marketing_email"}, SourceManage); len(w2) != 0 {
		t.Fatal("withdrawing twice wrote a second record")
	}
	states, err := s.States(ctx, subject)
	if err != nil || len(states) != 2 || !states[0].Granted || states[1].Granted {
		t.Fatalf("states = %+v %v", states, err)
	}
	list, _ := s.Records(ctx, RecordFilter{Subject: subject})
	if len(list) != 3 || list[0].Action != "withdraw" {
		t.Fatalf("records = %+v", list)
	}
}

func TestChainDetectsTampering(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	ctx := t.Context()
	for _, who := range []string{"a@example.com", "b@example.com", "c@example.com"} {
		if _, err := s.Grant(ctx, "signup", who, []string{"account"}, "en", SourceAPI); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := s.Verify(ctx)
	if err != nil || !rep.OK || rep.Records != 3 {
		t.Fatalf("verify = %+v %v", rep, err)
	}
	// The database itself refuses changes.
	if _, err := s.db.ExecContext(ctx, "UPDATE records SET purpose = 'x' WHERE seq = 2"); err == nil {
		t.Fatal("update allowed")
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM records WHERE seq = 2"); err == nil {
		t.Fatal("delete allowed")
	}
	// Someone with the file removes the guard and edits a record.
	if _, err := s.db.ExecContext(ctx, "DROP TRIGGER records_no_update; UPDATE records SET action = 'withdraw' WHERE seq = 2"); err != nil {
		t.Fatal(err)
	}
	rep, _ = s.Verify(ctx)
	if rep.OK || rep.BrokenAt != 2 {
		t.Fatalf("tampering not found: %+v", rep)
	}
}

func TestManageTokenAndKeys(t *testing.T) {
	s, path := open(t)
	ctx := t.Context()
	subject, _ := s.Subject("a@example.com")
	tok, err := s.ManageToken(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.SubjectFromToken(ctx, tok); err != nil || got != subject {
		t.Fatal(got, err)
	}
	if _, err := s.SubjectFromToken(ctx, subject+".00000000000000000000000000000000"); !errors.Is(err, ErrNotFound) {
		t.Fatal("forged token accepted")
	}
	id, key, err := s.CreateAPIKey(ctx, "ci")
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.CheckAPIKey(ctx, key); !ok {
		t.Fatal("key refused")
	}
	if ok, _ := s.CheckAPIKey(ctx, key+"0"); ok {
		t.Fatal("wrong key accepted")
	}
	if err := s.RevokeAPIKey(ctx, id); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.CheckAPIKey(ctx, key); ok {
		t.Fatal("revoked key accepted")
	}
	// Fingerprints survive a restart: the key lives in the file.
	_ = s.Close()
	s2, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if again, _ := s2.Subject("a@example.com"); again != subject {
		t.Fatal("fingerprint changed after reopening")
	}
}

func TestValidation(t *testing.T) {
	s, _ := open(t)
	ctx := t.Context()
	if _, err := s.CreatePurpose(ctx, "Bad Code", "x", "y"); !isInvalid(err) {
		t.Fatal(err)
	}
	if _, err := s.PublishNotice(ctx, "n", []NoticeText{{"hi", "t", "b"}}); !isInvalid(err) {
		t.Fatal("notice without English accepted")
	}
	if _, err := s.CreateForm(ctx, Form{ID: "f", Title: "F", NoticeSlug: "missing", Purposes: []FormPurpose{{Purpose: "p"}}}); !isInvalid(err) {
		t.Fatal(err)
	}
	if _, err := s.Grant(ctx, "nope", "a@example.com", []string{"x"}, "en", SourceForm); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestAdminAndTwoStep(t *testing.T) {
	s, _ := open(t)
	ctx := t.Context()
	now := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	if err := s.Setup(ctx, "Acme", "a@example.com", "short", true); !isInvalid(err) {
		t.Fatal("short password accepted:", err)
	}
	if err := s.Setup(ctx, "Acme", "a@example.com", "a long enough password", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SignIn(ctx, "a@example.com", "not the password", ""); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	tok, err := s.SignIn(ctx, "A@example.com", "a long enough password", "")
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.CheckSession(ctx, tok); !ok {
		t.Fatal("session refused")
	}
	secret, _, err := s.BeginTwoStep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := b32.DecodeString(secret)
	code := totp(key, now.Unix()/30)
	if err := s.ConfirmTwoStep(ctx, "000000"); !isInvalid(err) && code != "000000" {
		t.Fatal("wrong code confirmed:", err)
	}
	if err := s.ConfirmTwoStep(ctx, code); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SignIn(ctx, "a@example.com", "a long enough password", ""); !errors.Is(err, ErrCodeNeeded) {
		t.Fatal("signed in without a code:", err)
	}
	if _, err := s.SignIn(ctx, "a@example.com", "a long enough password", code); !errors.Is(err, ErrDenied) {
		t.Fatal("a used code worked twice:", err)
	}
	now = now.Add(30 * time.Second)
	if _, err := s.SignIn(ctx, "a@example.com", "a long enough password", totp(key, now.Unix()/30)); err != nil {
		t.Fatal(err)
	}
	now = now.Add(SessionTTL + time.Minute)
	if ok, _ := s.CheckSession(ctx, tok); ok {
		t.Fatal("session outlived its time")
	}
	if err := s.ResetPassword(ctx, "another long password"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SignIn(ctx, "a@example.com", "another long password", ""); err != nil {
		t.Fatal("reset did not switch two-step off:", err)
	}
}

// RFC 6238 test vector for SHA-1, truncated to six digits.
func TestTOTPVector(t *testing.T) {
	if got := totp([]byte("12345678901234567890"), 59/30); got != "287082" {
		t.Fatal(got)
	}
}

func TestWebhookQueue(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	ctx := t.Context()
	now := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	if _, err := s.CreateWebhook(ctx, "ftp://example.com"); !isInvalid(err) {
		t.Fatal(err)
	}
	w, err := s.CreateWebhook(ctx, "https://example.com/hook")
	if err != nil || !strings.HasPrefix(w.Secret, "whsec_") {
		t.Fatal(w, err)
	}
	if _, err := s.Grant(ctx, "signup", "a@example.com", []string{"account"}, "en", SourceForm); err != nil {
		t.Fatal(err)
	}
	due, err := s.DueDeliveries(ctx, 10)
	if err != nil || len(due) != 1 || due[0].Event != "consent.granted" || !strings.Contains(due[0].Payload, `"purpose":"account"`) {
		t.Fatalf("due = %+v %v", due, err)
	}
	if strings.Contains(due[0].Payload, "a@example.com") {
		t.Fatal("the identifier is in the webhook body")
	}
	if err := s.FinishDelivery(ctx, due[0], "the receiver answered 500"); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.DueDeliveries(ctx, 10); len(again) != 0 {
		t.Fatal("retried without waiting")
	}
	now = now.Add(2 * time.Minute)
	again, _ := s.DueDeliveries(ctx, 10)
	if len(again) != 1 || again[0].Attempts != 1 {
		t.Fatalf("retry = %+v", again)
	}
	if err := s.FinishDelivery(ctx, again[0], ""); err != nil {
		t.Fatal(err)
	}
	list, _ := s.RecentDeliveries(ctx, 10)
	if len(list) != 1 || list[0].Status != "sent" {
		t.Fatalf("deliveries = %+v", list)
	}
}

func TestLogo(t *testing.T) {
	s, _ := open(t)
	ctx := t.Context()
	if _, _, err := s.Logo(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	if err := s.SetLogo(ctx, png); err != nil {
		t.Fatal(err)
	}
	data, kind, err := s.Logo(ctx)
	if err != nil || kind != "image/png" || len(data) != len(png) {
		t.Fatal(kind, len(data), err)
	}
	for name, bad := range map[string][]byte{
		"svg":   []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"html":  []byte("<html><script>alert(1)</script>"),
		"large": append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, MaxLogoBytes)...),
	} {
		if err := s.SetLogo(ctx, bad); !isInvalid(err) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
	if err := s.RemoveLogo(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Logo(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatal("logo still there")
	}
}
