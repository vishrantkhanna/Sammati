package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Record is one consent decision. Records are only ever added. Each holds
// the hash of the one before it, so a changed or missing record breaks the
// chain and Verify finds it.
type Record struct {
	Seq           int64     `json:"seq"`
	At            time.Time `json:"at"`
	Subject       string    `json:"subject"`
	Action        string    `json:"action"`
	Purpose       string    `json:"purpose"`
	NoticeSlug    string    `json:"notice_slug"`
	NoticeVersion int       `json:"notice_version"`
	Lang          string    `json:"lang"`
	FormID        string    `json:"form_id"`
	Source        string    `json:"source"`
	PrevHash      string    `json:"prev_hash"`
	Hash          string    `json:"hash"`
}

// Sources say how a decision arrived.
const (
	SourceForm   = "form"   // the hosted form or the website widget
	SourceAPI    = "api"    // your own system, through the admin API
	SourceManage = "manage" // the person, on their manage page
)

// genesis is the previous hash of the first record.
const genesis = "0000000000000000000000000000000000000000000000000000000000000000"

// digest is the hash of a record: SHA-256 over its fields in a fixed
// order, one per line. Anyone can recompute it from an export.
func digest(r Record, at string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%d\n%s\n%s\n%s\n%s\n%s\n%d\n%s\n%s\n%s\n", r.PrevHash, r.Seq, at, r.Subject, r.Action, r.Purpose,
		r.NoticeSlug, r.NoticeVersion, r.Lang, r.FormID, r.Source)
	return hex.EncodeToString(h.Sum(nil))
}

func appendRecord(ctx context.Context, tx *sql.Tx, r Record, now time.Time) (Record, error) {
	var last sql.NullInt64
	r.PrevHash = genesis
	if err := tx.QueryRowContext(ctx, "SELECT max(seq) FROM records").Scan(&last); err != nil {
		return r, err
	}
	if last.Valid {
		if err := tx.QueryRowContext(ctx, "SELECT hash FROM records WHERE seq = ?", last.Int64).Scan(&r.PrevHash); err != nil {
			return r, err
		}
	}
	r.Seq, r.At = last.Int64+1, now.UTC()
	at := stamp(r.At)
	r.Hash = digest(r, at)
	_, err := tx.ExecContext(ctx, `INSERT INTO records (seq, at, subject, action, purpose, notice_slug, notice_version, lang, form_id, source, prev_hash, hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, r.Seq, at, r.Subject, r.Action, r.Purpose, r.NoticeSlug, r.NoticeVersion,
		r.Lang, r.FormID, r.Source, r.PrevHash, r.Hash)
	return r, err
}

// Grant records consent given on a form. granted holds the purposes the
// person ticked. Every required purpose of the form must be among them,
// and purposes that are not on the form are refused. The notice version
// shown at the time is stored with each record.
func (s *Store) Grant(ctx context.Context, formID, identifier string, granted []string, lang, source string) ([]Record, error) {
	subject, err := s.Subject(identifier)
	if err != nil {
		return nil, err
	}
	form, err := s.Form(ctx, formID)
	if err != nil {
		return nil, err
	}
	if !form.Active {
		return nil, invalid("form", "is switched off")
	}
	want := map[string]bool{}
	for _, g := range granted {
		want[g] = true
	}
	var codes []string
	for _, p := range form.Purposes {
		if p.Required && !want[p.Purpose] {
			return nil, invalid("purposes", p.Title+" is required to continue")
		}
		if want[p.Purpose] {
			codes = append(codes, p.Purpose)
			delete(want, p.Purpose)
		}
	}
	if len(want) > 0 {
		return nil, invalid("purposes", "include only purposes that are on this form")
	}
	if len(codes) == 0 {
		return nil, invalid("purposes", "choose at least one")
	}
	if lang == "" {
		lang = "en"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	notice, err := latestNotice(ctx, tx, form.NoticeSlug, lang)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(codes))
	for _, code := range codes {
		r, err := appendRecord(ctx, tx, Record{Subject: subject, Action: "grant", Purpose: code, NoticeSlug: notice.Slug,
			NoticeVersion: notice.Version, Lang: notice.Lang, FormID: form.ID, Source: source}, s.now())
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := enqueue(ctx, tx, "consent.granted", out, s.now()); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

// Withdraw records withdrawal of consent for the given purposes. Purposes
// with no consent in force are skipped, so it is safe to repeat.
func (s *Store) Withdraw(ctx context.Context, subject string, purposes []string, source string) ([]Record, error) {
	if len(subject) != 64 {
		return nil, invalid("subject", "is not valid")
	}
	if len(purposes) == 0 {
		return nil, invalid("purposes", "choose at least one")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	out := []Record{}
	done := map[string]bool{}
	for _, code := range purposes {
		if done[code] {
			continue
		}
		done[code] = true
		var last Record
		err := tx.QueryRowContext(ctx, `SELECT action, notice_slug, notice_version, lang, form_id FROM records
			WHERE subject = ? AND purpose = ? ORDER BY seq DESC LIMIT 1`, subject, code).
			Scan(&last.Action, &last.NoticeSlug, &last.NoticeVersion, &last.Lang, &last.FormID)
		if errors.Is(err, sql.ErrNoRows) || err == nil && last.Action != "grant" {
			continue
		}
		if err != nil {
			return nil, err
		}
		r, err := appendRecord(ctx, tx, Record{Subject: subject, Action: "withdraw", Purpose: code, NoticeSlug: last.NoticeSlug,
			NoticeVersion: last.NoticeVersion, Lang: last.Lang, FormID: last.FormID, Source: source}, s.now())
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := enqueue(ctx, tx, "consent.withdrawn", out, s.now()); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

// State is where one person stands on one purpose.
type State struct {
	Purpose       string    `json:"purpose"`
	Title         string    `json:"title"`
	Granted       bool      `json:"granted"`
	Since         time.Time `json:"since"`
	NoticeSlug    string    `json:"notice_slug"`
	NoticeVersion int       `json:"notice_version"`
}

// States returns a person's current position on every purpose they have
// ever decided on.
func (s *Store) States(ctx context.Context, subject string) ([]State, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.purpose, coalesce(p.title, r.purpose), r.action, r.at, r.notice_slug, r.notice_version
		FROM records r LEFT JOIN purposes p ON p.code = r.purpose
		WHERE r.subject = ?1 AND r.seq = (SELECT max(seq) FROM records WHERE subject = ?1 AND purpose = r.purpose)
		ORDER BY r.purpose`, subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []State{}
	for rows.Next() {
		var st State
		var action, at string
		if err := rows.Scan(&st.Purpose, &st.Title, &action, &at, &st.NoticeSlug, &st.NoticeVersion); err != nil {
			return nil, err
		}
		st.Granted, st.Since = action == "grant", parse(at)
		out = append(out, st)
	}
	return out, rows.Err()
}

// RecordFilter narrows a listing. Zero values mean no filter.
type RecordFilter struct {
	Subject string
	Purpose string
	// Before returns records with a lower sequence number, for paging.
	Before int64
	Limit  int
}

// Records lists records, newest first.
func (s *Store) Records(ctx context.Context, f RecordFilter) ([]Record, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}
	where, args := []string{"1 = 1"}, []any{}
	if f.Subject != "" {
		where, args = append(where, "subject = ?"), append(args, f.Subject)
	}
	if f.Purpose != "" {
		where, args = append(where, "purpose = ?"), append(args, f.Purpose)
	}
	if f.Before > 0 {
		where, args = append(where, "seq < ?"), append(args, f.Before)
	}
	args = append(args, f.Limit)
	return s.scan(ctx, "WHERE "+strings.Join(where, " AND ")+" ORDER BY seq DESC LIMIT ?", args...)
}

