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
	"strings"
	"sync"
)

const (
	headerRouteRequest = "X-Route-Request"
	defaultMaxBytes    = 1 << 20
)

var (
	serviceName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)
	methodName  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	contextType = reflect.TypeOf((*context.Context)(nil)).Elem()
	errorType   = reflect.TypeOf((*error)(nil)).Elem()
)

// Procedure is one exported handler of one route.
type Procedure struct {
	Service string
	Method  string
	RouteID string
	Fn      any
}

type registered struct {
	proc Procedure
	fn   reflect.Value
	in   reflect.Type
	out  reflect.Type
}

// Mux is the allowlist of route procedures.
// Register every procedure before serving. A request can call only a key that was registered.
type Mux struct {
	mu    sync.RWMutex
	procs map[string]registered
}

// NewMux returns an empty allowlist.
func NewMux() *Mux {
	return &Mux{procs: make(map[string]registered)}
}

// Handle adds one procedure. Fn must be an exported route handler signature.
func (m *Mux) Handle(p Procedure) error {
	if !serviceName.MatchString(p.Service) || !methodName.MatchString(p.Method) {
		return Errorf(CodeInvalidArgument, "route: invalid procedure name %s/%s", p.Service, p.Method)
	}
	if p.Fn == nil {
		return Errorf(CodeInvalidArgument, "route: %s/%s missing function", p.Service, p.Method)
	}
	fn := reflect.ValueOf(p.Fn)
	in, out, err := checkHandler(fn.Type(), p.Service, p.Method)
	if err != nil {
		return err
	}
	key := p.Service + "/" + p.Method
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.procs[key]; ok {
		return Errorf(CodeAlreadyExists, "route: duplicate procedure %s", key)
	}
	m.procs[key] = registered{proc: p, fn: fn, in: in, out: out}
	return nil
}

func (m *Mux) lookup(service, method string) (registered, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h, ok := m.procs[service+"/"+method]
	return h, ok
}

// Known reports whether service/method is registered.
func (m *Mux) Known(service, method string) bool {
	_, ok := m.lookup(service, method)
	return ok
}

func checkHandler(t reflect.Type, service, method string) (in, out reflect.Type, err error) {
	if t.Kind() != reflect.Func {
		return nil, nil, Errorf(CodeInvalidArgument, "route: %s/%s handler must be a function", service, method)
	}
	nIn := t.NumIn()
	if t.IsVariadic() || nIn < 1 || nIn > 2 || !t.In(0).Implements(contextType) {
		return nil, nil, Errorf(CodeInvalidArgument, "route: %s/%s handler context signature", service, method)
	}
	if nIn == 2 {
		in = t.In(1)
		kind := in.Kind()
		if kind == reflect.Pointer {
			kind = in.Elem().Kind()
		}
		if kind != reflect.Struct {
			return nil, nil, Errorf(CodeInvalidArgument, "route: %s/%s input must be a struct", service, method)
		}
	}
	switch t.NumOut() {
	case 1:
		if !t.Out(0).Implements(errorType) {
			return nil, nil, Errorf(CodeInvalidArgument, "route: %s/%s result must be error", service, method)
		}
	case 2:
		if !t.Out(1).Implements(errorType) {
			return nil, nil, Errorf(CodeInvalidArgument, "route: %s/%s result must end with error", service, method)
		}
		out = t.Out(0)
	default:
		return nil, nil, Errorf(CodeInvalidArgument, "route: %s/%s result signature", service, method)
	}
	return in, out, nil
}

// Server is an HTTP/1.1 endpoint for a [Mux].
//
// Browser calls send Origin and X-Route-Request: 1. Origin must be this host
// or listed in AllowedOrigins. Calls without Origin are non-browser clients.
// Cross-origin responses are emitted only for an allowed Origin, never as *.
type Server struct {
	Mux *Mux

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

	if r.Method == http.MethodOptions && origin != "" {
		w.Header().Set("Access-Control-Allow-Methods", http.MethodPost)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, "+headerRouteRequest)
		w.Header().Set("Access-Control-Max-Age", "600")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	service, method, ok := splitProcedure(r.URL.Path)
	if !ok {
		writePublic(w, CodeNotFound, "not found")
		return
	}
	procName := service + "/" + method
	if s.Mux == nil || !s.Mux.Known(service, method) {
		writePublic(w, CodeNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		writePublic(w, CodeUnimplemented, "method not allowed")
		return
	}
	if origin != "" && r.Header.Get(headerRouteRequest) != "1" {
		s.fail(w, r, procName, WrapError(CodePermissionDenied, "forbidden", Errorf(CodePermissionDenied, "missing %s", headerRouteRequest)))
		return
	}
	if err := checkContentType(r); err != nil {
		s.fail(w, r, procName, err)
		return
	}

	maxBytes := s.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		s.fail(w, r, procName, WrapError(CodeInternal, "internal error", err))
		return
	}
	if int64(len(body)) > maxBytes {
		s.fail(w, r, procName, NewError(CodeResourceExhausted, "request too large"))
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

	reg, _ := s.Mux.lookup(service, method)
	out, err := callHandler(ctx, reg, body)
	if err != nil {
		s.fail(w, r, procName, err)
		return
	}
	writeBytes(w, http.StatusOK, out)
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
	code, msg := Public(err)
	writePublic(w, code, msg)
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

func splitProcedure(urlPath string) (service, method string, ok bool) {
	path := strings.TrimPrefix(urlPath, "/")
	rest, found := strings.CutPrefix(path, "rpc/")
	if !found {
		return "", "", false
	}
	service, method, found = strings.Cut(rest, "/")
	if !found || strings.Contains(method, "/") {
		return "", "", false
	}
	if !serviceName.MatchString(service) || !methodName.MatchString(method) {
		return "", "", false
	}
	return service, method, true
}

func callHandler(ctx context.Context, reg registered, body []byte) (b []byte, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = WrapError(CodeInternal, "internal error", Errorf(CodeInternal, "panic: %v", rec))
		}
	}()

	raw, payload, err := parseObject(body)
	if err != nil {
		return nil, err
	}
	args := []reflect.Value{reflect.ValueOf(ctx)}
	if reg.in != nil {
		holder := reflect.New(reg.in)
		if reg.in.Kind() == reflect.Pointer {
			holder = reflect.New(reg.in.Elem())
		}
		if err := decodeInput(payload, holder, raw); err != nil {
			return nil, err
		}
		arg := holder.Elem()
		if reg.in.Kind() == reflect.Pointer {
			arg = holder
		}
		args = append(args, arg)
	} else if len(raw) != 0 {
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

func writePublic(w http.ResponseWriter, code Code, message string) {
	body, err := json.Marshal(struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code.String(), Message: message})
	if err != nil {
		body = []byte(`{"code":"internal","message":"internal error"}`)
		writeBytes(w, http.StatusInternalServerError, body)
		return
	}
	writeBytes(w, code.StatusCode(), body)
}

func writeBytes(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
