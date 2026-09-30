// Package route serves colocated UI routes and REST actions over HTTP/1.1.
//
// Handlers are registered on a [Router] with an HTTP method and path:
//
//	r := route.NewRouter()
//	_ = route.POST(r, "/clients", Create)
//	_ = route.GET(r, "/clients/{id}", Get)
//
// File-based registration uses a //gnact:METHOD /path directive above an
// exported handler; routegen emits the matching route.POST/GET/... calls.
//
// A handler is an exported function with one of these signatures:
//
//	func(ctx context.Context, in T) (R, error)
//	func(ctx context.Context, in T) error
//	func(ctx context.Context) (R, error)
//	func(ctx context.Context) error
//
// T and R are structs. Path and query fields use path:"name" and
// query:"name" tags. JSON body fields use json tags. The principal comes
// from [Server.Authenticate], never from the request body. Fields tagged
// route:"server" are rejected when the client sends them. Fields tagged
// route:"required" must be present. Unknown JSON fields are rejected.
//
// Errors returned as [Error] are written as RFC 7807 problem details
// (application/problem+json). Any other error is logged through
// [Server.OnError] and answered as an internal problem, without the
// underlying text.
package route
