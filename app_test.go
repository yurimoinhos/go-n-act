package route

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func appAPI() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "api "+r.URL.Path)
	})
}

func appGet(t *testing.T, h http.Handler, method, path string) (int, string, http.Header) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec.Code, rec.Body.String(), rec.Header()
}

func TestAppServesDistWithClientRouteFallback(t *testing.T) {
	app := &App{
		API: appAPI(),
		Dist: fstest.MapFS{
			"index.html":    {Data: []byte("<html>spa</html>")},
			"assets/app.js": {Data: []byte("console.log(1)")},
		},
	}
	cases := []struct {
		method, path string
		code         int
		body         string
	}{
		{"GET", "/api/users", 200, "api /api/users"},
		{"POST", "/api", 200, "api /api"},
		{"GET", "/apiary", 200, "<html>spa</html>"},
		{"GET", "/", 200, "<html>spa</html>"},
		{"GET", "/users/01ARZ3NDEKTSV4RRFFQ69G5FAV", 200, "<html>spa</html>"},
		{"HEAD", "/roles/", 200, ""},
		{"GET", "/assets/app.js", 200, "console.log(1)"},
		{"GET", "/assets/missing.js", 404, ""},
		{"GET", "/assets", 200, "<html>spa</html>"},
		{"POST", "/users", 405, ""},
	}
	for _, c := range cases {
		code, body, _ := appGet(t, app, c.method, c.path)
		if code != c.code || (c.body != "" && body != c.body) {
			t.Errorf("%s %s = %d %q, want %d %q", c.method, c.path, code, body, c.code, c.body)
		}
	}
	_, _, h := appGet(t, app, "GET", "/users/")
	if h.Get("Cache-Control") != "no-cache" || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("index headers: %v", h)
	}
}

func TestAppProxiesToDevServer(t *testing.T) {
	var seen []string
	dev := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		_, _ = io.WriteString(w, "vite "+r.URL.Path)
	}))
	defer dev.Close()
	app := &App{
		API:       appAPI(),
		APIPrefix: "/rest/",
		DevURL:    dev.URL,
		Dist:      fstest.MapFS{"index.html": {Data: []byte("dist")}},
	}
	for path, want := range map[string]string{
		"/rest/users":      "api /rest/users",
		"/users/1?tab=a":   "vite /users/1",
		"/@vite/client":    "vite /@vite/client",
		"/src/main.tsx":    "vite /src/main.tsx",
		"/api/not-the-api": "vite /api/not-the-api",
	} {
		if _, body, _ := appGet(t, app, "GET", path); body != want {
			t.Errorf("GET %s = %q, want %q", path, body, want)
		}
	}
	if !strings.Contains(strings.Join(seen, ","), "GET /users/1?tab=a") {
		t.Fatalf("query not forwarded: %v", seen)
	}
}

func TestAppWithoutUI(t *testing.T) {
	app := &App{API: appAPI()}
	if code, _, _ := appGet(t, app, "GET", "/users"); code != http.StatusNotFound {
		t.Fatalf("no UI = %d", code)
	}
	bad := &App{API: appAPI(), DevURL: "localhost:5173"}
	if code, _, _ := appGet(t, bad, "GET", "/users"); code != http.StatusInternalServerError {
		t.Fatalf("bad DevURL = %d", code)
	}
	if code, body, _ := appGet(t, bad, "GET", "/api/x"); code != 200 || body != "api /api/x" {
		t.Fatalf("API must work with a bad DevURL: %d %q", code, body)
	}
}
