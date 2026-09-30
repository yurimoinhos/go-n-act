package routegen

import (
	"context"
	"fmt"
	"go/format"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultImportPath = "github.com/yurimoinhos/go-n-act"
	defaultTSImport   = "@aggitech/route"
)

// InitOptions configures a new app or a routes folder.
type InitOptions struct {
	// Dir is the project directory. Empty means ".".
	Dir string
	// RoutesDir is the routes folder relative to Dir. Empty means "routes".
	RoutesDir string
	// Module is the new Go module path. Empty selects a default from Dir.
	Module string
	// Template is "app" or "routes". Empty means "app".
	Template string
	// Replace is a local directory written as a go.mod replace for this module.
	// Empty omits the replace line. It applies to the app template.
	Replace string
	// TSImport is the TypeScript specifier. Empty means @aggitech/route.
	TSImport string
	// ImportPath is the Go import path of this module. Empty means github.com/yurimoinhos/go-n-act.
	ImportPath string
}

// AddOptions configures one new route.
type AddOptions struct {
	// Dir is the routes directory. Empty means "routes".
	Dir string
	// Route is the file-route key, with an optional leading slash.
	Route string
	// Only is "both", "ui", or "api". Empty means "both".
	Only string
	// Style is "css", "scss", or "sass". Empty writes no style file.
	Style string
	// Generate writes the typed client, registers, and route tree after the route files.
	Generate bool
	// TSImport is the TypeScript specifier. Empty means @aggitech/route.
	TSImport string
	// ImportPath is the Go import path of this module. Empty means github.com/yurimoinhos/go-n-act.
	ImportPath string
}

// Init writes a project template and the root plus index routes.
// Paths in the result are relative to the project directory.
func Init(ctx context.Context, opt InitOptions) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opt.Dir == "" {
		opt.Dir = "."
	}
	template := opt.Template
	if template == "" {
		template = "app"
	}
	if template != "app" && template != "routes" {
		return nil, fmt.Errorf("routegen: -template must be app or routes")
	}
	if template == "routes" && opt.Replace != "" {
		return nil, fmt.Errorf("routegen: replace applies to the app template")
	}
	routesRel, err := cleanRoutesRel(opt.RoutesDir)
	if err != nil {
		return nil, err
	}
	project, err := filepath.Abs(opt.Dir)
	if err != nil {
		return nil, err
	}
	routesAbs := filepath.Join(project, filepath.FromSlash(routesRel))
	tsImport := opt.TSImport
	if tsImport == "" {
		tsImport = defaultTSImport
	}
	lib := opt.ImportPath
	if lib == "" {
		lib = defaultImportPath
	}

	var created []string
	switch template {
	case "app":
		module := opt.Module
		if module == "" {
			module = defaultModule(opt.Dir)
		}
		if err := validModulePath(module); err != nil {
			return nil, err
		}
		targets := []string{
			filepath.Join(project, "go.mod"),
			routesAbs,
			filepath.Join(project, "cmd", "server", "main.go"),
			filepath.Join(project, "package.json"),
			filepath.Join(project, "index.html"),
			filepath.Join(project, "src", "main.tsx"),
			filepath.Join(project, "vite.config.ts"),
			filepath.Join(project, ".gitignore"),
		}
		for _, target := range targets {
			if err := already(target); err != nil {
				return nil, err
			}
		}
		goMod, err := renderGoMod(module, lib, opt.Replace)
		if err != nil {
			return nil, err
		}
		mainSrc, err := renderMain(lib, module+"/"+routesRel, dirPackage(routesAbs, "index.go"))
		if err != nil {
			return nil, err
		}
		files := []struct {
			rel  string
			body string
		}{
			{rel: "go.mod", body: goMod},
			{rel: "cmd/server/main.go", body: mainSrc},
			{rel: "package.json", body: renderPackageJSON(npmName(opt.Dir), tsImport)},
			{rel: "index.html", body: indexHTML},
			{rel: "src/main.tsx", body: renderEntry(tsImport, path.Join("..", routesRel, "routeTree.gen"))},
			{rel: "vite.config.ts", body: viteConfig},
			{rel: ".gitignore", body: gitignore},
			{rel: path.Join(routesRel, "__root.tsx"), body: renderRoot(tsImport)},
		}
		for _, file := range files {
			if err := writeNew(filepath.Join(project, filepath.FromSlash(file.rel)), []byte(file.body)); err != nil {
				return nil, err
			}
			created = append(created, file.rel)
		}
	default:
		for _, rel := range []string{"__root.tsx", "index.tsx", "index.go"} {
			if err := already(filepath.Join(routesAbs, filepath.FromSlash(rel))); err != nil {
				return nil, err
			}
		}
		rootRel := path.Join(routesRel, "__root.tsx")
		if err := writeNew(filepath.Join(project, filepath.FromSlash(rootRel)), []byte(renderRoot(tsImport))); err != nil {
			return nil, err
		}
		created = append(created, rootRel)
	}

	generate := template == "app"
	if template == "routes" {
		if _, err := findModule(routesAbs); err == nil {
			generate = true
		}
	}
	added, err := Add(ctx, AddOptions{
		Dir:        routesAbs,
		Route:      "index",
		Only:       "both",
		Generate:   generate,
		TSImport:   tsImport,
		ImportPath: lib,
	})
	if err != nil {
		return nil, err
	}
	for _, rel := range added {
		created = append(created, path.Join(routesRel, rel))
	}
	sort.Strings(created)
	return created, nil
}

