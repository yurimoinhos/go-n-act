package routegen

// Refresh the embedded TypeScript runtime from ts/src (edit there, then generate).
//
//go:generate cp ../ts/src/client.ts ../ts/src/router.ts ../ts/src/match.ts ../ts/src/react.tsx ../ts/src/index.ts ./runtime/
