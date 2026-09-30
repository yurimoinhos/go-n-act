// Package routegen builds a route tree from colocated .tsx and .go files
// and writes the typed HTTP client, the Go register file, and the route tree.
//
// The Go compiler rejects '$' in a file name, and a Go import path rejects '$'
// in a directory. A dynamic UI file keeps the TanStack spelling ($id.tsx).
// Its endpoint is the sibling param_id.go. A splat endpoint is splat.go.
package routegen
