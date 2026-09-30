// Package template is the gnact module that creates an app or one route.
package template

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/yurimoinhos/go-n-act/cli"
	"github.com/yurimoinhos/go-n-act/routegen"
)

const usage = `gnact template init [dir] [flags]
gnact template new <route> [flags]

init flags:
  -module string
        Go module path of the new app
  -template string
        app or routes (default "app")
  -dir string
        routes directory relative to the project (default "routes")
  -replace
        add a go.mod replace for this local module
  -ts_import string
        TypeScript module specifier (default "@aggitech/route")
  -import_path string
        Go import path of the route module (default "github.com/yurimoinhos/go-n-act")

new flags:
  -dir string
        routes directory (default "routes")
  -only string
        both, ui, or api (default "both")
  -style string
        css, scss, or sass
  -generate
        write the client, registers, and route tree (default true)
  -ts_import string
        TypeScript module specifier (default "@aggitech/route")
  -import_path string
        Go import path of the route module (default "github.com/yurimoinhos/go-n-act")
`

func init() {
	cli.Register(cli.Module{
		Name:    "template",
		Summary: "create an app or add a route",
		Usage:   usage,
		Run:     run,
	})
}

func run(args []string) error {
	if len(args) == 0 {
		return cli.UsageError{Msg: "gnact: template: expected init or new"}
	}
	if args[0] == "-h" || args[0] == "-help" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(os.Stdout, usage)
		return cli.ErrHelp
	}
	switch args[0] {
	case "init":
		return runInit(args[1:])
	case "new":
		return runNew(args[1:])
	default:
		if strings.HasPrefix(args[0], "-") {
			return cli.UsageError{Msg: "gnact: template: expected init or new"}
		}
		return cli.UsageError{Msg: "gnact: template: unknown command " + args[0]}
	}
}

func runInit(args []string) error {
	var module, kind, dir, tsImport, importPath string
	var replace bool
	fs, err := cli.Parse("gnact template init", usage, args, func(fs *flag.FlagSet) {
		fs.StringVar(&module, "module", "", "Go module path of the new app")
		fs.StringVar(&kind, "template", "app", "app or routes")
		fs.StringVar(&dir, "dir", "routes", "routes directory relative to the project")
		fs.BoolVar(&replace, "replace", false, "add a go.mod replace for this local module")
		fs.StringVar(&tsImport, "ts_import", cli.DefaultTS, "TypeScript module specifier")
		fs.StringVar(&importPath, "import_path", cli.DefaultImport, "Go import path of the route module")
	})
	if err != nil {
		return err
	}
	project := "."
	switch rest := fs.Args(); len(rest) {
	case 0:
	case 1:
		project = rest[0]
	default:
		return cli.UsageError{Msg: "gnact: template: unexpected arguments"}
	}
	if kind != "app" && kind != "routes" {
		return cli.UsageError{Msg: "gnact: template: -template must be app or routes"}
	}
	if replace && kind == "routes" {
		return cli.UsageError{Msg: "gnact: template: -replace applies to the app template"}
	}
	replacePath := ""
	if replace {
		replacePath, err = thisModule()
		if err != nil {
			return err
		}
	}
	paths, err := routegen.Init(context.Background(), routegen.InitOptions{
		Dir:        project,
		RoutesDir:  dir,
		Module:     module,
		Template:   kind,
		Replace:    replacePath,
		TSImport:   tsImport,
		ImportPath: importPath,
	})
	if err != nil {
		return err
	}
	for _, p := range paths {
		fmt.Fprintln(os.Stdout, p)
	}
	return nil
}

func runNew(args []string) error {
	var dir, only, style, tsImport, importPath string
	var generate bool
	fs, err := cli.Parse("gnact template new", usage, args, func(fs *flag.FlagSet) {
		fs.StringVar(&dir, "dir", "routes", "routes directory")
		fs.StringVar(&only, "only", "both", "both, ui, or api")
		fs.StringVar(&style, "style", "", "css, scss, or sass")
		fs.BoolVar(&generate, "generate", true, "write the client, registers, and route tree")
		fs.StringVar(&tsImport, "ts_import", cli.DefaultTS, "TypeScript module specifier")
		fs.StringVar(&importPath, "import_path", cli.DefaultImport, "Go import path of the route module")
	})
	if err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return cli.UsageError{Msg: "gnact: template: new expects one route"}
	}
	switch only {
	case "both", "ui", "api":
	default:
		return cli.UsageError{Msg: "gnact: template: -only must be both, ui, or api"}
	}
	switch style {
	case "", "css", "scss", "sass":
	default:
		return cli.UsageError{Msg: "gnact: template: -style must be css, scss, or sass"}
	}
	paths, err := routegen.Add(context.Background(), routegen.AddOptions{
		Dir:        dir,
		Route:      fs.Arg(0),
		Only:       only,
		Style:      style,
		Generate:   generate,
		TSImport:   tsImport,
		ImportPath: importPath,
	})
	if err != nil {
		return err
	}
	for _, p := range paths {
		fmt.Fprintln(os.Stdout, p)
	}
	return nil
}

func thisModule() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("gnact: template: cannot locate the route module")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("gnact: template: go.mod not found above the template module")
		}
		dir = parent
	}
}
