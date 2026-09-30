package routegen

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddRouteFiles(t *testing.T) {
	cases := []struct {
		route   string
		tsx     string
		goRel   string
		pkg     string
		fn      string
		pattern string
		want    []string
	}{
		{
			route: "clients/$id", tsx: "clients/$id.tsx", goRel: "clients/param_id.go",
			pkg: "package clients", fn: "func ParamId(", pattern: `"/clients/$id"`,
			want: []string{"path:\"id\"", "route:\"required\"", "Id string", "gnact:GET /clients/{id}"},
		},
		{
			route: "clients/", tsx: "clients/index.tsx", goRel: "clients/index.go",
			pkg: "package clients", fn: "func Index(", pattern: `"/clients/"`,
			want: []string{"gnact:POST /clients"},
		},
		{
			route: "clients/index/", tsx: "clients/index.tsx", goRel: "clients/index.go",
			pkg: "package clients", fn: "func Index(", pattern: `"/clients/"`,
		},
		{
			route: "api/trpc/$", tsx: "api/trpc/$.tsx", goRel: "api/trpc/splat.go",
			pkg: "package trpc", fn: "func Splat(", pattern: `"/api/trpc/$"`,
			want: []string{"path:\"rest\"", "Rest string", "gnact:GET /api/trpc/{rest}"},
		},
		{
			route: "clients/route", tsx: "clients/route.tsx", goRel: "clients/route.go",
			pkg: "package clients", fn: "func Route(", pattern: `"/clients"`,
		},
		{
			route: "clients/$id/route", tsx: "clients/$id/route.tsx", goRel: "clients/param_id/route.go",
			pkg: "package param_id", fn: "func Route(", pattern: `"/clients/$id"`,
			want: []string{"path:\"id\"", "route:\"required\"", "gnact:GET /clients/{id}"},
		},
		{
			route: "_auth", tsx: "_auth.tsx", goRel: "pathless_auth.go",
			pkg: "package routes", fn: "func PathlessAuth(", pattern: `"/_auth"`,
		},
		{
			route: "_auth/login", tsx: "_auth/login.tsx", goRel: "_auth/login.go",
			pkg: "package pathless_auth", fn: "func Login(", pattern: `"/login"`,
		},
		{
			route: "posts.$postId", tsx: "posts.$postId.tsx", goRel: "posts.param_postId.go",
			pkg: "package routes", fn: "func ParamPostId(", pattern: `"/posts/$postId"`,
			want: []string{"path:\"postId\"", "route:\"required\"", "PostId string", "gnact:GET /posts/{postId}"},
		},
		{
			route: "/", tsx: "index.tsx", goRel: "index.go",
			pkg: "package routes", fn: "func Index(", pattern: `"/"`,
			want: []string{"gnact:POST /"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.route, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "routes")
			_, err := Add(context.Background(), AddOptions{Dir: dir, Route: tc.route})
			if err != nil {
				t.Fatal(err)
			}
			tsx := readText(t, filepath.Join(dir, filepath.FromSlash(tc.tsx)))
			if !strings.Contains(tsx, tc.pattern) || !strings.Contains(tsx, "createFileRoute") {
				t.Fatalf("tsx = %s", tsx)
			}
			body := readText(t, filepath.Join(dir, filepath.FromSlash(tc.goRel)))
			if !strings.Contains(body, tc.pkg) || !strings.Contains(body, tc.fn) || !strings.Contains(body, "handles this route.") {
				t.Fatalf("go = %s", body)
			}
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Fatalf("go missing %s in %s", want, body)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(tc.goRel)+".wrong")); err == nil {
				t.Fatal("unexpected file")
			}
		})
	}
}

func TestAddRefusesSecondWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "routes")
	if _, err := Add(context.Background(), AddOptions{Dir: dir, Route: "about"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "about.go")
	before := readText(t, path)
	if _, err := Add(context.Background(), AddOptions{Dir: dir, Route: "about"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatal(err)
	}
	if readText(t, path) != before {
		t.Fatal("second add changed the file")
	}
}

func TestAddStyleAndOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "routes")
	if _, err := Add(context.Background(), AddOptions{Dir: dir, Route: "clients/$id", Style: "scss"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "clients", "$id.scss")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "clients", "param_id.scss")); !os.IsNotExist(err) {
		t.Fatal(err)
	}

	api := filepath.Join(t.TempDir(), "routes")
	if _, err := Add(context.Background(), AddOptions{Dir: api, Route: "about", Only: "api", Style: "css"}); err == nil || !strings.Contains(err.Error(), "style applies") {
		t.Fatal(err)
	}
	if _, err := os.Stat(api); !os.IsNotExist(err) {
		t.Fatal(err)
	}

	ui := filepath.Join(t.TempDir(), "routes")
	if _, err := Add(context.Background(), AddOptions{Dir: ui, Route: "about", Only: "ui"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ui, "about.tsx")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ui, "about.go")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestAddRejectsRootNames(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "routes")
	for _, route := range []string{"route", "__root", "../x"} {
		if _, err := Add(context.Background(), AddOptions{Dir: dir, Route: route}); err == nil {
			t.Fatalf("route %s", route)
		}
	}
	if _, err := Add(context.Background(), AddOptions{Dir: dir, Route: "route"}); err == nil || !strings.Contains(err.Error(), "__root.tsx") {
		t.Fatal(err)
	}
}

