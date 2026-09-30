# route / gnact

Library for colocated routes. Each route is a `.tsx` file and, when the route has a server, a `.go` file in the same directory. The `.go` file is the only endpoint for that route. The UI calls that endpoint through the generated client, over HTTP/1.1 with JSON.

The router follows the file-based route model: full path, layout, index, parameter, splat, pathless segment, loader, and `beforeLoad`. It does not depend on TanStack Router. React is an optional dependency of the UI entry.

## Getting started

```bash
go get github.com/yurimoinhos/go-n-act
go install github.com/yurimoinhos/go-n-act/cmd/gnact@latest
```

Every push to `master` gets a semver tag. `feat` bumps the minor, everything else bumps the patch, and the first tag is `v0.1.0`. The Action publishes the tag and asks `proxy.golang.org` to index the module, which then appears at `pkg.go.dev/github.com/yurimoinhos/go-n-act`. A major `v2` is not created automatically: the module path would have to change.

In the project directory:

```bash
gnact routegen -dir routes
```

In the server `main`:

```go
import gnact "github.com/yurimoinhos/go-n-act"

r := gnact.NewRouter()
if err := routes.RegisterAll(r); err != nil {
    log.Fatal(err)
}
log.Fatal(http.ListenAndServe(":8080", &gnact.Server{
    Router: r,
    Authenticate: func(req *http.Request) (gnact.Principal, error) {
        return gnact.Principal{Subject: "ada"}, nil
    },
}))
```

To serve the UI from the same origin, wrap the server in `App`. Requests under `APIPrefix` (default `/api`) go to the API, and every other request goes to the UI. When `DevURL` is set, the UI is proxied to the dev server, HMR websocket included. Otherwise it is served from `Dist`. A path with no extension that is not a file gets `index.html`, so client routes survive a reload. A missing asset stays `404`.

```go
log.Fatal(http.ListenAndServe(":8080", &gnact.App{
    API:    &gnact.Server{Router: r},
    DevURL: os.Getenv("UI_DEV_URL"), // e.g. http://localhost:5173 in development
    Dist:   os.DirFS("dist"),        // vite build output
}))
```

In the UI, import the generated tree and the `@aggitech/route` entry. The generated client imports `@aggitech/route/client`, so a bundle that only calls the server does not pull in React.

## Route files

The route key is the file path without the extension.

| File | URL | Service |
| --- | --- | --- |
| `__root.tsx` | root layout | `route.root.v1` |
| `index.tsx` | `/` | `route.index.v1` |
| `about.tsx` | `/about` | `route.about.v1` |
| `clients/route.tsx` | `/clients` | `route.clients.layout.v1` |
| `clients/index.tsx` | `/clients/` | `route.clients.index.v1` |
| `clients/$id.tsx` | `/clients/$id` | `route.clients.param_id.v1` |
| `clients/$id/route.tsx` | `/clients/$id` (layout) | `route.clients.param_id.layout.v1` |
| `api/trpc/$.tsx` | `/api/trpc/$` | `route.api.trpc.splat.v1` |
| `posts.$postId.tsx` | `/posts/$postId` | `route.posts.param_postId.v1` |
| `_auth.tsx` | pathless segment `/_auth` | `route.pathless_auth.v1` |
| `_auth.login.tsx` | `/login` under `/_auth` | `route.login.v1` |

`createFileRoute` takes the full URL path. Index ends with `/`. The layout for the same directory does not. The root layout is only `__root.tsx`. A `route.tsx` at the root of `routes` is an error, because it would also be the URL `/`.

A `.css`, `.scss`, or `.sass` file next to the route enters the tree as a side-effect import. The order is `.css`, then `.scss`, then `.sass`.

A file with only `.go` becomes an endpoint and client, and does not enter the router tree. A file with only `.tsx` is a UI route. `*_test.go`, `doc.go`, and `*.gen.*` are ignored.

## Go endpoints

The compiler rejects `$` in a file name, and an import path rejects `$` in a directory. The UI keeps `$id.tsx`. The endpoint sits beside it, under a different name:

| UI | Go |
| --- | --- |
| `clients/$id.tsx` | `clients/param_id.go` |
| `clients/$id/route.tsx` | `clients/param_id/route.go` |
| `api/trpc/$.tsx` | `api/trpc/splat.go` |
| `posts.$postId.tsx` | `posts.param_postId.go` |
| `_auth.tsx` | `pathless_auth.go` |
| `_auth.login.tsx` | `pathless_auth.login.go` |
| `_auth/login.tsx` | `_auth/login.go` |

A directory named `$id` cannot contain `.go`. The generator asks you to rename it before looking for `go.mod`. A file whose name starts with `_` is also rejected, because the go tool ignores that name. The `_auth` directory remains valid.

A static segment with the literal name `param_<name>`, `splat`, or `pathless_<name>` is rewritten to `$<name>`, `$`, or `_<name>`.

A handler is an exported function, with no receiver, in one of these forms:

```go
r := gnact.NewRouter()
_ = gnact.POST(r, "/clients", Create)
_ = gnact.GET(r, "/clients/{id}", Get)
```

