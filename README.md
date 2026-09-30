# route

Biblioteca para rotas colocadas. Cada rota é um arquivo `.tsx` e, quando a rota tem servidor, um arquivo `.go` no mesmo diretório. O `.go` é o único endpoint daquela rota. A UI chama esse endpoint pelo cliente gerado, em HTTP/1.1 com JSON.

O router segue o modelo de rotas por arquivo: caminho completo, layout, index, parâmetro, splat, segmento sem URL, loader e `beforeLoad`. Não depende do TanStack Router. React é dependência opcional do entry da UI.

## Começar

```bash
go get github.com/yurimoinhos/go-n-act
go install github.com/yurimoinhos/go-n-act/cmd/routegen@latest
```

No diretório `routes` do projeto:

```bash
routegen -dir routes -import_path example.com/app/routes -ts_import @aggitech/route
```

No `main` do servidor:

```go
mux := route.NewMux()
if err := routes.RegisterAll(mux); err != nil {
    log.Fatal(err)
}
log.Fatal(http.ListenAndServe(":8080", &route.Server{
    Mux: mux,
    Authenticate: func(r *http.Request) (route.Principal, error) {
        return route.Principal{Subject: "ada"}, nil
    },
}))
```

Na UI, importe a árvore gerada e o entry `@aggitech/route`. O cliente gerado importa `@aggitech/route/client`, então o bundle que só chama o servidor não puxa o React.

## Arquivos de rota

A chave da rota é o caminho do arquivo sem extensão.

| Arquivo | URL | Serviço |
| --- | --- | --- |
| `__root.tsx` | layout raiz | `route.root.v1` |
| `index.tsx` | `/` | `route.index.v1` |
| `about.tsx` | `/about` | `route.about.v1` |
| `clients/route.tsx` | `/clients` | `route.clients.layout.v1` |
| `clients/index.tsx` | `/clients/` | `route.clients.index.v1` |
| `clients/$id.tsx` | `/clients/$id` | `route.clients.param_id.v1` |
| `clients/$id/route.tsx` | `/clients/$id` (layout) | `route.clients.param_id.layout.v1` |
| `api/trpc/$.tsx` | `/api/trpc/$` | `route.api.trpc.splat.v1` |
| `posts.$postId.tsx` | `/posts/$postId` | `route.posts.param_postId.v1` |
| `_auth.tsx` | segmento sem URL `/_auth` | `route.pathless_auth.v1` |

`createFileRoute` recebe o caminho completo da URL. Index termina com `/`. O layout do mesmo diretório não termina. O layout raiz é só `__root.tsx`. Um `route.tsx` na raiz de `routes` é erro, porque também seria a URL `/`.

Um `.css`, `.scss` ou `.sass` ao lado da rota entra na árvore como import de efeito. A ordem é `.css`, depois `.scss`, depois `.sass`.

Arquivo só com `.go` vira endpoint e cliente, e não entra na árvore do router. Arquivo só com `.tsx` é rota de UI. `*_test.go`, `doc.go` e `*.gen.*` são ignorados.

## Endpoints Go

O compilador rejeita `$` no nome do arquivo, e um import path rejeita `$` no diretório. A UI continua com `$id.tsx`. O endpoint fica ao lado, com outro nome:

| UI | Go |
| --- | --- |
| `clients/$id.tsx` | `clients/param_id.go` |
| `api/trpc/$.tsx` | `api/trpc/splat.go` |
| `posts.$postId.tsx` | `posts.param_postId.go` |

Um diretório chamado `$id` não pode conter `.go`. O gerador pede para renomear antes de procurar o `go.mod`.

Um handler é uma função exportada, sem receiver, com uma destas formas:

```go
func(ctx context.Context, in T) (R, error)
func(ctx context.Context, in T) error
func(ctx context.Context) (R, error)
func(ctx context.Context) error
```

`T` e `R` são structs, ou ponteiro para struct. Funções que não têm essa forma são ignoradas. Uma função exportada que tem essa forma é um endpoint, mesmo que o nome do arquivo pareça um helper.

Nomes de handler são únicos dentro do diretório, porque os arquivos dividem o mesmo package Go.

O principal vem só de `Server.Authenticate`, pelo context. O JSON não define identidade.

Tags:

- `json:"nome"` é o contrato.
- `route:"server"` existe na resposta quando a struct volta do handler, e é rejeitado se o cliente enviar o campo. A resposta pública continua `invalid request`.
- `route:"required"` exige a chave no JSON. Zero e `false` são válidos. A mensagem pública inclui o nome JSON, por exemplo `missing field id`.
- `json:"-"` e `route:"-"` ficam de fora.
- Campo desconhecido é `invalid request`.

`*route.Error` é público: o cliente vê `code` e `message`. Qualquer outro erro, inclusive panic, vira `internal` / `internal error`. O texto real chega só em `OnError`.

Um enum no TypeScript é um conjunto de constantes exportadas de um tipo nomeado:

```go
type Role string

const (
    RoleAdmin Role = "admin"
    RoleUser  Role = "user"
)
```

Isso gera `"admin" | "user"`.

