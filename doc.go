// Package route serves colocated UI routes over HTTP/1.1.
//
// Each route is a file stem. The .go file is the only server endpoint for that
// stem, and the generated client is the only way the UI calls it. Handlers are
// registered explicitly; a request cannot reach a function that was not registered.
//
// A handler is an exported function with one of these signatures:
//
//	func(ctx context.Context, in T) (R, error)
//	func(ctx context.Context, in T) error
//	func(ctx context.Context) (R, error)
//	func(ctx context.Context) error
//
// T and R are structs. The principal comes from [Server.Authenticate], never
// from the JSON body. Fields tagged route:"server" are rejected when the
// client sends them. Fields tagged route:"required" must be present.
// Unknown JSON fields are rejected.
//
// Errors returned as [Error] keep their code and message. Any other error is
// logged through [Server.OnError] and answered as an internal error, without
// the underlying text.
package route
