package route

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
)

const (
	headerRouteRequest = "X-Route-Request"
	defaultMaxBytes    = 1 << 20
)

var (
	contextType = reflect.TypeOf((*context.Context)(nil)).Elem()
	errorType   = reflect.TypeOf((*error)(nil)).Elem()
	paramRE     = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)
)

// Route is one allowlisted HTTP handler.
type Route struct {
	Method  string
	Path    string
	Fn      any
	Name    string // optional stable name for codegen/OpenAPI
	RouteID string // optional UI route id when colocated
}

type registered struct {
	route   Route
	fn      reflect.Value
	in      reflect.Type
	out     reflect.Type
	pattern *pathPattern
}

type pathPattern struct {
	raw    string
	parts  []patternPart
	params []string
}

type patternPart struct {
	literal string
	param   string // non-empty when this segment is a {param}
}

// Router is the allowlist of REST routes.
// Register every route before serving. A request can call only a key that was registered.
type Router struct {
	mu     sync.RWMutex
	routes map[string]registered // method + "\x00" + path
	byPath map[string][]string   // path -> methods
}

// Mux is an alias for [Router] kept for gradual migration.
type Mux = Router

// NewRouter returns an empty allowlist.
func NewRouter() *Router {
	return &Router{
		routes: make(map[string]registered),
		byPath: make(map[string][]string),
	}
}

// NewMux returns an empty allowlist.
func NewMux() *Router { return NewRouter() }

// Handle adds one route. Fn must be an exported handler signature.
func (r *Router) Handle(rt Route) error {
	method := strings.ToUpper(strings.TrimSpace(rt.Method))
	if !allowedMethod(method) {
		return Errorf(CodeInvalidArgument, "route: invalid method %q", rt.Method)
	}
	path := normalizePattern(rt.Path)
	if path == "" || !strings.HasPrefix(path, "/") {
		return Errorf(CodeInvalidArgument, "route: invalid path %q", rt.Path)
	}
	if rt.Fn == nil {
		return Errorf(CodeInvalidArgument, "route: %s %s missing function", method, path)
	}
	fn := reflect.ValueOf(rt.Fn)
	in, out, err := checkHandler(fn.Type(), method, path)
	if err != nil {
		return err
	}
	pat, err := compilePattern(path)
	if err != nil {
		return err
	}
	key := routeKey(method, path)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.routes[key]; ok {
		return Errorf(CodeAlreadyExists, "route: duplicate %s %s", method, path)
	}
	r.routes[key] = registered{route: rt, fn: fn, in: in, out: out, pattern: pat}
	r.byPath[path] = appendUnique(r.byPath[path], method)
	return nil
}

// GET registers a GET handler.
func GET(r *Router, path string, fn any) error {
	return r.Handle(Route{Method: http.MethodGet, Path: path, Fn: fn})
}

// POST registers a POST handler.
func POST(r *Router, path string, fn any) error {
	return r.Handle(Route{Method: http.MethodPost, Path: path, Fn: fn})
}

// PATCH registers a PATCH handler.
func PATCH(r *Router, path string, fn any) error {
	return r.Handle(Route{Method: http.MethodPatch, Path: path, Fn: fn})
}

// PUT registers a PUT handler.
func PUT(r *Router, path string, fn any) error {
	return r.Handle(Route{Method: http.MethodPut, Path: path, Fn: fn})
}

// DELETE registers a DELETE handler.
func DELETE(r *Router, path string, fn any) error {
	return r.Handle(Route{Method: http.MethodDelete, Path: path, Fn: fn})
}

// Known reports whether method+path is registered.
func (r *Router) Known(method, path string) bool {
	_, ok := r.lookup(strings.ToUpper(method), normalizePattern(path))
	return ok
}