// Add writes the files for one route. Paths in the result are relative to the routes directory.
// An existing target file is left unchanged.
func Add(ctx context.Context, opt AddOptions) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opt.Dir == "" {
		opt.Dir = "routes"
	}
	only := opt.Only
	if only == "" {
		only = "both"
	}
	switch only {
	case "both", "ui", "api":
	default:
		return nil, fmt.Errorf("routegen: -only must be both, ui, or api")
	}
	if opt.Style != "" {
		switch opt.Style {
		case "css", "scss", "sass":
		default:
			return nil, fmt.Errorf("routegen: -style must be css, scss, or sass")
		}
	}
	if only == "api" && opt.Style != "" {
		return nil, fmt.Errorf("routegen: style applies to the UI file")
	}
	key, err := parseRouteKey(opt.Route)
	if err != nil {
		return nil, err
	}
	if _, err := classify(key); err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(opt.Dir)
	if err != nil {
		return nil, err
	}
	tsImport := opt.TSImport
	if tsImport == "" {
		tsImport = defaultTSImport
	}

	type pending struct {
		rel  string
		body []byte
	}
	var files []pending
	var rels []string
	if only != "api" {
		meta, err := classify(key)
		if err != nil {
			return nil, err
		}
		name, err := handlerName(key)
		if err != nil {
			return nil, err
		}
		rel := key + ".tsx"
		rels = append(rels, rel)
		files = append(files, pending{rel: rel, body: []byte(renderTSX(tsImport, meta.pattern, name+"Page"))})
		if opt.Style != "" {
			style := key + "." + opt.Style
			rels = append(rels, style)
			files = append(files, pending{rel: style})
		}
	}
	if only != "ui" {
		goRel := goRelFromKey(key)
		name, err := handlerName(key)
		if err != nil {
			return nil, err
		}
		fields, err := inputFields(key)
		if err != nil {
			return nil, err
		}
		body, err := renderHandler(dirPackage(dir, goRel), name, fields)
		if err != nil {
			return nil, err
		}
		rels = append(rels, goRel)
		files = append(files, pending{rel: goRel, body: body})
	}
	for _, file := range files {
		if err := already(filepath.Join(dir, filepath.FromSlash(file.rel))); err != nil {
			return nil, err
		}
	}
	if err := preview(dir, rels); err != nil {
		return nil, err
	}
	var created []string
	for _, file := range files {
		abs := filepath.Join(dir, filepath.FromSlash(file.rel))
		if err := writeNew(abs, file.body); err != nil {
			return nil, err
		}
		created = append(created, file.rel)
	}
	if opt.Generate {
		gen, err := Generate(ctx, Options{Dir: dir, ImportPath: opt.ImportPath, TSImport: tsImport})
		if err != nil {
			return nil, err
		}
		if err := Write(dir, gen, false); err != nil {
			return nil, err
		}
		for abs := range gen {
			created = append(created, slashRel(dir, abs))
		}
	}
	sort.Strings(created)
	return created, nil
}

func preview(dir string, extra []string) error {
	var rels []string
	if _, err := os.Stat(dir); err == nil {
		rels, err = scan(dir)
		if err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	rels = append(rels, extra...)
	if err := rejectDollarDirs(rels); err != nil {
		return err
	}
	_, err := Plan(rels)
	return err
}

func parseRouteKey(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("routegen: missing route")
	}
	if strings.ContainsAny(raw, "\\ \t") || strings.Contains(raw, "//") {
		return "", fmt.Errorf("routegen: invalid route %q", raw)
	}
	trailing := len(raw) > 1 && strings.HasSuffix(raw, "/")
	raw = strings.Trim(raw, "/")
	if raw == "" {
		return "index", nil
	}
	parts := strings.Split(raw, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("routegen: invalid route %q", raw)
		}
	}
	if trailing && parts[len(parts)-1] != "index" {
		raw += "/index"
	}
	if raw == "route" || raw == "__root" || strings.HasSuffix(raw, "/__root") {
		return "", fmt.Errorf("routegen: the root layout is __root.tsx")
	}
	return raw, nil
}

