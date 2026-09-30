package route

import "context"

// Principal is the caller established by the server.
// Handlers must read it from the context. The request JSON cannot set it.
type Principal struct {
	Subject string
	Roles   []string
	Attrs   map[string]string
}

type principalKey struct{}

// WithPrincipal returns a context that carries p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the principal attached to ctx.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
