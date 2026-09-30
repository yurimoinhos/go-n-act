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
	mux := NewMux()
	err := mux.Handle(Procedure{
		Service: "route.clients.index.v1",
		Method:  "Loader",
		RouteID: "/clients/",
		Fn: func(_ context.Context, in pageIn) (pageOut, error) {
			calls++
			actor = in.Actor
			return pageOut{Actor: "server"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var logged []string
	srv := &Server{
		Mux: mux,
		OnError: func(_ context.Context, procedure string, err error) {
			logged = append(logged, procedure+": "+err.Error())
		},
	}

	t.Run("nil collections become empty", func(t *testing.T) {
		calls = 0
		rec := post(t, srv, "/rpc/route.clients.index.v1/Loader", `{"page":1}`, nil)
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
		rec := post(t, srv, "/rpc/route.clients.index.v1/Loader", `{"page":1,"actor":"eve"}`, nil)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid request") {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
		if calls != 0 || actor != "" {
			t.Fatalf("handler ran calls=%d actor=%q", calls, actor)
		}
		if strings.Contains(rec.Body.String(), "actor") {
			t.Fatalf("response names the server field: %s", rec.Body.String())
		}
	})

	t.Run("unknown field", func(t *testing.T) {
		calls = 0
		rec := post(t, srv, "/rpc/route.clients.index.v1/Loader", `{"page":1,"nope":true}`, nil)
		if rec.Code != http.StatusBadRequest || calls != 0 {
			t.Fatalf("status %d calls %d body %s", rec.Code, calls, rec.Body.String())
		}
	})

	t.Run("unknown procedure hides the catalog", func(t *testing.T) {
		rec := post(t, srv, "/rpc/route.missing.v1/Nope", `{}`, nil)
		if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "Loader") {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("get is not a call", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/rpc/route.clients.index.v1/Loader", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status %d", rec.Code)
		}
	})
	_ = logged
}

func TestServerErrorsStayInside(t *testing.T) {
	mux := NewMux()
	_ = mux.Handle(Procedure{
		Service: "route.demo.leaf.v1",
		Method:  "Boom",
		Fn: func(context.Context, struct{}) (struct{}, error) {
			return struct{}{}, errors.New("password=secret")
		},
	})
	_ = mux.Handle(Procedure{
		Service: "route.demo.leaf.v1",
		Method:  "Missing",
		Fn: func(context.Context, struct{}) (struct{}, error) {
			return struct{}{}, WrapError(CodeNotFound, "client missing", errors.New("row 9"))
		},
	})
	_ = mux.Handle(Procedure{
		Service: "route.demo.leaf.v1",
		Method:  "Panic",
		Fn: func(context.Context, struct{}) (struct{}, error) {
			panic("secret boom")
		},
	})
	var logs []string
	srv := &Server{Mux: mux, OnError: func(_ context.Context, procedure string, err error) {
		logs = append(logs, procedure+" "+err.Error())
	}}

	rec := post(t, srv, "/rpc/route.demo.leaf.v1/Boom", `{}`, nil)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("boom status %d body %s", rec.Code, rec.Body.String())
	}

	rec = post(t, srv, "/rpc/route.demo.leaf.v1/Missing", `{}`, nil)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "client missing") || strings.Contains(rec.Body.String(), "row 9") {
		t.Fatalf("missing status %d body %s", rec.Code, rec.Body.String())
	}

	rec = post(t, srv, "/rpc/route.demo.leaf.v1/Panic", `{}`, nil)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("panic status %d body %s", rec.Code, rec.Body.String())
	}
	if len(logs) < 2 || !strings.Contains(strings.Join(logs, "\n"), "secret") {
		t.Fatalf("logs = %v", logs)
	}
}