// Describe returns a stable snapshot of registered routes for codegen/OpenAPI.
func (r *Router) Describe() []Route {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Route, 0, len(r.routes))
	for _, reg := range r.routes {
		out = append(out, reg.route)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

func (r *Router) lookup(method, path string) (registered, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if reg, ok := r.routes[routeKey(method, path)]; ok {
		return reg, true
	}
	for key, reg := range r.routes {
		m, _, _ := strings.Cut(key, "\x00")
		if m != method {
			continue
		}
		if _, ok := reg.pattern.match(path); ok {
			return reg, true
		}
	}
	return registered{}, false
}

func (r *Router) match(method, path string) (registered, map[string]string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if reg, ok := r.routes[routeKey(method, path)]; ok {
		return reg, map[string]string{}, true
	}
	for key, reg := range r.routes {
		m, _, _ := strings.Cut(key, "\x00")
		if m != method {
			continue
		}
		params, ok := reg.pattern.match(path)
		if ok {
			return reg, params, true
		}
	}
	return registered{}, nil, false
}

func (r *Router) methodsForPath(path string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if methods, ok := r.byPath[path]; ok {
		out := append([]string(nil), methods...)
		sort.Strings(out)
		return out
	}
	var methods []string
	for _, reg := range r.routes {
		if _, ok := reg.pattern.match(path); ok {
			methods = appendUnique(methods, reg.route.Method)
		}
	}
	sort.Strings(methods)
	return methods
}

func allowedMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

func routeKey(method, path string) string {
	return method + "\x00" + path
}

func normalizePattern(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if path != "/" {
		path = strings.TrimSuffix(path, "/")
	}
	return path
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func compilePattern(path string) (*pathPattern, error) {
	path = normalizePattern(path)
	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if path == "/" {
		segs = nil
	}
	pat := &pathPattern{raw: path}
	seen := map[string]bool{}
	for _, seg := range segs {
		if seg == "" {
			return nil, Errorf(CodeInvalidArgument, "route: invalid path %q", path)
		}
		if m := paramRE.FindStringSubmatch(seg); m != nil {
			if m[0] != seg {
				return nil, Errorf(CodeInvalidArgument, "route: invalid path segment %q", seg)
			}
			name := m[1]
			if seen[name] {
				return nil, Errorf(CodeInvalidArgument, "route: duplicate path param %q", name)
			}
			seen[name] = true
			pat.parts = append(pat.parts, patternPart{param: name})
			pat.params = append(pat.params, name)
			continue
		}
		if strings.ContainsAny(seg, "{}") {
			return nil, Errorf(CodeInvalidArgument, "route: invalid path segment %q", seg)
		}
		pat.parts = append(pat.parts, patternPart{literal: seg})
	}
	return pat, nil
}

func (p *pathPattern) match(path string) (map[string]string, bool) {
	path = normalizePattern(path)
	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if path == "/" {
		segs = nil
	}
	if len(segs) != len(p.parts) {
		return nil, false
	}
	params := map[string]string{}
	for i, part := range p.parts {
		if part.param != "" {
			if segs[i] == "" {
				return nil, false
			}
			params[part.param] = segs[i]
			continue
		}
		if segs[i] != part.literal {
			return nil, false
		}
	}
	return params, true
}

func checkHandler(t reflect.Type, method, path string) (in, out reflect.Type, err error) {
	if t.Kind() != reflect.Func {
		return nil, nil, Errorf(CodeInvalidArgument, "route: %s %s handler must be a function", method, path)
	}
	nIn := t.NumIn()
	if t.IsVariadic() || nIn < 1 || nIn > 2 || !t.In(0).Implements(contextType) {
		return nil, nil, Errorf(CodeInvalidArgument, "route: %s %s handler context signature", method, path)
	}
	if nIn == 2 {
		in = t.In(1)
		kind := in.Kind()
		if kind == reflect.Pointer {
			kind = in.Elem().Kind()
		}
		if kind != reflect.Struct {
			return nil, nil, Errorf(CodeInvalidArgument, "route: %s %s input must be a struct", method, path)
		}
	}
	switch t.NumOut() {
	case 1:
		if !t.Out(0).Implements(errorType) {
			return nil, nil, Errorf(CodeInvalidArgument, "route: %s %s result must be error", method, path)
		}
	case 2:
		if !t.Out(1).Implements(errorType) {
			return nil, nil, Errorf(CodeInvalidArgument, "route: %s %s result must end with error", method, path)
		}
		out = t.Out(0)
	default:
		return nil, nil, Errorf(CodeInvalidArgument, "route: %s %s result signature", method, path)
	}
	return in, out, nil
}

// Server is an HTTP/1.1 endpoint for a [Router].
//
// Browser calls send Origin and X-Route-Request: 1. Origin must be this host
// or listed in AllowedOrigins. Calls without Origin are non-browser clients.
// Cross-origin responses are emitted only for an allowed Origin, never as *.
type Server struct {
	Router *Router

	// Mux is an alias for Router. If Router is nil, Mux is used.
	Mux *Router

	// MaxBytes limits the request body. Zero means 1 MiB.
	MaxBytes int64

	// AllowedOrigins lists extra browser origins, scheme and host included.
	// The request's own origin is always allowed.
	AllowedOrigins []string

	// TrustForwardedProto uses X-Forwarded-Proto when comparing same-origin.
	// Leave it false unless a trusted proxy overwrites that header.
	TrustForwardedProto bool

	// Authenticate maps the request to a principal before the handler runs.
	// A non-nil error rejects the call. The handler receives the principal
	// only through the context.
	Authenticate func(*http.Request) (Principal, error)

	// OnError observes failures that were not returned as a public [Error],
	// plus the cause wrapped inside a public error. It must not write the response.
	OnError func(ctx context.Context, procedure string, err error)
}

func (s *Server) router() *Router {
	if s.Router != nil {
		return s.Router
	}
	return s.Mux
}

// ServeHTTP implements [http.Handler].
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")

	origin := r.Header.Get("Origin")
	if origin != "" {
		w.Header().Set("Vary", "Origin")
		if !s.originAllowed(r, origin) {
			s.fail(w, r, "", WrapError(CodePermissionDenied, "forbidden", Errorf(CodePermissionDenied, "origin %s", origin)))
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}

	path := normalizePattern(r.URL.Path)
	rt := s.router()
	methods := []string{}
	if rt != nil {
		methods = rt.methodsForPath(path)
	}

	if r.Method == http.MethodOptions && origin != "" {
		allow := "OPTIONS"
		if len(methods) > 0 {
			allow = strings.Join(append([]string{"OPTIONS"}, methods...), ", ")
		}
		w.Header().Set("Access-Control-Allow-Methods", allow)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, "+headerRouteRequest)
		w.Header().Set("Access-Control-Max-Age", "600")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	procName := r.Method + " " + path
	if rt == nil || len(methods) == 0 {
		writeProblem(w, PublicProblem(NewError(CodeNotFound, "not found")))
		return
	}
	reg, params, ok := rt.match(r.Method, path)
	if !ok {
		writeProblem(w, PublicProblem(NewError(CodeUnimplemented, "method not allowed")))
		return
	}
	if origin != "" && r.Header.Get(headerRouteRequest) != "1" {
		s.fail(w, r, procName, WrapError(CodePermissionDenied, "forbidden", Errorf(CodePermissionDenied, "missing %s", headerRouteRequest)))
		return
	}

	hasBody := methodHasBody(r.Method)
	if hasBody {
		if err := checkContentType(r); err != nil {
			s.fail(w, r, procName, err)
			return
		}
	}

	maxBytes := s.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	var body []byte
	if hasBody || r.ContentLength > 0 || r.Body != nil {
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
		if err != nil {
			s.fail(w, r, procName, WrapError(CodeInternal, "internal error", err))
			return
		}
		if int64(len(body)) > maxBytes {
			s.fail(w, r, procName, NewError(CodeResourceExhausted, "request too large"))
			return
		}
	}
	if !hasBody && len(bytesTrimSpace(body)) > 0 {
		s.fail(w, r, procName, NewError(CodeInvalidArgument, "invalid request"))
		return
	}

	ctx := r.Context()
	if s.Authenticate != nil {
		principal, err := s.Authenticate(r)
		if err != nil {
			var pub *Error
			if !errors.As(err, &pub) {
				err = WrapError(CodeUnauthenticated, "unauthenticated", err)
			}
			s.fail(w, r, procName, err)
			return
		}
		ctx = WithPrincipal(ctx, principal)
	}

	out, err := callHandler(ctx, reg, body, params, r.URL.Query(), hasBody)
	if err != nil {
		s.fail(w, r, procName, err)
		return
	}
	writeBytes(w, http.StatusOK, out)
}

func methodHasBody(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return false
	}
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func (s *Server) originAllowed(r *http.Request, origin string) bool {
	if origin == "" || origin == "null" {
		return false
	}
	if origin == s.ownOrigin(r) {
		return true
	}
	for _, allowed := range s.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}

func (s *Server) ownOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if s.TrustForwardedProto {
		switch r.Header.Get("X-Forwarded-Proto") {
		case "http", "https":
			scheme = r.Header.Get("X-Forwarded-Proto")
		}
	}
	return scheme + "://" + r.Host
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, procedure string, err error) {
	if s.OnError != nil {
		var pub *Error
		if !errorsAsPublic(err, &pub) || pub.Unwrap() != nil || pub.Code == CodeInternal {
			s.OnError(r.Context(), procedure, err)
		}
	}
	writeProblem(w, PublicProblem(err))
}

func errorsAsPublic(err error, target **Error) bool {
	return errors.As(err, target)
}

func checkContentType(r *http.Request) error {
	h := r.Header.Get("Content-Type")
	if h == "" {
		return nil
	}
	media, _, err := mime.ParseMediaType(h)
	if err != nil || media != "application/json" {
		return NewError(CodeInvalidArgument, "invalid request")
	}
	return nil
}

func callHandler(ctx context.Context, reg registered, body []byte, pathParams map[string]string, query map[string][]string, hasBody bool) (b []byte, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = WrapError(CodeInternal, "internal error", Errorf(CodeInternal, "panic: %v", rec))
		}
	}()

	args := []reflect.Value{reflect.ValueOf(ctx)}
	if reg.in != nil {
		holder := reflect.New(reg.in)
		if reg.in.Kind() == reflect.Pointer {
			holder = reflect.New(reg.in.Elem())
		}
		target := holder
		if reg.in.Kind() != reflect.Pointer {
			// holder is already *T
		}
		if err := decodeRequest(holder, body, pathParams, query, hasBody); err != nil {
			return nil, err
		}
		arg := holder.Elem()
		if reg.in.Kind() == reflect.Pointer {
			arg = holder
		}
		args = append(args, arg)
		_ = target
	} else if hasBody {
		raw, _, err := parseObject(body)
		if err != nil {
			return nil, err
		}
		if len(raw) != 0 {
			return nil, NewError(CodeInvalidArgument, "invalid request")
		}
	} else if len(bytesTrimSpace(body)) > 0 {
		return nil, NewError(CodeInvalidArgument, "invalid request")
	}

	outs := reg.fn.Call(args)
	errVal := outs[len(outs)-1]
	if !errVal.IsNil() {
		return nil, errVal.Interface().(error)
	}
	if reg.out == nil {
		return []byte("{}"), nil
	}
	val := outs[0]
	cp := reflect.New(val.Type()).Elem()
	cp.Set(val)
	normalizeEmpty(cp)
	encoded, err := json.Marshal(cp.Interface())
	if err != nil {
		return nil, WrapError(CodeInternal, "internal error", err)
	}
	return encoded, nil
}

func writeProblem(w http.ResponseWriter, p Problem) {
	type wire struct {
		Type     string `json:"type"`
		Title    string `json:"title"`
		Status   int    `json:"status"`
		Detail   string `json:"detail"`
		Instance string `json:"instance,omitempty"`
		Code     string `json:"code"`
	}
	body, err := json.Marshal(wire{
		Type:     p.Type,
		Title:    p.Title,
		Status:   p.Status,
		Detail:   p.Detail,
		Instance: p.Instance,
		Code:     p.Code,
	})
	if err != nil {
		body = []byte(`{"type":"urn:gnact:error:internal","title":"Internal Server Error","status":500,"detail":"internal error","code":"internal"}`)
		writeProblemBytes(w, http.StatusInternalServerError, body)
		return
	}
	writeProblemBytes(w, p.Status, body)
}

func writeProblemBytes(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writePublic(w http.ResponseWriter, code Code, message string) {
	writeProblem(w, PublicProblem(NewError(code, message)))
}

func writeBytes(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
