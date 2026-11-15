// Package api is Sammati's HTTP interface: a public part that websites
// and people use to give and withdraw consent, and an admin part behind
// an API key.
package api

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vishrantkhanna/sammati/internal/store"
)

// Server answers API requests.
type Server struct {
	store  *store.Store
	log    *slog.Logger
	limit  *limiter
	login  *limiter
	origin string
	frames string
	proxy  bool
}

// Options configure the server.
type Options struct {
	// AllowOrigin is the Access-Control-Allow-Origin value for the public
	// API. "*" suits most sites, because the public API uses no cookies.
	AllowOrigin string
	// PublicPerMinute is how many public requests one address may make
	// each minute. Zero means 60.
	PublicPerMinute int
	// FrameAncestors lists the sites allowed to embed the form in a frame,
	// as a Content-Security-Policy source list. Empty means any site.
	FrameAncestors string
	// TrustProxy reads the client address from X-Forwarded-For. Switch it
	// on only behind your own reverse proxy.
	TrustProxy bool
}

// New builds the API handler.
func New(s *store.Store, log *slog.Logger, o Options) http.Handler {
	if o.AllowOrigin == "" {
		o.AllowOrigin = "*"
	}
	if o.PublicPerMinute <= 0 {
		o.PublicPerMinute = 60
	}
	if o.FrameAncestors == "" {
		o.FrameAncestors = "*"
	}
	a := &Server{store: s, log: log, origin: o.AllowOrigin, frames: o.FrameAncestors, proxy: o.TrustProxy,
		limit: newLimiter(o.PublicPerMinute, time.Minute, o.TrustProxy), login: newLimiter(10, time.Minute, o.TrustProxy)}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		write(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.Handle("GET /api/v1/forms/{id}", a.public(a.getForm))
	mux.Handle("POST /api/v1/consents", a.public(a.postConsent))
	mux.Handle("GET /api/v1/manage", a.public(a.getManage))
	mux.Handle("POST /api/v1/manage/withdraw", a.public(a.postManageWithdraw))
	mux.Handle("OPTIONS /api/v1/", a.public(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))

	mux.Handle("GET /api/v1/admin/purposes", a.admin(a.listPurposes))
	mux.Handle("POST /api/v1/admin/purposes", a.admin(a.createPurpose))
	mux.Handle("GET /api/v1/admin/notices", a.admin(a.listNotices))
	mux.Handle("POST /api/v1/admin/notices", a.admin(a.publishNotice))
	mux.Handle("GET /api/v1/admin/forms", a.admin(a.listForms))
	mux.Handle("POST /api/v1/admin/forms", a.admin(a.createForm))
	mux.Handle("POST /api/v1/admin/consents", a.admin(a.adminConsent))
	mux.Handle("POST /api/v1/admin/withdrawals", a.admin(a.adminWithdraw))
	mux.Handle("GET /api/v1/admin/subjects", a.admin(a.lookupSubject))
	mux.Handle("GET /api/v1/admin/records", a.admin(a.listRecords))
	mux.Handle("GET /api/v1/admin/records.csv", a.admin(a.exportCSV))
	mux.Handle("GET /api/v1/admin/verify", a.admin(a.verify))
	a.adminRoutes(mux)
	a.pages(mux)
	return secure(mux)
}

func secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// public adds CORS and the rate limit. The public API carries no cookies
// and no secrets, so any site may call it.
func (a *Server) public(fn http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", a.origin)
		h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type")
		h.Set("Access-Control-Max-Age", "600")
		if !a.limit.allow(r) {
			h.Set("Retry-After", "60")
			problem(w, http.StatusTooManyRequests, "Too many requests. Try again in a minute.")
			return
		}
		fn(w, r)
	})
}

// admin lets a request through with an API key or a signed-in session.
// A session request that changes something must carry the X-Sammati-Admin
// header, which another site cannot add, so a forged request from another
// page is refused.
func (a *Server) admin(fn http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok := false
		if key, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); found {
			var err error
			if ok, err = a.store.CheckAPIKey(r.Context(), strings.TrimSpace(key)); err != nil {
				a.fail(w, r, err)
				return
			}
		} else if c, err := r.Cookie(sessionCookie); err == nil {
			safe := r.Method == http.MethodGet || r.Method == http.MethodHead
			if safe || r.Header.Get("X-Sammati-Admin") == "1" {
				if ok, err = a.store.CheckSession(r.Context(), c.Value); err != nil {
					a.fail(w, r, err)
					return
				}
			}
		}
		if !ok {
			problem(w, http.StatusUnauthorized, "Sign in, or send a valid API key as: Authorization: Bearer <key>.")
			return
		}
		fn(w, r)
	})
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func problem(w http.ResponseWriter, status int, detail string) {
	write(w, status, map[string]any{"error": http.StatusText(status), "detail": detail})
}