func lastToken(key string) string {
	base := path.Base(key)
	if i := strings.LastIndex(base, "."); i >= 0 {
		return base[i+1:]
	}
	return base
}

func handlerName(key string) (string, error) {
	name := exportIdent(lastToken(key))
	if name == "" || !token.IsIdentifier(name) || !token.IsExported(name) {
		return "", fmt.Errorf("routegen: cannot name a handler for %s", key)
	}
	return name, nil
}

type inputField struct {
	Name     string
	JSON     string
	Required bool
	Omit     bool
}

func inputFields(key string) ([]inputField, error) {
	var fields []inputField
	seen := map[string]bool{}
	for _, part := range strings.Split(key, "/") {
		for _, tok := range strings.Split(part, ".") {
			switch {
			case tok == "$":
				if seen["rest"] {
					return nil, fmt.Errorf("routegen: duplicate parameter rest")
				}
				seen["rest"] = true
				fields = append(fields, inputField{Name: "Rest", JSON: "rest", Omit: true})
			case strings.HasPrefix(tok, "$"):
				jsonName := tok[1:]
				if !routeIdent(jsonName) {
					return nil, fmt.Errorf("routegen: invalid parameter %s", tok)
				}
				if seen[jsonName] {
					return nil, fmt.Errorf("routegen: duplicate parameter %s", jsonName)
				}
				seen[jsonName] = true
				fields = append(fields, inputField{Name: exportIdent(jsonName), JSON: jsonName, Required: true})
			}
		}
	}
	return fields, nil
}

func dirPackage(routesDir, goRel string) string {
	dir := path.Dir(goRel)
	base := filepath.Base(routesDir)
	if dir != "." && dir != "" {
		base = path.Base(dir)
	}
	if isPathless(base) {
		trimmed := strings.TrimLeft(base, "_")
		if trimmed == "" {
			trimmed = "path"
		}
		base = "pathless_" + trimmed
	}
	return packageName(base)
}

func renderHandler(pkg, name string, fields []inputField) ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\nimport \"context\"\n\n// %s handles this route.\nfunc %s(_ context.Context, _ %sIn) (%sOut, error) {\n\treturn %sOut{OK: true}, nil\n}\n\ntype %sIn struct {\n",
		pkg, name, name, name, name, name, name)
	for _, field := range fields {
		fmt.Fprintf(&b, "\t%s string %s\n", field.Name, structTag(field.JSON, field.Omit, field.Required))
	}
	fmt.Fprintf(&b, "}\n\ntype %sOut struct {\n\tOK bool %s\n}\n", name, structTag("ok", false, false))
	out, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("routegen: format route: %w\n%s", err, b.String())
	}
	return out, nil
}

func structTag(jsonName string, omitempty, required bool) string {
	var b strings.Builder
	b.WriteByte('`')
	b.WriteString("json:\"")
	b.WriteString(jsonName)
	if omitempty {
		b.WriteString(",omitempty")
	}
	b.WriteByte('"')
	if required {
		b.WriteString(" route:\"required\"")
	}
	b.WriteByte('`')
	return b.String()
}

func renderTSX(tsImport, pattern, component string) string {
	return fmt.Sprintf("import { createFileRoute } from %s;\n\nexport const Route = createFileRoute(%s)({\n  component: function %s() {\n    return null;\n  },\n});\n",
		strconv.Quote(tsImport), strconv.Quote(pattern), component)
}

func renderRoot(tsImport string) string {
	return fmt.Sprintf("import { Outlet, createRootRoute } from %s;\n\nexport const Route = createRootRoute({\n  component: function RootLayout() {\n    return <Outlet />;\n  },\n});\n",
		strconv.Quote(tsImport))
}