// Each calls fn for every record, oldest first. Exports use it.
func (s *Store) Each(ctx context.Context, fn func(Record) error) error {
	var after int64
	for {
		page, err := s.scan(ctx, "WHERE seq > ? ORDER BY seq LIMIT 500", after)
		if err != nil {
			return err
		}
		for _, r := range page {
			if err := fn(r); err != nil {
				return err
			}
			after = r.Seq
		}
		if len(page) < 500 {
			return nil
		}
	}
}

func (s *Store) scan(ctx context.Context, tail string, args ...any) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT seq, at, subject, action, purpose, notice_slug, notice_version, lang, form_id, source, prev_hash, hash
		FROM records `+tail, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var r Record
		var at string
		if err := rows.Scan(&r.Seq, &at, &r.Subject, &r.Action, &r.Purpose, &r.NoticeSlug, &r.NoticeVersion, &r.Lang, &r.FormID,
			&r.Source, &r.PrevHash, &r.Hash); err != nil {
			return nil, err
		}
		r.At = parse(at)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Report is the result of checking the chain.
type Report struct {
	OK      bool   `json:"ok"`
	Records int64  `json:"records"`
	Head    string `json:"head"`
	// BrokenAt is the sequence number of the first bad record, when not OK.
	BrokenAt int64  `json:"broken_at,omitempty"`
	Problem  string `json:"problem,omitempty"`
}

// Verify walks the whole chain and recomputes every hash. Keep a copy of
// Head somewhere else from time to time: a chain rebuilt from scratch by
// someone with full access to the file would verify, but its head would
// not match your copy.
func (s *Store) Verify(ctx context.Context) (Report, error) {
	rep := Report{OK: true, Head: genesis}
	err := s.Each(ctx, func(r Record) error {
		if !rep.OK {
			return nil
		}
		bad := ""
		switch {
		case r.Seq != rep.Records+1:
			bad = "a record is missing before this one"
		case r.PrevHash != rep.Head:
			bad = "does not follow the record before it"
		case digest(r, stamp(r.At)) != r.Hash:
			bad = "contents do not match the hash"
		}
		if bad != "" {
			rep.OK, rep.BrokenAt, rep.Problem = false, r.Seq, bad
			return nil
		}
		rep.Records, rep.Head = r.Seq, r.Hash
		return nil
	})
	return rep, err
}
