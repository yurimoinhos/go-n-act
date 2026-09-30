package route

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type pageIn struct {
	Page  int    `json:"page"`
	Actor string `json:"actor" route:"server"`
}

type pageOut struct {
	Items []string          `json:"items"`
	Extra map[string]string `json:"extra"`
	Actor string            `json:"actor"`
}

func TestServerRoundTripAndSecurity(t *testing.T) {
	var actor string
	var calls int
	r := NewRouter()
	if err := POST(r, "/clients", func(_ context.Context, in pageIn) (pageOut, error) {
		calls++
		actor = in.Actor
		return pageOut{Actor: "server"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	var logged []string
	srv := &Server{
		Router: r,
		OnError: func(_ context.Context, procedure string, err error) {
			logged = append(logged, procedure+": "+err.Error())
		},
	}

	t.Run("nil collections become empty", func(t *testing.T) {
		calls = 0
		rec := post(t, srv, "/clients", `{"page":1}`, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
		if calls != 1 {
			t.Fatalf("calls = %d", calls)
		}
		var got pageOut
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Items == nil || got.Extra == nil || got.Actor != "server" {
			t.Fatalf("body = %s", rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(rec.Header())
		}
	})

	t.Run("server field is rejected", func(t *testing.T) {
		calls = 0
		actor = ""
		rec := post(t, srv, "/clients", `{"page":1,"actor":"eve"}`, nil)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid request") {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
		if calls != 0 || actor != "" {
			t.Fatalf("handler ran calls=%d actor=%q", calls, actor)
		}
		if strings.Contains(rec.Body.String(), "actor") {
			t.Fatalf("response names the server field: %s", rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Fatalf("content-type %q", ct)
		}
	})

	t.Run("unknown field", func(t *testing.T) {
		calls = 0
		rec := post(t, srv, "/clients", `{"page":1,"nope":true}`, nil)
		if rec.Code != http.StatusBadRequest || calls != 0 {
			t.Fatalf("status %d calls %d body %s", rec.Code, calls, rec.Body.String())
		}
	})

	t.Run("unknown route hides the catalog", func(t *testing.T) {
		rec := post(t, srv, "/missing", `{}`, nil)
		if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "/clients") {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("get is not allowed on post route", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/clients", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status %d", rec.Code)
		}
	})
	_ = logged
}

func TestServerErrorsStayInside(t *testing.T) {
	r := NewRouter()
	_ = POST(r, "/demo/boom", func(context.Context, struct{}) (struct{}, error) {
		return struct{}{}, errors.New("password=secret")
	})
	_ = POST(r, "/demo/missing", func(context.Context, struct{}) (struct{}, error) {
		return struct{}{}, WrapError(CodeNotFound, "client missing", errors.New("row 9"))
	})
	_ = POST(r, "/demo/panic", func(context.Context, struct{}) (struct{}, error) {
		panic("secret boom")
	})
	var logs []string
	srv := &Server{Router: r, OnError: func(_ context.Context, procedure string, err error) {
		logs = append(logs, procedure+" "+err.Error())
	}}

	rec := post(t, srv, "/demo/boom", `{}`, nil)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("boom status %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"internal"`) || !strings.Contains(rec.Body.String(), `"type":"urn:gnact:error:internal"`) {
		t.Fatalf("boom problem %s", rec.Body.String())
	}

	rec = post(t, srv, "/demo/missing", `{}`, nil)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "client missing") || strings.Contains(rec.Body.String(), "row 9") {
		t.Fatalf("missing status %d body %s", rec.Code, rec.Body.String())
	}

	rec = post(t, srv, "/demo/panic", `{}`, nil)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("panic status %d body %s", rec.Code, rec.Body.String())
	}
	if len(logs) < 2 || !strings.Contains(strings.Join(logs, "\n"), "secret") {
		t.Fatalf("logs = %v", logs)
	}
}

func TestServerPrincipalAndRequired(t *testing.T) {
	r := NewRouter()
	_ = POST(r, "/who", func(ctx context.Context, _ struct{}) (struct {
		Subject string `json:"subject"`
	}, error) {
		p, _ := PrincipalFrom(ctx)
		return struct {
			Subject string `json:"subject"`
		}{Subject: p.Subject}, nil
	})
	_ = POST(r, "/need", func(_ context.Context, in struct {
		ID string `json:"id" route:"required"`
	}) (struct {
		ID string `json:"id"`
	}, error) {
		return struct {
			ID string `json:"id"`
		}{ID: in.ID}, nil
	})
	srv := &Server{
		Router: r,
		Authenticate: func(r *http.Request) (Principal, error) {
			if r.Header.Get("Authorization") == "" {
				return Principal{}, errors.New("no cookie")
			}
			return Principal{Subject: "ada"}, nil
		},
	}

	rec := post(t, srv, "/who", `{}`, nil)
	if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "cookie") {
		t.Fatalf("auth status %d body %s", rec.Code, rec.Body.String())
	}

	rec = post(t, srv, "/who", `{}`, map[string]string{"Authorization": "ok"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"subject":"ada"`) {
		t.Fatalf("who status %d body %s", rec.Code, rec.Body.String())
	}

	rec = post(t, srv, "/need", `{}`, map[string]string{"Authorization": "ok"})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "missing field id") {
		t.Fatalf("need status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestServerPathAndQuery(t *testing.T) {
	r := NewRouter()
	_ = GET(r, "/clients/{id}", func(_ context.Context, in struct {
		ID    string `path:"id"`
		Extra string `query:"extra"`
	}) (struct {
		ID    string `json:"id"`
		Extra string `json:"extra"`
	}, error) {
		return struct {
			ID    string `json:"id"`
			Extra string `json:"extra"`
		}{ID: in.ID, Extra: in.Extra}, nil
	})
	srv := &Server{Router: r}
	rec := call(t, srv, http.MethodGet, "/clients/42?extra=hi", ``, nil, "app.test")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"42"`) || !strings.Contains(rec.Body.String(), `"extra":"hi"`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestServerOrigin(t *testing.T) {
	r := NewRouter()
	_ = POST(r, "/ping", func(context.Context) (struct{}, error) { return struct{}{}, nil })
	srv := &Server{Router: r, AllowedOrigins: []string{"https://app.example.com"}}

	t.Run("no origin", func(t *testing.T) {
		rec := post(t, srv, "/ping", ``, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal(rec.Header())
		}
	})

	t.Run("same origin needs the route header", func(t *testing.T) {
		rec := post(t, srv, "/ping", `{}`, map[string]string{
			"Origin": "http://app.test",
		})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("same origin with header", func(t *testing.T) {
		rec := call(t, srv, http.MethodPost, "/ping", `{}`, map[string]string{
			"Origin":          "http://app.test",
			"X-Route-Request": "1",
			"Content-Type":    "application/json",
		}, "app.test")
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "http://app.test" {
			t.Fatal(rec.Header())
		}
	})

	t.Run("foreign origin", func(t *testing.T) {
		rec := call(t, srv, http.MethodPost, "/ping", `{}`, map[string]string{
			"Origin":          "https://evil.test",
			"X-Route-Request": "1",
		}, "app.test")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status %d", rec.Code)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("reflected origin")
		}
	})

	t.Run("allowlist", func(t *testing.T) {
		rec := call(t, srv, http.MethodPost, "/ping", `{}`, map[string]string{
			"Origin":          "https://app.example.com",
			"X-Route-Request": "1",
			"Content-Type":    "application/json",
		}, "api.internal")
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("preflight", func(t *testing.T) {
		rec := call(t, srv, http.MethodOptions, "/ping", ``, map[string]string{
			"Origin": "http://app.test",
		}, "app.test")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status %d", rec.Code)
		}
		allow := rec.Header().Get("Access-Control-Allow-Methods")
		if !strings.Contains(allow, "POST") || !strings.Contains(allow, "OPTIONS") {
			t.Fatal(rec.Header())
		}
		if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "X-Route-Request") {
			t.Fatal(rec.Header())
		}
	})

	t.Run("body limit", func(t *testing.T) {
		srv.MaxBytes = 8
		rec := post(t, srv, "/ping", `{"page":12345}`, nil)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unexpected body", func(t *testing.T) {
		srv.MaxBytes = 0
		rec := post(t, srv, "/ping", `{"page":1}`, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
	})
}

func TestNoInputEmptyObject(t *testing.T) {
	r := NewRouter()
	_ = POST(r, "/ping", func(context.Context) error { return nil })
	srv := &Server{Router: r}
	rec := post(t, srv, "/ping", `{}`, nil)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "{}" {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func post(t *testing.T, h http.Handler, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	if headers == nil {
		headers = map[string]string{}
	}
	if _, ok := headers["Content-Type"]; !ok && body != "" {
		headers["Content-Type"] = "application/json"
	}
	return call(t, h, http.MethodPost, path, body, headers, "app.test")
}

func call(t *testing.T, h http.Handler, method, path, body string, headers map[string]string, host string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Host = host
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
