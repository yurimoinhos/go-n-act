// Package routegen is the gnact module that writes generated route files.
package routegen

import (
	"context"
	"flag"
	"path/filepath"
	"strings"

	"github.com/yurimoinhos/go-n-act/cli"
	gen "github.com/yurimoinhos/go-n-act/routegen"
)

const usage = `gnact routegen [generate flags]
gnact routegen generate [flags]

generate flags:
  -dir string
        routes directory (default "routes")
  -actions string
        optional actions directory (default "actions"; skipped when missing)
  -openapi
        also write openapi.json
  -check
        fail when generated files differ
  -import_path string
        Go import path of the route module (default "github.com/yurimoinhos/go-n-act")
  -ts_import string
        optional npm TypeScript specifier; empty embeds routes/gnact/ from this gnact binary
`

func init() {
	cli.Register(cli.Module{
		Name:    "routegen",
		Summary: "write the typed client, Go registers, and the route tree",
		Usage:   usage,
		Run:     run,
	})
}

func run(args []string) error {
	if len(args) > 0 && args[0] != "generate" && !strings.HasPrefix(args[0], "-") {
		return cli.UsageError{Msg: "gnact: routegen: unknown command " + args[0]}
	}
	if len(args) > 0 && args[0] == "generate" {
		args = args[1:]
	}
	var dir, actions, importPath, tsImport string
	var check, openapi bool
	fs, err := cli.Parse("gnact routegen", usage, args, func(fs *flag.FlagSet) {
		fs.StringVar(&dir, "dir", "routes", "routes directory")
		fs.StringVar(&actions, "actions", "actions", "optional actions directory")
		fs.BoolVar(&openapi, "openapi", false, "also write openapi.json")
		fs.BoolVar(&check, "check", false, "fail when generated files differ")
		fs.StringVar(&importPath, "import_path", cli.DefaultImport, "Go import path of the route module")
		fs.StringVar(&tsImport, "ts_import", cli.DefaultTS, "optional npm TS specifier; empty embeds routes/gnact/")
	})
	if err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return cli.UsageError{Msg: "gnact: routegen: unexpected arguments"}
	}
	actionsDir := actions
	if actionsDir != "" && !filepath.IsAbs(actionsDir) {
		actionsDir = filepath.Join(filepath.Dir(dir), actionsDir)
		if filepath.Dir(dir) == "." {
			actionsDir = actions
		}
	}
	files, err := gen.Generate(context.Background(), gen.Options{
		Dir:        dir,
		ActionsDir: actionsDir,
		ImportPath: importPath,
		TSImport:   tsImport,
		OpenAPI:    openapi,
	})
	if err != nil {
		return err
	}
	return gen.Write(dir, files, check)
}
