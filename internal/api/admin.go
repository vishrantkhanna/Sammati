package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/vishrantkhanna/sammati/internal/store"
	"github.com/vishrantkhanna/sammati/internal/web"
)

const sessionCookie = "sammati_session"

// Responsibility is shown at setup, in the footer and in the README.
const Responsibility = "Sammati is self-hosted software. You install it, run it and secure it, and you are solely responsible for " +
	"the data it holds and for meeting your legal obligations. It is not a Consent Manager registered with the Data " +
	"Protection Board of India, and using it does not make you compliant on its own. It is provided under the MIT " +
	"licence, without warranty of any kind."

func (a *Server) adminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/setup", a.getSetup)
	mux.Handle("POST /api/v1/setup", a.slow(a.postSetup))
	mux.Handle("POST /api/v1/login", a.slow(a.postLogin))
	mux.HandleFunc("POST /api/v1/logout", a.postLogout)

	mux.Handle("GET /api/v1/admin/me", a.admin(a.me))
	mux.Handle("GET /api/v1/admin/stats", a.admin(a.stats))
	mux.Handle("PUT /api/v1/admin/settings", a.admin(a.putSettings))
	mux.Handle("POST /api/v1/admin/password", a.admin(a.postPassword))
	mux.Handle("POST /api/v1/admin/two-step/begin", a.admin(a.twoStepBegin))
	mux.Handle("POST /api/v1/admin/two-step/confirm", a.admin(a.twoStepConfirm))
	mux.Handle("POST /api/v1/admin/two-step/disable", a.admin(a.twoStepDisable))
	mux.Handle("POST /api/v1/admin/purposes/{code}/archive", a.admin(a.archivePurpose))
	mux.Handle("POST /api/v1/admin/forms/{id}/active", a.admin(a.formActive))
	mux.Handle("GET /api/v1/admin/webhooks", a.admin(a.listWebhooks))
	mux.Handle("POST /api/v1/admin/webhooks", a.admin(a.createWebhook))
	mux.Handle("DELETE /api/v1/admin/webhooks/{id}", a.admin(a.deleteWebhook))
	mux.Handle("GET /api/v1/admin/deliveries", a.admin(a.listDeliveries))
	mux.Handle("PUT /api/v1/admin/logo", a.admin(a.putLogo))
	mux.Handle("DELETE /api/v1/admin/logo", a.admin(a.deleteLogo))
	mux.Handle("GET /api/v1/admin/keys", a.admin(a.listKeys))
	mux.Handle("POST /api/v1/admin/keys", a.admin(a.createKey))
	mux.Handle("DELETE /api/v1/admin/keys/{id}", a.admin(a.revokeKey))
}

// slow applies the strict limit used for sign-in and setup.
func (a *Server) slow(fn http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Sammati-Admin") != "1" {
			problem(w, http.StatusForbidden, "This request must come from the Sammati admin page.")
			return
		}
		if !a.login.allow(r) {
			w.Header().Set("Retry-After", "60")
			problem(w, http.StatusTooManyRequests, "Too many attempts. Try again in a minute.")
			return
		}
		fn(w, r)
	})
}

func (a *Server) getSetup(w http.ResponseWriter, r *http.Request) {
	done, err := a.store.IsSetup(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, map[string]any{"setup_done": done, "responsibility": Responsibility})
}

