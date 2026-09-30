package route

import (
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"sync"
)

// App serves the API and the UI from one origin.
//
// Requests under APIPrefix go to API, usually a [*Server]. Every other request
// goes to the UI: proxied to DevURL (the Vite dev server, HMR websocket
// included) when it is set, otherwise served from Dist. A Dist path without an
// extension that is not a file gets index.html, so client routes survive a
// reload. A missing asset (a path with an extension) stays 404.
type App struct {
	// API handles every request under APIPrefix.
	API http.Handler

	// APIPrefix is the path prefix owned by API. Empty means "/api".
	APIPrefix string

	// DevURL is the UI dev server, for example http://localhost:5173.
	// When set, it wins over Dist.
	DevURL string

	// Dist is the built UI, for example os.DirFS("dist").
	Dist fs.FS

	once   sync.Once
	prefix string
	ui     http.Handler
}

// ServeHTTP implements [http.Handler].
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.once.Do(a.init)
	p := r.URL.Path
	if a.API != nil && (p == a.prefix || strings.HasPrefix(p, a.prefix+"/")) {
		a.API.ServeHTTP(w, r)
		return
	}
	a.ui.ServeHTTP(w, r)
}

func (a *App) init() {
	a.prefix = normalizePattern(a.APIPrefix)
	if a.prefix == "" || a.prefix == "/" {
		a.prefix = "/api"
	}
	switch {
	case a.DevURL != "":
		target, err := url.Parse(a.DevURL)
		if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
			a.ui = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writePublic(w, CodeInternal, "internal error")
			})
			return
		}
		a.ui = &httputil.ReverseProxy{
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(target)
				pr.SetXForwarded()
			},
		}
	case a.Dist != nil:
		a.ui = distHandler{fsys: a.Dist}
	default:
		a.ui = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writePublic(w, CodeNotFound, "not found")
		})
	}
}

type distHandler struct {
	fsys fs.FS
}

func (d distHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writePublic(w, CodeUnimplemented, "method not allowed")
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name != "" && name != "index.html" {
		if st, err := fs.Stat(d.fsys, name); err == nil && !st.IsDir() {
			http.ServeFileFS(w, r, d.fsys, name)
			return
		}
		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
	}
	// index.html answers "/" and every client route. It must not be cached,
	// or a deploy would keep serving stale asset hashes.
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, d.fsys, "index.html")
}
