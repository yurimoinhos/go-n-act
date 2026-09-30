package routegen

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// goHandler is one exported route function.
type goHandler struct {
	Name    string
	Doc     string
	In      types.Type
	Out     types.Type
	Service string
	RouteID string
	File    string
}

type loaded struct {
	module string
	byFile map[string][]goHandler
	byDir  map[string]*packages.Package
}

func loadRoutes(ctx context.Context, modRoot, routesDir string) (*loaded, error) {
	rel, err := filepath.Rel(modRoot, routesDir)
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return nil, fmt.Errorf("routegen: routes directory %s is outside module %s", routesDir, modRoot)
	}
	pattern := "./..."
	if rel != "." {
		pattern = "./" + rel + "/..."
	}
	cfg := &packages.Config{
		Context: ctx,
		Dir:     modRoot,
		Env:     os.Environ(),
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedImports | packages.NeedDeps | packages.NeedModule,
		ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
			return parser.ParseFile(fset, filename, src, parser.ParseComments|parser.SkipObjectResolution)
		},
	}
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		return nil, fmt.Errorf("routegen: load routes: %w", err)
	}
	if err := packageErrors(pkgs); err != nil {
		return nil, err
	}
	out := &loaded{
		byFile: map[string][]goHandler{},
		byDir:  map[string]*packages.Package{},
	}
	for _, pkg := range pkgs {
		if pkg.Module != nil && out.module == "" {
			out.module = pkg.Module.Path
		}
		files := pkg.CompiledGoFiles
		if len(files) == 0 {
			files = pkg.GoFiles
		}
		for i, file := range pkg.Syntax {
			if i >= len(files) {
				break
			}
			filename := filepath.Clean(files[i])
			out.byDir[filepath.Dir(filename)] = pkg
			if strings.HasSuffix(filename, ".gen.go") || strings.HasSuffix(filename, "_test.go") {
				continue
			}
			found := handlersInFile(pkg, file, filename)
			if len(found) > 0 {
				out.byFile[filename] = found
			}
		}
	}
	return out, nil
}

func packageErrors(pkgs []*packages.Package) error {
	var b strings.Builder
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		for _, err := range pkg.Errors {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(err.Error())
		}
	})
	if b.Len() == 0 {
		return nil
	}
	return fmt.Errorf("routegen: load routes: %s", b.String())
}

func handlersInFile(pkg *packages.Package, file *ast.File, filename string) []goHandler {
	if pkg.TypesInfo == nil {
		return nil
	}
	var found []goHandler
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Recv != nil || fd.Name == nil || !fd.Name.IsExported() {
			continue
		}
		obj := pkg.TypesInfo.Defs[fd.Name]
		fn, ok := obj.(*types.Func)
		if !ok || fn.Pkg() == nil {
			continue
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok {
			continue
		}
		in, out, ok := handlerSig(sig)
		if !ok {
			continue
		}
		doc := ""
		if fd.Doc != nil {
			doc = fd.Doc.Text()
		}
		found = append(found, goHandler{
			Name: fn.Name(),
			Doc:  doc,
			In:   in,
			Out:  out,
			File: filename,
		})
	}
	return found
}

func handlerSig(sig *types.Signature) (in, out types.Type, ok bool) {
	if sig.Recv() != nil || sig.Variadic() {
		return nil, nil, false
	}
	params := sig.Params()
	if params == nil || params.Len() < 1 || params.Len() > 2 || !isContext(params.At(0).Type()) {
		return nil, nil, false
	}
	if params.Len() == 2 {
		in = params.At(1).Type()
		if !isStructish(in) {
			return nil, nil, false
		}
	}
	results := sig.Results()
	if results == nil {
		return nil, nil, false
	}
	switch results.Len() {
	case 1:
		if !isErrorType(results.At(0).Type()) {
			return nil, nil, false
		}
	case 2:
		out = results.At(0).Type()
		if !isErrorType(results.At(1).Type()) || !isStructish(out) {
			return nil, nil, false
		}
	default:
		return nil, nil, false
	}
	return in, out, true
}

func isContext(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Name() == "Context" && named.Obj().Pkg().Path() == "context"
}

func isErrorType(t types.Type) bool {
	errObj := types.Universe.Lookup("error")
	return errObj != nil && types.Identical(t, errObj.Type())
}

func isStructish(t types.Type) bool {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	if n, ok := t.(*types.Named); ok {
		t = types.Unalias(n.Underlying())
	}
	_, ok := t.(*types.Struct)
	return ok
}