func (a *Server) postSetup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Organisation string `json:"organisation"`
		Email        string `json:"email"`
		Password     string `json:"password"`
		Accept       bool   `json:"accept"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := a.store.Setup(r.Context(), in.Organisation, in.Email, in.Password, in.Accept); err != nil {
		a.fail(w, r, err)
		return
	}
	a.signIn(w, r, in.Email, in.Password, "")
}

func (a *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	a.signIn(w, r, in.Email, in.Password, in.Code)
}

func (a *Server) secureRequest(r *http.Request) bool {
	return r.TLS != nil || a.proxy && r.Header.Get("X-Forwarded-Proto") == "https"
}

func (a *Server) signIn(w http.ResponseWriter, r *http.Request, email, password, code string) {
	token, err := a.store.SignIn(r.Context(), email, password, code)
	if errors.Is(err, store.ErrCodeNeeded) {
		write(w, http.StatusUnauthorized, map[string]any{"error": "Unauthorized", "code_needed": true, "detail": "Enter the code from your authenticator app."})
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: a.secureRequest(r), MaxAge: int(store.SessionTTL.Seconds())})
	write(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Server) postLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && r.Header.Get("X-Sammati-Admin") == "1" {
		if err := a.store.SignOut(r.Context(), c.Value); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	write(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Server) me(w http.ResponseWriter, r *http.Request) {
	v, err := a.store.Admin(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, map[string]any{"admin": v, "responsibility": Responsibility, "version": web.Version, "has_logo": a.hasLogo(r)})
}

func (a *Server) stats(w http.ResponseWriter, r *http.Request) {
	v, err := a.store.Stats(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, v)
}

func (a *Server) done(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Organisation string `json:"organisation"`
	}
	if !decode(w, r, &in) {
		return
	}
	if n := len([]rune(in.Organisation)); n < 1 || n > 120 {
		a.fail(w, r, &store.InvalidError{Field: "organisation", Message: "must be 1 to 120 characters"})
		return
	}
	a.done(w, r, a.store.SetSetting(r.Context(), "organisation", in.Organisation))
}

func (a *Server) postPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decode(w, r, &in) {
		return
	}
	a.done(w, r, a.store.ChangePassword(r.Context(), in.Current, in.New))
}

func (a *Server) twoStepBegin(w http.ResponseWriter, r *http.Request) {
	secret, uri, err := a.store.BeginTwoStep(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, map[string]any{"secret": secret, "uri": uri})
}

func (a *Server) twoStepConfirm(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	a.done(w, r, a.store.ConfirmTwoStep(r.Context(), in.Code))
}

func (a *Server) twoStepDisable(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	a.done(w, r, a.store.DisableTwoStep(r.Context(), in.Password))
}

// putLogo takes the image itself as the request body.
func (a *Server) putLogo(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, store.MaxLogoBytes+1))
	if err != nil {
		a.fail(w, r, &store.InvalidError{Field: "logo", Message: "must be an image of up to 256 KB"})
		return
	}
	a.done(w, r, a.store.SetLogo(r.Context(), data))
}

func (a *Server) deleteLogo(w http.ResponseWriter, r *http.Request) {
	a.done(w, r, a.store.RemoveLogo(r.Context()))
}

// getLogo serves the organisation's logo to anyone, because it appears on
// the public consent form.
func (a *Server) getLogo(w http.ResponseWriter, r *http.Request) {
	data, kind, err := a.store.Logo(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", kind)
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
	h.Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

func (a *Server) hasLogo(r *http.Request) bool {
	_, ok, err := a.store.Setting(r.Context(), "logo_type")
	return err == nil && ok
}

func (a *Server) archivePurpose(w http.ResponseWriter, r *http.Request) {
	a.done(w, r, a.store.ArchivePurpose(r.Context(), r.PathValue("code")))
}

func (a *Server) formActive(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Active bool `json:"active"`
	}
	if !decode(w, r, &in) {
		return
	}
	a.done(w, r, a.store.SetFormActive(r.Context(), r.PathValue("id"), in.Active))
}

func (a *Server) listWebhooks(w http.ResponseWriter, r *http.Request) {
	v, err := a.store.Webhooks(r.Context())
	a.list(w, r, v, err)
}

func (a *Server) createWebhook(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL string `json:"url"`
	}
	if !decode(w, r, &in) {
		return
	}
	v, err := a.store.CreateWebhook(r.Context(), in.URL)
	a.created(w, r, v, err)
}

func (a *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	a.done(w, r, a.store.DeleteWebhook(r.Context(), r.PathValue("id")))
}

func (a *Server) listDeliveries(w http.ResponseWriter, r *http.Request) {
	v, err := a.store.RecentDeliveries(r.Context(), 50)
	a.list(w, r, v, err)
}

func (a *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	v, err := a.store.APIKeys(r.Context())
	a.list(w, r, v, err)
}

func (a *Server) createKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	id, key, err := a.store.CreateAPIKey(r.Context(), in.Name)
	a.created(w, r, map[string]any{"id": id, "key": key}, err)
}

func (a *Server) revokeKey(w http.ResponseWriter, r *http.Request) {
	a.done(w, r, a.store.RevokeAPIKey(r.Context(), r.PathValue("id")))
}

// pages serves the admin screens, the hosted form, the manage page and
// the widget script. Admin pages may never be framed; the form and manage
// pages may be, so they can be embedded on your site.
func (a *Server) pages(mux *http.ServeMux) {
	const base = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'self'; frame-ancestors "
	page := func(name, ancestors string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Security-Policy", base+ancestors)
			if ancestors == "'none'" {
				w.Header().Set("X-Frame-Options", "DENY")
			}
			web.Serve(w, name)
		}
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/admin/", http.StatusFound) })
	mux.HandleFunc("GET /admin/{$}", page("admin.html", "'none'"))
	mux.HandleFunc("GET /f/{id}", page("form.html", a.frames))
	mux.HandleFunc("GET /manage", page("manage.html", a.frames))
	mux.HandleFunc("GET /logo", a.getLogo)
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, _ *http.Request) { web.Serve(w, "mark.svg") })
	mux.HandleFunc("GET /widget.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
		web.Serve(w, "widget.js")
	})
	mux.HandleFunc("GET /static/{name}", func(w http.ResponseWriter, r *http.Request) { web.Serve(w, r.PathValue("name")) })
}
