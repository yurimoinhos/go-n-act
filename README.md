# route / gnact

Biblioteca e CLI para rotas colocadas e actions REST. Cada rota de UI é um `.tsx`; o servidor Go registra handlers com verbos HTTP reais. O runtime TypeScript (cliente + router) é **embutido no binário `gnact`** e escrito em `routes/gnact/` no codegen — o `package.json` do app usa npm só para React/Vite, **sem dependência npm do gnact**.

## Começar

```bash
go get github.com/yurimoinhos/go-n-act
go install github.com/yurimoinhos/go-n-act/cmd/gnact@latest
```

No diretório do projeto:

```bash
gnact routegen -dir routes
```

Isso gera o cliente tipado, os `register.gen.go`, a árvore de rotas e, quando há UI ou handlers, o runtime em `routes/gnact/` (mesma versão do `gnact` que gerou).

No `main` do servidor:

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

Na UI, importe a árvore gerada e o runtime local (`./gnact` ou `../routes/gnact`). React no browser não precisa de Node em runtime; Node/Bun entram no **dev/build** (Vite). Em produção o Go (ou outro static host) serve o bundle.

## Registro de handlers

Dois modos, nenhum obrigatório — use um, o outro, ou os dois.

### API runtime

```go
r := gnact.NewRouter()
_ = gnact.POST(r, "/clients", Create)
_ = gnact.GET(r, "/clients/{id}", Get)
```

### Arquivo + diretiva

```go
//gnact:POST /clients
func Create(ctx context.Context, in CreateIn) (Client, error)

//gnact:GET /clients/{id}
func Get(ctx context.Context, in struct {
    ID string `path:"id"`
}) (Client, error)
```

Pasta opcional `actions/` (`gnact routegen -actions actions`). Handler exportado **sem** `//gnact:METHOD` não vira endpoint.

Tags de input: `json` (body em POST/PUT/PATCH), `path:"nome"`, `query:"nome"`, `route:"required"|"server"|"-"`.

## HTTP

`GET` / `POST` / `PATCH` / `PUT` / `DELETE` no path declarado. `OPTIONS` automático (CORS). Sucesso `200` com JSON. Erro em **RFC 7807** (`application/problem+json`):

```json
{
  "type": "urn:gnact:error:invalid_argument",
  "title": "Bad Request",
  "status": 400,
  "detail": "missing field id",
  "code": "invalid_argument"
}
```

OpenAPI opcional: `gnact routegen -openapi`.

## Arquivos de rota (UI)

A chave da rota é o caminho do arquivo sem extensão (mesmo modelo de file routes: layout, index, `$param`, splat, pathless).

| Arquivo | URL |
| --- | --- |
| `__root.tsx` | layout raiz |
| `index.tsx` | `/` |
| `clients/index.tsx` | `/clients/` |
| `clients/$id.tsx` | `/clients/$id` |

Go ao lado usa `param_id.go` (sem `$` no nome). Arquivo só `.go` com diretiva vira API + cliente, sem entrar na árvore do router. Arquivo só `.tsx` é UI.

## Segurança

Allowlist: só o que foi registrado. Browser com `Origin` precisa de `X-Route-Request: 1`. Origin allowlist (nunca `*`). Principal só via `Server.Authenticate`.

## CLI

```bash
gnact routegen [-dir routes] [-actions actions] [-openapi] [-check]
gnact template init [dir]
gnact template new <rota>
gnact test
```

`-ts_import` vazio (padrão) embute `routes/gnact/`. Só passe um specifier npm se quiser sobrescrever de propósito.

Templates Go do scaffold vivem em `routegen/templates/*.go.tmpl` (extensão `.go.tmpl` para o `go` tool não tentar compilá-los; `.tmpl.go` seria tratado como source).

## Limites

- `int64` vira `number` (precisão JS).
- Handlers no mesmo diretório precisam de nomes únicos.
- Diretiva `//gnact:METHOD /path` obrigatória para expor o handler via arquivo.