## HTTP

`POST /rpc/{service}/{method}`. O corpo é o objeto JSON. Corpo vazio vira `{}`. Sucesso `200` devolve o JSON do handler, sem envelope. Handler sem saída devolve `{}`.

Erro:

```json
{"code":"invalid_argument","message":"missing field id"}
```

Procedimento desconhecido responde `404` com `not found` e não lista os métodos. `GET` num procedimento conhecido responde `405`.

`Content-Type` vazio ou `application/json`. O corpo padrão cabe em 1 MiB. Acima disso, `413`. `Cache-Control: no-store` e `X-Content-Type-Options: nosniff`.

Slice e map nil saem como `[]` e `{}`. Valor nil dentro de um map não é normalizado.

## Segurança

O mux é uma allowlist. Só entra quem foi registrado.

Chamada sem `Origin` é cliente não-browser e passa. Com `Origin`, o valor tem de ser `scheme://Host` desta requisição, ou um item de `AllowedOrigins`. Nunca `*` e nunca `null`. Além do `Origin`, o browser precisa enviar `X-Route-Request: 1`. Sem esse header, a resposta é `403`.

CORS usa credencial, `Vary: Origin`, preflight `OPTIONS` com `204`, `Allow-Headers` incluindo `Content-Type` e `X-Route-Request`, e `Max-Age` 600.

O cliente gerado manda `Content-Type: application/json` e `X-Route-Request: 1`. Com `baseURL` vazio a credencial é `same-origin`. Com base absoluta, é `include`. A barra final do `baseURL` é removida.

Resposta que não é JSON vira `ClientError` com mensagem `invalid response`. O corpo não é copiado para o erro.

No router, `navigate` aceita só caminho. URL absoluta e URL protocol-relative (`//host`) falham com `navigation href must be a path`.

## CLI

`routegen` usa a flag da biblioteca padrão. Flags só no `main`.

| Flag | Padrão | Uso |
| --- | --- | --- |
| `-dir` | `routes` | diretório das rotas |
| `-check` | `false` | compara os arquivos gerados e não escreve |
| `-import_path` | `github.com/yurimoinhos/go-n-act` | import Go escrito no register |
| `-ts_import` | `@aggitech/route` | módulo TypeScript |

Argumento extra termina com código 2. Erro de geração termina com código 1. `-check` em silêncio significa que o disco está igual. Arquivo gerenciado a mais (`register.gen.go`, `routeTree.gen.tsx`, `*.gen.ts` com o cabeçalho do gerador) também falha o check. Sem `-check`, esses arquivos sobrando são apagados.

Cada package Go ganha `Register`. A raiz ganha `RegisterAll`, que chama os filhos e o `Register` local quando a raiz tem endpoint.

O `.tsx` precisa exportar `Route` e chamar `createFileRoute` com o caminho exato daquela rota. O primeiro call do arquivo vale. Template string com `${}` é erro. `__root.tsx` chama `createRootRoute`.

## Router

`createFileRoute("/clients/$id")` devolve a rota. `addChildren` liga o pai. `createRootRoute` é a raiz `__root`.

A escolha da folha olha a árvore inteira. Segmento estático vale 10, parâmetro vale 3, splat vale 1, index soma mais 1. Empate fica com o id menor. A URL perde a barra final antes de comparar, então `/clients` escolhe o index `/clients/` e o layout `/clients` continua na cadeia de pais. Segmento pathless nunca é folha.

`beforeLoad` e loaders correm do pai para o filho. `validateSearch` corre antes do `beforeLoad` daquela rota. `redirect()` interrompe a carga. `replace` vale true por padrão. O nono redirect falha com `too many redirects`. `notFound()` e URL sem rota terminam com status `notFound`.

Enquanto a carga nova está pendente, os matches anteriores ficam. Carga substituída não grava o resultado e não aplica o redirect dela. Duas navegações ao mesmo href em voo compartilham uma carga. O cache guarda só loader que terminou bem. A chave é o id da rota mais o href. `staleTime` 0 busca de novo.

`MemoryHistory` não avisa os listeners em `push` e `replace`. `back` e `forward` avisam.

`RouterProvider` desenha o componente da raiz, ou `Outlet`. `Outlet` desce um nível. `Link` chama `preventDefault` no clique esquerdo sem modificador.

Os hooks `useLoaderData`, `useParams` e `useSearch` existem no objeto da rota depois que o entry `@aggitech/route` é importado.

## Limites

- `int64` vira `number`. Acima de `2^53` o valor não é exato.
- `[]byte` e `time.Time` viram `string`.
- `interface`, `any`, canal e função não geram tipo.
- Nil dentro de um map não é normalizado.
- Handlers exportados no mesmo diretório precisam de nomes únicos.
- Arquivo e diretório Go não podem conter `$`. Use `param_<nome>.go` e `splat.go`.
- Uma função exportada com a assinatura de handler é um endpoint.
- Enum precisa ser constante exportada de um tipo nomeado.
- O layout raiz é `__root.tsx`, não `route.tsx`.
- O cliente gerado importa `@aggitech/route/client`.
