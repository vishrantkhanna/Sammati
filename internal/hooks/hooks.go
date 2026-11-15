// Package hooks sends queued webhook events.
package hooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/vishrantkhanna/sammati/internal/store"
)

// Sign returns the X-Sammati-Signature value for a body sent at t:
// "t=<unix seconds>,v1=<hex HMAC-SHA256 of "<t>.<body>">". Receivers
// recompute it with the webhook's secret and compare.
func Sign(secret string, t time.Time, body []byte) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts + "."))
	m.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(m.Sum(nil))
}

// Sender delivers events until its context ends.
type Sender struct {
	Store  *store.Store
	Log    *slog.Logger
	Client *http.Client
	Every  time.Duration
}

// New returns a sender with safe defaults: a ten-second timeout and no
// following of redirects.
func New(s *store.Store, log *slog.Logger) *Sender {
	return &Sender{Store: s, Log: log, Every: 10 * time.Second, Client: &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// Run sends due deliveries on a timer.
func (s *Sender) Run(ctx context.Context) {
	t := time.NewTicker(s.Every)
	defer t.Stop()
	for {
		s.Once(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Once sends everything that is due now.
func (s *Sender) Once(ctx context.Context) {
	due, err := s.Store.DueDeliveries(ctx, 50)
	if err != nil {
		if ctx.Err() == nil {
			s.Log.ErrorContext(ctx, "webhooks: read queue", "err", err)
		}
		return
	}
	for _, d := range due {
		problem := s.send(ctx, d)
		if err := s.Store.FinishDelivery(ctx, d, problem); err != nil && ctx.Err() == nil {
			s.Log.ErrorContext(ctx, "webhooks: save result", "err", err)
		}
	}
}

func (s *Sender) send(ctx context.Context, d store.Delivery) string {
	body := []byte(d.Payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL, bytes.NewReader(body))
	if err != nil {
		return "the address is not valid"
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Sammati-Webhook")
	req.Header.Set("X-Sammati-Event", d.Event)
	req.Header.Set("X-Sammati-Delivery", strconv.FormatInt(d.ID, 10))
	req.Header.Set("X-Sammati-Signature", Sign(d.Secret, time.Now(), body))
	res, err := s.Client.Do(req)
	if err != nil {
		return "could not connect"
	}
	_ = res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Sprintf("the receiver answered %d", res.StatusCode)
	}
	return ""
}