`T` and `R` are structs, or pointers to structs. Functions that do not match this form are ignored. An exported function that matches this form is an endpoint, even if the file name looks like a helper.

Handler names are unique within the directory, because the files share the same Go package.

The principal comes only from `Server.Authenticate`, through the context. The JSON does not define identity.

Tags:

- `json:"name"` is the contract.
- `route:"server"` exists on the response when the struct is returned from the handler, and is rejected if the client sends the field. The public response remains `invalid request`.
- `route:"required"` requires the key in the JSON. Zero and `false` are valid. The public message includes the JSON name, for example `missing field id`.
- `json:"-"` and `route:"-"` are left out.
- An unknown field is `invalid request`.

`*route.Error` is public: the client sees `code` and `message`. Any other error, including panic, becomes `internal` / `internal error`. The real text reaches only `OnError`.

A TypeScript enum is a set of exported constants of a named type:

```go
//gnact:POST /clients
func Create(ctx context.Context, in CreateIn) (Client, error)

//gnact:GET /clients/{id}
func Get(ctx context.Context, in struct {
    ID string `path:"id"`
}) (Client, error)
```

This generates `"admin" | "user"`.

Tags de input: `json` (body em POST/PUT/PATCH), `path:"nome"`, `query:"nome"`, `route:"required"|"server"|"-"`.

`POST /rpc/{service}/{method}`. The body is the JSON object. An empty body becomes `{}`. A successful `200` returns the handler JSON, with no envelope. A handler with no output returns `{}`.

Error:

```json
{
  "type": "urn:gnact:error:invalid_argument",
  "title": "Bad Request",
  "status": 400,
  "detail": "missing field id",
  "code": "invalid_argument"
}
```

An unknown procedure responds `404` with `not found` and does not list methods. `GET` on a known procedure responds `405`.

`Content-Type` is empty or `application/json`. The default body fits in 1 MiB. Above that, `413`. `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`.

Nil slices and maps go out as `[]` and `{}`. A nil value inside a map is not normalized.

## Security

The mux is an allowlist. Only what was registered gets in.

A call without `Origin` is a non-browser client and passes. With `Origin`, the value must be `scheme://Host` of this request, or an item in `AllowedOrigins`. Never `*` and never `null`. Besides `Origin`, the browser must send `X-Route-Request: 1`. Without that header, the response is `403`.

CORS uses credentials, `Vary: Origin`, preflight `OPTIONS` with `204`, `Allow-Headers` including `Content-Type` and `X-Route-Request`, and `Max-Age` 600.

The generated client sends `Content-Type: application/json` and `X-Route-Request: 1`. With an empty `baseURL`, credentials are `same-origin`. With an absolute base, they are `include`. A trailing slash on `baseURL` is stripped.

A non-JSON response becomes `ClientError` with message `invalid response`. The body is not copied into the error.

In the router, `navigate` accepts only a path. Absolute URLs and protocol-relative URLs (`//host`) fail with `navigation href must be a path`.

## CLI

The binary is `gnact`. Each module is a subcommand, registered in-process: `routegen`, `template`, and `test`. With no module, `gnact` writes the list to stdout and exits with code 0. `gnact help <module>` writes that module's flags. An unknown flag, unknown module, or extra argument exits with code 2, without repeating the full help. Failure to generate, write, or run `go test` exits with code 1. The list of files created by `template` goes to stdout, one path per line.

### routegen

`gnact routegen` writes the client, the registers, and the tree. With no arguments, the behavior is that of `generate`. `gnact routegen generate` is the same command.

| Flag | Default | Use |
| --- | --- | --- |
| `-dir` | `routes` | routes directory |
| `-check` | `false` | compare generated files and do not write |
| `-import_path` | `github.com/yurimoinhos/go-n-act` | Go import of this module, written into the register |
| `-ts_import` | `@aggitech/route` | TypeScript module |

Silent `-check` means the disk matches. An extra managed file (`register.gen.go`, `routeTree.gen.tsx`, `*.gen.ts` with the generator header) also fails the check. Without `-check`, those leftover files are deleted.

### template

`gnact template init [dir]` creates an app or only the routes folder. An empty `dir` is the current directory.

| Flag | Default | Use |
| --- | --- | --- |
| `-template` | `app` | `app` or `routes` |
| `-module` | `example.com/app` if `dir` is `.`, otherwise `example.com/<name>` | new module path |
| `-dir` | `routes` | routes folder, relative to the project |
| `-replace` | off | writes a `replace` in `go.mod` pointing at this local module |
| `-import_path` | `github.com/yurimoinhos/go-n-act` | Go import of this module |
| `-ts_import` | `@aggitech/route` | TypeScript module |

`app` writes `go.mod` (`go 1.23.0`), `cmd/server/main.go`, `package.json`, `index.html`, `src/main.tsx`, `vite.config.ts`, `.gitignore`, and the routes `__root.tsx`, `index.tsx`, and `index.go`. The server listens on `:8080`. The minimal UI mounts `routeTree.gen` with `createRouter`. The command does not install npm dependencies.