// fail maps a store error to a response. Unexpected errors are logged and
// never shown.
func (a *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ie *store.InvalidError
	switch {
	case errors.As(err, &ie):
		write(w, http.StatusUnprocessableEntity, map[string]any{"error": "Unprocessable Content", "field": ie.Field, "detail": ie.Field + " " + ie.Message})
	case errors.Is(err, store.ErrDenied):
		problem(w, http.StatusUnauthorized, "The details are not right.")
	case errors.Is(err, store.ErrNotFound):
		problem(w, http.StatusNotFound, "Not found.")
	default:
		a.log.ErrorContext(r.Context(), "request failed", "path", r.URL.Path, "err", err)
		problem(w, http.StatusInternalServerError, "Something went wrong.")
	}
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		problem(w, http.StatusBadRequest, "Send a JSON object with the documented fields.")
		return false
	}
	return true
}

// ---- public ----

func (a *Server) getForm(w http.ResponseWriter, r *http.Request) {
	f, err := a.store.Form(r.Context(), r.PathValue("id"))
	if err == nil && !f.Active {
		err = store.ErrNotFound
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	n, err := a.store.Notice(r.Context(), f.NoticeSlug, r.URL.Query().Get("lang"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	org, _, err := a.store.Setting(r.Context(), "organisation")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, map[string]any{"form": f, "notice": n, "organisation": org, "has_logo": a.hasLogo(r)})
}

type consentIn struct {
	Form       string   `json:"form"`
	Identifier string   `json:"identifier"`
	Purposes   []string `json:"purposes"`
	Lang       string   `json:"lang"`
}

func (a *Server) grant(w http.ResponseWriter, r *http.Request, source string) {
	var in consentIn
	if !decode(w, r, &in) {
		return
	}
	recs, err := a.store.Grant(r.Context(), in.Form, in.Identifier, in.Purposes, in.Lang, source)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	tok, err := a.store.ManageToken(r.Context(), recs[0].Subject)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusCreated, map[string]any{"records": recs, "manage_token": tok})
}

func (a *Server) postConsent(w http.ResponseWriter, r *http.Request)  { a.grant(w, r, store.SourceForm) }
func (a *Server) adminConsent(w http.ResponseWriter, r *http.Request) { a.grant(w, r, store.SourceAPI) }

func (a *Server) getManage(w http.ResponseWriter, r *http.Request) {
	subject, err := a.store.SubjectFromToken(r.Context(), r.URL.Query().Get("token"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	states, err := a.store.States(r.Context(), subject)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	org, _, err := a.store.Setting(r.Context(), "organisation")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, map[string]any{"consents": states, "organisation": org, "has_logo": a.hasLogo(r)})
}

func (a *Server) postManageWithdraw(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string   `json:"token"`
		Purposes []string `json:"purposes"`
	}
	if !decode(w, r, &in) {
		return
	}
	subject, err := a.store.SubjectFromToken(r.Context(), in.Token)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	recs, err := a.store.Withdraw(r.Context(), subject, in.Purposes, store.SourceManage)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, map[string]any{"records": recs})
}

// ---- admin ----

func (a *Server) listPurposes(w http.ResponseWriter, r *http.Request) {
	v, err := a.store.Purposes(r.Context())
	a.list(w, r, v, err)
}

func (a *Server) createPurpose(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code, Title, Description string }
	if !decode(w, r, &in) {
		return
	}
	p, err := a.store.CreatePurpose(r.Context(), in.Code, in.Title, in.Description)
	a.created(w, r, p, err)
}

func (a *Server) listNotices(w http.ResponseWriter, r *http.Request) {
	v, err := a.store.Notices(r.Context())
	a.list(w, r, v, err)
}

