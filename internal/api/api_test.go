package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vishrantkhanna/sammati/internal/store"
)

type client struct {
	t   *testing.T
	url string
	key string
}

func (c client) do(method, path string, body any, auth bool) (int, map[string]any) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, c.url+path, rd)
	if auth {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := map[string]any{"raw": string(raw)}
	_ = json.Unmarshal(raw, &out)
	return res.StatusCode, out
}

func setup(t *testing.T, o Options) client {
	t.Helper()
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	_, key, err := s.CreateAPIKey(t.Context(), "test")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(s, slog.New(slog.NewTextHandler(io.Discard, nil)), o))
	t.Cleanup(srv.Close)
	return client{t: t, url: srv.URL, key: key}
}

func TestEndToEnd(t *testing.T) {
	c := setup(t, Options{})
	if code, _ := c.do("GET", "/api/v1/admin/purposes", nil, false); code != 401 {
		t.Fatal("admin without a key:", code)
	}
	must := func(code, want int, body map[string]any) {
		t.Helper()
		if code != want {
			t.Fatalf("status %d, want %d: %v", code, want, body["raw"])
		}
	}
	code, b := c.do("POST", "/api/v1/admin/purposes", map[string]string{"code": "newsletter", "title": "Newsletter", "description": "Send you our monthly newsletter."}, true)
	must(code, 201, b)
	code, b = c.do("POST", "/api/v1/admin/notices", map[string]any{"slug": "news", "texts": []map[string]string{{"lang": "en", "title": "Notice", "body": "We email you once a month."}}}, true)
	must(code, 201, b)
	code, b = c.do("POST", "/api/v1/admin/forms", map[string]any{"id": "news", "title": "Newsletter", "notice_slug": "news", "purposes": []map[string]any{{"purpose": "newsletter", "required": true}}}, true)
	must(code, 201, b)

	code, b = c.do("GET", "/api/v1/forms/news?lang=hi", nil, false)
	must(code, 200, b)
	if b["notice"].(map[string]any)["lang"] != "en" {
		t.Fatal("no fallback to English")
	}
	code, b = c.do("POST", "/api/v1/consents", map[string]any{"form": "news", "identifier": "a@example.com", "purposes": []string{}}, false)
	must(code, 422, b)
	code, b = c.do("POST", "/api/v1/consents", map[string]any{"form": "news", "identifier": "a@example.com", "purposes": []string{"newsletter"}}, false)
	must(code, 201, b)
	token := b["manage_token"].(string)
	if strings.Contains(b["raw"].(string), "a@example.com") {
		t.Fatal("the identifier came back in the response")
	}

	code, b = c.do("GET", "/api/v1/manage?token=nope", nil, false)
	must(code, 404, b)
	code, b = c.do("GET", "/api/v1/manage?token="+token, nil, false)
	must(code, 200, b)
	if got := b["consents"].([]any)[0].(map[string]any)["granted"]; got != true {
		t.Fatal("not granted:", b["raw"])
	}
	code, b = c.do("POST", "/api/v1/manage/withdraw", map[string]any{"token": token, "purposes": []string{"newsletter"}}, false)
	must(code, 200, b)

	code, b = c.do("GET", "/api/v1/admin/subjects?identifier=A@example.com", nil, true)
	must(code, 200, b)
	if got := b["consents"].([]any)[0].(map[string]any)["granted"]; got != false {
		t.Fatal("still granted after withdrawal:", b["raw"])
	}
	code, b = c.do("GET", "/api/v1/admin/records?identifier=a@example.com", nil, true)
	must(code, 200, b)
	if len(b["data"].([]any)) != 2 {
		t.Fatal("records:", b["raw"])
	}
	code, b = c.do("GET", "/api/v1/admin/verify", nil, true)
	must(code, 200, b)
	if b["ok"] != true || b["records"].(float64) != 2 {
		t.Fatal("verify:", b["raw"])
	}
	code, b = c.do("GET", "/api/v1/admin/records.csv", nil, true)
	must(code, 200, b)
	if lines := strings.Count(strings.TrimSpace(b["raw"].(string)), "\n"); lines != 2 {
		t.Fatal("csv lines:", lines)
	}
	code, b = c.do("POST", "/api/v1/admin/purposes", map[string]string{"code": "x", "title": "X", "description": "Y", "extra": "z"}, true)
	must(code, 400, b)
}

func TestPublicRateLimit(t *testing.T) {
	c := setup(t, Options{PublicPerMinute: 3})
	last := 0
	for range 4 {
		last, _ = c.do("GET", "/api/v1/forms/none", nil, false)
	}
	if last != 429 {
		t.Fatal("fourth request:", last)
	}
	if code, _ := c.do("GET", "/api/v1/admin/purposes", nil, true); code != 200 {
		t.Fatal("admin was rate limited:", code)
	}
}