func TestServerPrincipalAndRequired(t *testing.T) {
	mux := NewMux()
	_ = mux.Handle(Procedure{
		Service: "route.who.leaf.v1",
		Method:  "Who",
		Fn: func(ctx context.Context, _ struct{}) (struct {
			Subject string `json:"subject"`
		}, error) {
			p, _ := PrincipalFrom(ctx)
			return struct {
				Subject string `json:"subject"`
			}{Subject: p.Subject}, nil
		},
	})
	_ = mux.Handle(Procedure{
		Service: "route.who.leaf.v1",
		Method:  "Need",
		Fn: func(_ context.Context, in struct {
			ID string `json:"id" route:"required"`
		}) (struct {
			ID string `json:"id"`
		}, error) {
			return struct {
				ID string `json:"id"`
			}{ID: in.ID}, nil
		},
	})
	srv := &Server{
		Mux: mux,
		Authenticate: func(r *http.Request) (Principal, error) {
			if r.Header.Get("Authorization") == "" {
				return Principal{}, errors.New("no cookie")
			}
			return Principal{Subject: "ada"}, nil
		},
	}

	rec := post(t, srv, "/rpc/route.who.leaf.v1/Who", `{}`, nil)
	if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "cookie") {
		t.Fatalf("auth status %d body %s", rec.Code, rec.Body.String())
	}

	rec = post(t, srv, "/rpc/route.who.leaf.v1/Who", `{}`, map[string]string{"Authorization": "ok"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"subject":"ada"`) {
		t.Fatalf("who status %d body %s", rec.Code, rec.Body.String())
	}

	rec = post(t, srv, "/rpc/route.who.leaf.v1/Need", `{}`, map[string]string{"Authorization": "ok"})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "missing field id") {
		t.Fatalf("need status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestServerOrigin(t *testing.T) {
	mux := NewMux()
	_ = mux.Handle(Procedure{
		Service: "route.ping.leaf.v1",
		Method:  "Ping",
		Fn:      func(context.Context) (struct{}, error) { return struct{}{}, nil },
	})
	srv := &Server{Mux: mux, AllowedOrigins: []string{"https://app.example.com"}}

	t.Run("no origin", func(t *testing.T) {
		rec := post(t, srv, "/rpc/route.ping.leaf.v1/Ping", ``, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal(rec.Header())
		}
	})

	t.Run("same origin needs the route header", func(t *testing.T) {
		rec := post(t, srv, "/rpc/route.ping.leaf.v1/Ping", `{}`, map[string]string{
			"Origin": "http://app.test",
		})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("same origin with header", func(t *testing.T) {
		rec := call(t, srv, http.MethodPost, "/rpc/route.ping.leaf.v1/Ping", `{}`, map[string]string{
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
		rec := call(t, srv, http.MethodPost, "/rpc/route.ping.leaf.v1/Ping", `{}`, map[string]string{
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
		rec := call(t, srv, http.MethodPost, "/rpc/route.ping.leaf.v1/Ping", `{}`, map[string]string{
			"Origin":          "https://app.example.com",
			"X-Route-Request": "1",
			"Content-Type":    "application/json",
		}, "api.internal")
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("preflight", func(t *testing.T) {
		rec := call(t, srv, http.MethodOptions, "/rpc/route.ping.leaf.v1/Ping", ``, map[string]string{
			"Origin": "http://app.test",
		}, "app.test")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status %d", rec.Code)
		}
		if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "X-Route-Request") {
			t.Fatal(rec.Header())
		}
	})

	t.Run("body limit", func(t *testing.T) {
		srv.MaxBytes = 8
		rec := post(t, srv, "/rpc/route.ping.leaf.v1/Ping", `{"page":12345}`, nil)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unexpected body", func(t *testing.T) {
		srv.MaxBytes = 0
		rec := post(t, srv, "/rpc/route.ping.leaf.v1/Ping", `{"page":1}`, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
	})
}

func TestNoInputEmptyObject(t *testing.T) {
	mux := NewMux()
	_ = mux.Handle(Procedure{
		Service: "route.ping.leaf.v1",
		Method:  "Ping",
		Fn:      func(context.Context) error { return nil },
	})
	srv := &Server{Mux: mux}
	rec := post(t, srv, "/rpc/route.ping.leaf.v1/Ping", `{}`, nil)
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
