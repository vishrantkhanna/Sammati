package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"time"
)

// Webhook is an address that is told about every consent decision.
type Webhook struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
	// Secret signs each delivery. It is returned only when the webhook is
	// created.
	Secret string `json:"secret,omitempty"`
}

// CreateWebhook adds a webhook and returns it with its signing secret.
func (s *Store) CreateWebhook(ctx context.Context, raw string) (Webhook, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || len(raw) > 500 {
		return Webhook{}, invalid("url", "must be an http or https address without a username or password")
	}
	b := make([]byte, 28)
	if _, err := rand.Read(b); err != nil {
		return Webhook{}, err
	}
	w := Webhook{ID: hex.EncodeToString(b[:4]), URL: u.String(), Secret: "whsec_" + hex.EncodeToString(b[4:]), CreatedAt: s.now().UTC()}
	_, err = s.db.ExecContext(ctx, "INSERT INTO webhooks (id, url, secret, created_at) VALUES (?, ?, ?, ?)", w.ID, w.URL, w.Secret, stamp(w.CreatedAt))
	return w, err
}

// Webhooks lists the webhooks, without their secrets.
func (s *Store) Webhooks(ctx context.Context) ([]Webhook, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, url, created_at FROM webhooks ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Webhook{}
	for rows.Next() {
		var w Webhook
		var at string
		if err := rows.Scan(&w.ID, &w.URL, &at); err != nil {
			return nil, err
		}
		w.CreatedAt = parse(at)
		out = append(out, w)
	}
	return out, rows.Err()
}

// DeleteWebhook removes a webhook and anything still waiting to be sent
// to it.
func (s *Store) DeleteWebhook(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM webhooks WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, err = s.db.ExecContext(ctx, "DELETE FROM deliveries WHERE webhook_id = ? AND status = 'pending'", id)
	return err
}

// Event is the body sent to a webhook.
type Event struct {
	Event     string    `json:"event"`
	CreatedAt time.Time `json:"created_at"`
	Subject   string    `json:"subject"`
	FormID    string    `json:"form_id"`
	Source    string    `json:"source"`
	Records   []Record  `json:"records"`
}

// enqueue queues one event for every webhook, in the same transaction as
// the records it describes, so a decision is never saved without its
// event or the other way round.
func enqueue(ctx context.Context, tx *sql.Tx, event string, recs []Record, now time.Time) error {
	if len(recs) == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM webhooks")
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil || len(ids) == 0 {
		return err
	}
	body, err := json.Marshal(Event{Event: event, CreatedAt: now.UTC(), Subject: recs[0].Subject, FormID: recs[0].FormID, Source: recs[0].Source, Records: recs})
	if err != nil {
		return err
	}
	at := stamp(now)
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, "INSERT INTO deliveries (webhook_id, event, payload, next_at, created_at) VALUES (?, ?, ?, ?, ?)",
			id, event, string(body), at, at); err != nil {
			return err
		}
	}
	return nil
}

// Delivery is one event on its way to one webhook.
type Delivery struct {
	ID        int64     `json:"id"`
	WebhookID string    `json:"webhook_id"`
	Event     string    `json:"event"`
	Attempts  int       `json:"attempts"`
	Status    string    `json:"status"`
	LastError string    `json:"last_error"`
	CreatedAt time.Time `json:"created_at"`
	// Set only for sending.
	URL     string `json:"-"`
	Secret  string `json:"-"`
	Payload string `json:"-"`
}

// DueDeliveries returns deliveries that are ready to send.
func (s *Store) DueDeliveries(ctx context.Context, limit int) ([]Delivery, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, d.webhook_id, d.event, d.attempts, d.payload, w.url, w.secret
		FROM deliveries d JOIN webhooks w ON w.id = d.webhook_id
		WHERE d.status = 'pending' AND d.next_at <= ? ORDER BY d.id LIMIT ?`, stamp(s.now()), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		var d Delivery
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.Event, &d.Attempts, &d.Payload, &d.URL, &d.Secret); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// backoff is the wait before each retry. After the last one a delivery is
// marked failed.
var backoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour}

// FinishDelivery records the result of one attempt.
func (s *Store) FinishDelivery(ctx context.Context, d Delivery, problem string) error {
	if problem == "" {
		_, err := s.db.ExecContext(ctx, "UPDATE deliveries SET status = 'sent', attempts = attempts + 1, last_error = '' WHERE id = ?", d.ID)
		return err
	}
	if len(problem) > 300 {
		problem = problem[:300]
	}
	if d.Attempts >= len(backoff) {
		_, err := s.db.ExecContext(ctx, "UPDATE deliveries SET status = 'failed', attempts = attempts + 1, last_error = ? WHERE id = ?", problem, d.ID)
		return err
	}
	_, err := s.db.ExecContext(ctx, "UPDATE deliveries SET attempts = attempts + 1, last_error = ?, next_at = ? WHERE id = ?",
		problem, stamp(s.now().Add(backoff[d.Attempts])), d.ID)
	return err
}

// RecentDeliveries lists the latest deliveries, newest first.
func (s *Store) RecentDeliveries(ctx context.Context, limit int) ([]Delivery, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, webhook_id, event, attempts, status, last_error, created_at FROM deliveries ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		var d Delivery
		var at string
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.Event, &d.Attempts, &d.Status, &d.LastError, &at); err != nil {
			return nil, err
		}
		d.CreatedAt = parse(at)
		out = append(out, d)
	}
	return out, rows.Err()
}