func (c client) session(method, path string, body any, cookie *http.Cookie, header bool) (int, map[string]any, []*http.Cookie) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, c.url+path, rd)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if header {
		req.Header.Set("X-Sammati-Admin", "1")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := map[string]any{"raw": string(raw)}
	_ = json.Unmarshal(raw, &out)
	return res.StatusCode, out, res.Cookies()
}

func TestSetupSignInAndForgeryGuard(t *testing.T) {
	c := setup(t, Options{})
	const pass = "correct horse battery"
	in := map[string]any{"organisation": "Acme", "email": "Owner@Example.com", "password": pass, "accept": false}
	if code, _, _ := c.session("POST", "/api/v1/setup", in, nil, false); code != 403 {
		t.Fatal("setup without the admin header:", code)
	}
	if code, _, _ := c.session("POST", "/api/v1/setup", in, nil, true); code != 422 {
		t.Fatal("setup without accepting responsibility:", code)
	}
	in["accept"] = true
	code, b, cookies := c.session("POST", "/api/v1/setup", in, nil, true)
	if code != 200 || len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("setup = %d %v %v", code, b["raw"], cookies)
	}
	if code, _, _ := c.session("POST", "/api/v1/setup", in, nil, true); code != 422 {
		t.Fatal("setup ran twice:", code)
	}
	ck := cookies[0]
	if code, b, _ := c.session("GET", "/api/v1/admin/me", nil, ck, false); code != 200 || b["admin"].(map[string]any)["email"] != "owner@example.com" {
		t.Fatal("me:", code, b["raw"])
	}
	// A changing request with the cookie but without the header is what a
	// forged request from another site looks like.
	body := map[string]string{"code": "p1", "title": "P", "description": "D"}
	if code, _, _ := c.session("POST", "/api/v1/admin/purposes", body, ck, false); code != 401 {
		t.Fatal("forged request accepted:", code)
	}
	if code, _, _ := c.session("POST", "/api/v1/admin/purposes", body, ck, true); code != 201 {
		t.Fatal("real request refused:", code)
	}
	if code, _, _ := c.session("POST", "/api/v1/login", map[string]string{"email": "owner@example.com", "password": "wrong wrong wrong"}, nil, true); code != 401 {
		t.Fatal("wrong password:", code)
	}
	if code, _, _ := c.session("POST", "/api/v1/logout", nil, ck, true); code != 200 {
		t.Fatal("logout:", code)
	}
	if code, _, _ := c.session("GET", "/api/v1/admin/me", nil, ck, false); code != 401 {
		t.Fatal("session lived after sign-out:", code)
	}
}

func TestPages(t *testing.T) {
	c := setup(t, Options{FrameAncestors: "https://shop.example"})
	for path, want := range map[string]string{"/admin/": "frame-ancestors 'none'", "/f/anything": "frame-ancestors https://shop.example", "/manage": "frame-ancestors https://shop.example"} {
		res, err := http.Get(c.url + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != 200 || !strings.Contains(res.Header.Get("Content-Security-Policy"), want) {
			t.Errorf("%s = %d %q", path, res.StatusCode, res.Header.Get("Content-Security-Policy"))
		}
	}
	for _, path := range []string{"/widget.js", "/static/app.css", "/static/admin.js", "/static/form.js", "/static/manage.js", "/static/i18n.js"} {
		res, err := http.Get(c.url + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != 200 {
			t.Errorf("%s = %d", path, res.StatusCode)
		}
	}
	if res, _ := http.Get(c.url + "/static/..%2f..%2fweb.go"); res.StatusCode != 404 {
		t.Error("path escape served:", res.StatusCode)
	}
}

func TestLogoUpload(t *testing.T) {
	c := setup(t, Options{})
	if res, _ := http.Get(c.url + "/logo"); res.StatusCode != 404 {
		t.Fatal("logo before upload:", res.StatusCode)
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	put := func(body []byte) int {
		req, _ := http.NewRequest("PUT", c.url+"/api/v1/admin/logo", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+c.key)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		return res.StatusCode
	}
	if code := put([]byte("<svg><script>alert(1)</script></svg>")); code != 422 {
		t.Fatal("svg accepted:", code)
	}
	if code := put(png); code != 200 {
		t.Fatal("png refused:", code)
	}
	res, err := http.Get(c.url + "/logo")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/png" || !strings.Contains(res.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatal("logo served as", res.StatusCode, res.Header)
	}
}