func TestAddGeneratesClient(t *testing.T) {
	mod := t.TempDir()
	writeFile(t, filepath.Join(mod, "go.mod"), "module example.com/app\n\ngo 1.23.0\n")
	routes := filepath.Join(mod, "routes")
	paths, err := Add(context.Background(), AddOptions{Dir: routes, Route: "clients/$id", Generate: true})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(paths, "\n")
	if !strings.Contains(joined, "clients/param_id.gen.ts") || !strings.Contains(joined, "clients/register.gen.go") {
		t.Fatalf("paths = %s", joined)
	}
	gen := readText(t, filepath.Join(routes, "clients", "param_id.gen.ts"))
	if !strings.Contains(gen, "id: string") || !strings.Contains(gen, "ParamId handles this route.") {
		t.Fatalf("gen = %s", gen)
	}
}

func TestInitAppBuilds(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo")
	paths, err := Init(context.Background(), InitOptions{
		Dir:      dir,
		Module:   "example.com/app",
		Template: "app",
		Replace:  repoRoot(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(paths, "\n")
	for _, rel := range []string{
		"go.mod",
		"cmd/server/main.go",
		"package.json",
		"src/main.tsx",
		"routes/__root.tsx",
		"routes/index.tsx",
		"routes/index.go",
		"routes/register.gen.go",
		"routes/routeTree.gen.tsx",
		"routes/index.gen.ts",
		"routes/gnact/client.ts",
		"routes/gnact/index.ts",
	} {
		if !strings.Contains(joined, rel) {
			t.Fatalf("missing %s in %s", rel, joined)
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Fatal(rel, err)
		}
	}
	mod := readText(t, filepath.Join(dir, "go.mod"))
	if !strings.Contains(mod, "module example.com/app") || !strings.Contains(mod, "replace github.com/yurimoinhos/go-n-act =>") {
		t.Fatalf("go.mod = %s", mod)
	}
	pkg := readText(t, filepath.Join(dir, "package.json"))
	if strings.Contains(pkg, "@aggitech/route") || strings.Contains(pkg, "go-n-act") || strings.Contains(pkg, "gnact") {
		t.Fatalf("package.json must not depend on gnact: %s", pkg)
	}
	if !strings.Contains(pkg, `"react"`) || !strings.Contains(pkg, `"vite"`) {
		t.Fatalf("package.json = %s", pkg)
	}
	server := readText(t, filepath.Join(dir, "cmd", "server", "main.go"))
	if !strings.Contains(server, "\":8080\"") || !strings.Contains(server, "RegisterAll") {
		t.Fatalf("server = %s", server)
	}
	entry := readText(t, filepath.Join(dir, "src", "main.tsx"))
	if !strings.Contains(entry, `from "../routes/routeTree.gen"`) || !strings.Contains(entry, "createRouter") || !strings.Contains(entry, "routes/gnact/index.ts") {
		t.Fatalf("entry = %s", entry)
	}
	root := readText(t, filepath.Join(dir, "routes", "__root.tsx"))
	if !strings.Contains(root, "createRootRoute") || !strings.Contains(root, "Outlet") || !strings.Contains(root, "./gnact/index.ts") {
		t.Fatalf("root = %s", root)
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(t.TempDir(), "server"), "./cmd/server")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	if _, err := Init(context.Background(), InitOptions{Dir: dir, Module: "example.com/app"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatal(err)
	}
}

func TestInitRoutesTemplate(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.23.0\n")
	paths, err := Init(context.Background(), InitOptions{Dir: dir, Template: "routes"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(paths, "\n")
	if strings.Contains(joined, "package.json") || strings.Contains(joined, "cmd/server/main.go") {
		t.Fatalf("paths = %s", joined)
	}
	if _, err := os.Stat(filepath.Join(dir, "routes", "register.gen.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "package.json")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func readText(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