`routes` writes only `__root.tsx`, `index.tsx`, and `index.go` inside an existing module. If there is a `go.mod` above the folder, the generator runs afterward.

`init` refuses an existing `go.mod` for the `app` template, that template's destination routes folder, and any file it would itself write.

`gnact template new <route>` creates a route. The key follows the file name, with an optional leading `/`. A trailing slash becomes index, except when the last segment is already `index`.

| Example | Files |
| --- | --- |
| `about` | `about.tsx`, `about.go` |
| `clients/` | `clients/index.tsx`, `clients/index.go` |
| `clients/route` | layout `clients/route.tsx`, `clients/route.go` |
| `clients/$id` | `clients/$id.tsx`, `clients/param_id.go` |
| `clients/$id/route` | `clients/$id/route.tsx`, `clients/param_id/route.go` |
| `api/trpc/$` | `api/trpc/$.tsx`, `api/trpc/splat.go` |
| `posts.$postId` | `posts.$postId.tsx`, `posts.param_postId.go` |
| `_auth` | `_auth.tsx`, `pathless_auth.go` |
| `_auth/login` | `_auth/login.tsx`, `_auth/login.go` |

| Flag | Default | Use |
| --- | --- | --- |
| `-dir` | `routes` | routes directory |
| `-only` | `both` | `both`, `ui`, or `api` |
| `-style` | empty | `css`, `scss`, or `sass`, with the same stem as the `.tsx` |
| `-generate` | `true` | writes client, registers, and tree |
| `-import_path` | `github.com/yurimoinhos/go-n-act` | Go import of this module |
| `-ts_import` | `@aggitech/route` | TypeScript module |

`-style` with `-only api` is an error. The `route` route at the root and the `__root` route are errors: the root layout is `__root.tsx`. An existing file is not overwritten. The `.tsx` calls `createFileRoute` with the full path. The `.go` exports a handler named after the last segment and a comment that the generator copies. A parameter becomes a `route:"required"` field. A splat becomes `Rest` with `json:"rest,omitempty"`.

### test

`gnact test` compares the generated files with the disk and, if they match, runs `go test ./...` in the module that contains the routes folder. It does not write generation. `go test` output goes to stdout and stderr.

| Flag | Default | Use |
| --- | --- | --- |
| `-dir` | `routes` | routes directory |
| `-import_path` | `github.com/yurimoinhos/go-n-act` | Go import used for comparison |
| `-ts_import` | `@aggitech/route` | TypeScript module used for comparison |

Each Go package gets `Register`. The root gets `RegisterAll`, which calls the children and the local `Register` when the root has an endpoint.

The `.tsx` must export `Route` and call `createFileRoute` with the exact path for that route. The first call in the file counts. A template string with `${}` is an error. `__root.tsx` calls `createRootRoute`.

## Router

`createFileRoute("/clients/$id")` returns the route. `addChildren` links the parent. `createRootRoute` is the `__root` root.

Leaf selection looks at the whole tree. A static segment scores 10, a parameter scores 3, a splat scores 1, and index adds 1 more. A tie goes to the smaller id. The URL loses its trailing slash before comparison, so `/clients` picks the index `/clients/` and the layout `/clients` stays in the parent chain. A pathless segment is never a leaf.

`beforeLoad` and loaders run from parent to child. `validateSearch` runs before that route's `beforeLoad`. `redirect()` aborts the load. `replace` defaults to true. The ninth redirect fails with `too many redirects`. `notFound()` and a URL with no route end with status `notFound`.

While the new load is pending, previous matches stay. A superseded load does not write its result and does not apply its redirect. Two in-flight navigations to the same href share one load. The cache stores only loaders that finished successfully. The key is the route id plus the href. `staleTime` 0 fetches again.

`MemoryHistory` does not notify listeners on `push` and `replace`. `back` and `forward` do.

`createRouter` follows the browser address bar (`BrowserHistory`: `pushState`, `replaceState`, `popstate`) when a `window` exists, and memory (`MemoryHistory`) elsewhere. Pass `history` to choose. `RouterProvider` loads the current location on mount when nothing has loaded yet.

`RouterProvider` renders the root component, or `Outlet`. `Outlet` goes one level down. `Link` calls `preventDefault` on an unmodified left click.

The hooks `useLoaderData`, `useParams`, and `useSearch` exist on the route object after the `@aggitech/route` entry is imported.

## Limits

- `int64` becomes `number`. Above `2^53` the value is not exact.
- `[]byte` and `time.Time` become `string`.
- `interface`, `any`, channel, and function do not generate a type.
- Nil inside a map is not normalized.
- Exported handlers in the same directory need unique names.
- A Go file and directory cannot contain `$`. Use `param_<name>.go` and `splat.go`.
- A Go file cannot start with `_`. Use `pathless_<name>.go` next to `_<name>.tsx`. An `_auth` directory may contain `.go`.
- An exported function with a handler signature is an endpoint.
- An enum must be an exported constant of a named type.
- The root layout is `__root.tsx`, not `route.tsx`.
- The generated client imports `@aggitech/route/client`.
- `gnact/` at the root of the routes directory is reserved for the embedded runtime. routegen skips it when scanning routes.