func (a *Server) publishNotice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Slug  string             `json:"slug"`
		Texts []store.NoticeText `json:"texts"`
	}
	if !decode(w, r, &in) {
		return
	}
	v, err := a.store.PublishNotice(r.Context(), in.Slug, in.Texts)
	a.created(w, r, map[string]any{"slug": in.Slug, "version": v}, err)
}

func (a *Server) listForms(w http.ResponseWriter, r *http.Request) {
	v, err := a.store.Forms(r.Context())
	a.list(w, r, v, err)
}

func (a *Server) createForm(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID              string              `json:"id"`
		Title           string              `json:"title"`
		NoticeSlug      string              `json:"notice_slug"`
		IdentifierLabel string              `json:"identifier_label"`
		SuccessMessage  string              `json:"success_message"`
		Purposes        []store.FormPurpose `json:"purposes"`
	}
	if !decode(w, r, &in) {
		return
	}
	f, err := a.store.CreateForm(r.Context(), store.Form{ID: in.ID, Title: in.Title, NoticeSlug: in.NoticeSlug,
		IdentifierLabel: in.IdentifierLabel, SuccessMessage: in.SuccessMessage, Purposes: in.Purposes})
	a.created(w, r, f, err)
}

func (a *Server) adminWithdraw(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Identifier string   `json:"identifier"`
		Purposes   []string `json:"purposes"`
	}
	if !decode(w, r, &in) {
		return
	}
	subject, err := a.store.Subject(in.Identifier)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	recs, err := a.store.Withdraw(r.Context(), subject, in.Purposes, store.SourceAPI)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, map[string]any{"records": recs})
}

// lookupSubject answers "what has this person agreed to?" and returns the
// token for their manage page, to put in your own emails.
func (a *Server) lookupSubject(w http.ResponseWriter, r *http.Request) {
	subject, err := a.store.Subject(r.URL.Query().Get("identifier"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	states, err := a.store.States(r.Context(), subject)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	tok, err := a.store.ManageToken(r.Context(), subject)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, map[string]any{"subject": subject, "consents": states, "manage_token": tok})
}

func (a *Server) listRecords(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.RecordFilter{Purpose: q.Get("purpose")}
	f.Before, _ = strconv.ParseInt(q.Get("before"), 10, 64)
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	if id := q.Get("identifier"); id != "" {
		var err error
		if f.Subject, err = a.store.Subject(id); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	v, err := a.store.Records(r.Context(), f)
	a.list(w, r, v, err)
}

func (a *Server) exportCSV(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="consent-records.csv"`)
	c := csv.NewWriter(w)
	_ = c.Write([]string{"seq", "at", "subject", "action", "purpose", "notice_slug", "notice_version", "lang", "form_id", "source", "prev_hash", "hash"})
	err := a.store.Each(r.Context(), func(x store.Record) error {
		return c.Write([]string{strconv.FormatInt(x.Seq, 10), x.At.UTC().Format(time.RFC3339Nano), x.Subject, x.Action, x.Purpose,
			x.NoticeSlug, strconv.Itoa(x.NoticeVersion), x.Lang, x.FormID, x.Source, x.PrevHash, x.Hash})
	})
	c.Flush()
	if err != nil {
		a.log.ErrorContext(r.Context(), "export failed", "err", err)
	}
}

func (a *Server) verify(w http.ResponseWriter, r *http.Request) {
	rep, err := a.store.Verify(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, rep)
}

func (a *Server) list(w http.ResponseWriter, r *http.Request, v any, err error) {
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, map[string]any{"data": v})
}

func (a *Server) created(w http.ResponseWriter, r *http.Request, v any, err error) {
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusCreated, v)
}

// limiter is a fixed-window counter per client address, kept in memory.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	start  time.Time
	seen   map[string]int
	proxy  bool
}

func newLimiter(max int, window time.Duration, proxy bool) *limiter {
	return &limiter{max: max, window: window, seen: map[string]int{}, proxy: proxy}
}

func (l *limiter) allow(r *http.Request) bool {
	addr, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		addr = r.RemoteAddr
	}
	if l.proxy {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			parts := strings.Split(fwd, ",")
			addr = strings.TrimSpace(parts[len(parts)-1])
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if now := time.Now(); now.Sub(l.start) > l.window {
		l.start, l.seen = now, map[string]int{}
	}
	l.seen[addr]++
	return l.seen[addr] <= l.max
}