func renderMain(libPath, routesPath, routesPkg string) (string, error) {
	libName := routeQualifier(libPath)
	alias := routesPkg
	if alias == libName {
		alias = "approutes"
	}
	var b strings.Builder
	b.WriteString("package main\n\nimport (\n\t\"log\"\n\t\"net/http\"\n\n")
	fmt.Fprintf(&b, "\t%s\n", strconv.Quote(libPath))
	if alias == routesPkg {
		fmt.Fprintf(&b, "\t%s\n", strconv.Quote(routesPath))
	} else {
		fmt.Fprintf(&b, "\t%s %s\n", alias, strconv.Quote(routesPath))
	}
	fmt.Fprintf(&b, ")\n\nfunc main() {\n\tmux := %s.NewMux()\n\tif err := %s.RegisterAll(mux); err != nil {\n\t\tlog.Fatal(err)\n\t}\n\tsrv := &%s.Server{Mux: mux}\n\tlog.Fatal(http.ListenAndServe(\":8080\", srv))\n}\n",
		libName, alias, libName)
	out, err := format.Source([]byte(b.String()))
	if err != nil {
		return "", fmt.Errorf("routegen: format server: %w", err)
	}
	return string(out), nil
}

func renderGoMod(module, lib, replace string) (string, error) {
	if err := validModulePath(module); err != nil {
		return "", err
	}
	if err := validModulePath(lib); err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "module %s\n\ngo 1.23.0\n\nrequire %s v0.0.0\n", module, lib)
	if replace != "" {
		abs, err := filepath.Abs(replace)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return "", fmt.Errorf("routegen: replace path: %w", err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("routegen: replace path %s is not a directory", abs)
		}
		fmt.Fprintf(&b, "\nreplace %s => %s\n", lib, goModPath(abs))
	}
	return b.String(), nil
}

func goModPath(p string) string {
	if strings.ContainsAny(p, " \\") {
		return strconv.Quote(p)
	}
	return filepath.ToSlash(p)
}

func renderPackageJSON(name, tsImport string) string {
	dep := tsImport
	if dep == "" || strings.HasPrefix(dep, ".") || strings.HasPrefix(dep, "/") {
		dep = defaultTSImport
	}
	return fmt.Sprintf(`{
  "name": %s,
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "preview": "vite preview"
  },
  "dependencies": {
    %s: "0.0.0",
    "react": "^18.3.1",
    "react-dom": "^18.3.1"
  },
  "devDependencies": {
    "@vitejs/plugin-react": "^4.3.4",
    "vite": "^6.0.0"
  }
}
`, strconv.Quote(name), strconv.Quote(dep))
}

func renderEntry(tsImport, tree string) string {
	return fmt.Sprintf(`import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { createRouter, RouterProvider } from %s;
import { routeTree } from %s;

const router = createRouter({ routeTree });

const root = document.getElementById("root");
if (root) {
  createRoot(root).render(
    <StrictMode>
      <RouterProvider router={router} />
    </StrictMode>,
  );
}
`, strconv.Quote(tsImport), strconv.Quote(tree))
}

const indexHTML = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>App</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
`

const viteConfig = `import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
});
`

const gitignore = `node_modules/
dist/
`

func cleanRoutesRel(dir string) (string, error) {
	if dir == "" {
		return "routes", nil
	}
	if filepath.IsAbs(dir) {
		return "", fmt.Errorf("routegen: init -dir must be a relative path")
	}
	dir = path.Clean(filepath.ToSlash(dir))
	if dir == "." || strings.HasPrefix(dir, "../") || dir == ".." || strings.Contains(dir, "$") {
		return "", fmt.Errorf("routegen: invalid routes directory %q", dir)
	}
	return dir, nil
}

func defaultModule(dir string) string {
	if dir == "" || dir == "." {
		return "example.com/app"
	}
	base := sanitizeModule(filepath.Base(dir))
	if base == "" || base == "." {
		return "example.com/app"
	}
	return "example.com/" + base
}

func npmName(dir string) string {
	if dir == "" || dir == "." {
		return "app"
	}
	name := sanitizeModule(filepath.Base(dir))
	if name == "" {
		return "app"
	}
	return name
}

func sanitizeModule(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	dash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if !ok {
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
			continue
		}
		dash = r == '-'
		b.WriteRune(r)
	}
	return strings.Trim(b.String(), "-.")
}

func validModulePath(p string) error {
	if p == "" || strings.ContainsAny(p, " \t$\\\"'") || strings.Contains(p, "..") || strings.Contains(p, "//") {
		return fmt.Errorf("routegen: invalid module path %q", p)
	}
	if strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/") {
		return fmt.Errorf("routegen: invalid module path %q", p)
	}
	return nil
}

func already(path string) error {
	_, err := os.Stat(path)
	if err == nil {
		return fmt.Errorf("routegen: %s already exists", path)
	}
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func writeNew(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func slashRel(base, abs string) string {
	rel, err := filepath.Rel(base, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}
