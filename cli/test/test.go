// Package test is the gnact module that checks generated files and runs go test.
package test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/yurimoinhos/go-n-act/cli"
	"github.com/yurimoinhos/go-n-act/routegen"
)

const usage = `gnact test [flags]

  -dir string
        routes directory (default "routes")
  -import_path string
        Go import path of the route module (default "github.com/yurimoinhos/go-n-act")
  -ts_import string
        TypeScript module specifier (default "@aggitech/route")
`

func init() {
	cli.Register(cli.Module{
		Name:    "test",
		Summary: "check generated files and run go test",
		Usage:   usage,
		Run:     run,
	})
}

func run(args []string) error {
	var dir, importPath, tsImport string
	fs, err := cli.Parse("gnact test", usage, args, func(fs *flag.FlagSet) {
		fs.StringVar(&dir, "dir", "routes", "routes directory")
		fs.StringVar(&importPath, "import_path", cli.DefaultImport, "Go import path of the route module")
		fs.StringVar(&tsImport, "ts_import", cli.DefaultTS, "TypeScript module specifier")
	})
	if err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return cli.UsageError{Msg: "gnact: test: unexpected arguments"}
	}
	files, err := routegen.Generate(context.Background(), routegen.Options{
		Dir:        dir,
		ImportPath: importPath,
		TSImport:   tsImport,
	})
	if err != nil {
		return err
	}
	if err := routegen.Write(dir, files, true); err != nil {
		return err
	}
	mod, err := moduleRoot(dir)
	if err != nil {
		return err
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = mod
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("gnact: test: go test failed")
		}
		return fmt.Errorf("gnact: test: %w", err)
	}
	return nil
}

func moduleRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		info, err := os.Stat(filepath.Join(dir, "go.mod"))
		if err == nil && !info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("gnact: test: go.mod not found above %s", start)
		}
		dir = parent
	}
}
