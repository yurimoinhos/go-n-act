// Package routegen is the gnact module that writes generated route files.
package routegen

import (
	"context"
	"flag"
	"strings"

	"github.com/yurimoinhos/go-n-act/cli"
	gen "github.com/yurimoinhos/go-n-act/routegen"
)

const usage = `gnact routegen [generate flags]
gnact routegen generate [flags]

generate flags:
  -dir string
        routes directory (default "routes")
  -check
        fail when generated files differ
  -import_path string
        Go import path of the route module (default "github.com/yurimoinhos/go-n-act")
  -ts_import string
        TypeScript module specifier (default "@aggitech/route")
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
	var dir, importPath, tsImport string
	var check bool
	fs, err := cli.Parse("gnact routegen", usage, args, func(fs *flag.FlagSet) {
		fs.StringVar(&dir, "dir", "routes", "routes directory")
		fs.BoolVar(&check, "check", false, "fail when generated files differ")
		fs.StringVar(&importPath, "import_path", cli.DefaultImport, "Go import path of the route module")
		fs.StringVar(&tsImport, "ts_import", cli.DefaultTS, "TypeScript module specifier")
	})
	if err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return cli.UsageError{Msg: "gnact: routegen: unexpected arguments"}
	}
	files, err := gen.Generate(context.Background(), gen.Options{
		Dir:        dir,
		ImportPath: importPath,
		TSImport:   tsImport,
	})
	if err != nil {
		return err
	}
	return gen.Write(dir, files, check)
}
