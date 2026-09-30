// Package routegen builds a route tree from colocated .tsx and .go files
// and writes the typed HTTP client, the Go register file, and the route tree.
//
// The Go compiler rejects '$' in a file name, and a Go import path rejects '$'
// in a directory. A dynamic UI file keeps the TanStack spelling ($id.tsx).
// Its endpoint is the sibling param_id.go. A splat endpoint is splat.go.
// The go tool ignores a file whose name starts with '_', so a pathless UI file
// _auth.tsx pairs with pathless_auth.go in the same directory. A directory
// named _auth is a valid import path; only the file name changes.
//
// A static segment literally named param_<name>, splat, or pathless_<name>
// is rewritten onto the dynamic or pathless spelling.
package routegen
