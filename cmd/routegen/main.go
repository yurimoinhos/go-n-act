// Command routegen writes the typed route client, Go register files, and the route tree.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/yurimoinhos/go-n-act/routegen"
)

func main() {
	dir := flag.String("dir", "routes", "routes directory")
	check := flag.Bool("check", false, "fail when generated files differ")
	importPath := flag.String("import_path", "github.com/yurimoinhos/go-n-act", "Go import path of the route module")
	tsImport := flag.String("ts_import", "@aggitech/route", "TypeScript module specifier")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "routegen: unexpected arguments")
		os.Exit(2)
	}
	files, err := routegen.Generate(context.Background(), routegen.Options{
		Dir:        *dir,
		ImportPath: *importPath,
		TSImport:   *tsImport,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := routegen.Write(*dir, files, *check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
