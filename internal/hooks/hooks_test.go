package hooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vishrantkhanna/sammati/internal/store"
)

func TestDeliverySignedAndRetried(t *testing.T) {
	ctx := t.Context()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var secret string
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		ts, sig, _ := strings.Cut(strings.TrimPrefix(r.Header.Get("X-Sammati-Signature"), "t="), ",v1=")
		m := hmac.New(sha256.New, []byte(secret))
		m.Write([]byte(ts + "." + string(body)))
		if sig != hex.EncodeToString(m.Sum(nil)) || r.Header.Get("X-Sammati-Event") != "consent.granted" {
			t.Errorf("bad signature or event: %q", r.Header.Get("X-Sammati-Signature"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if _, err := s.CreatePurpose(ctx, "news", "News", "Send news."); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishNotice(ctx, "n", []store.NoticeText{{Lang: "en", Title: "T", Body: "B"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateForm(ctx, store.Form{ID: "f", Title: "F", NoticeSlug: "n", Purposes: []store.FormPurpose{{Purpose: "news"}}}); err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateWebhook(ctx, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	secret = w.Secret
	if _, err := s.Grant(ctx, "f", "a@example.com", []string{"news"}, "en", store.SourceForm); err != nil {
		t.Fatal(err)
	}
	sender := New(s, slog.New(slog.NewTextHandler(io.Discard, nil)))
	sender.Once(ctx)
	sender.Once(ctx)
	if calls != 1 {
		t.Fatal("calls:", calls)
	}
	list, _ := s.RecentDeliveries(ctx, 5)
	if len(list) != 1 || list[0].Status != "sent" {
		t.Fatalf("%+v", list)
	}
}
