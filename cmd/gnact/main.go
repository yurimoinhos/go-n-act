// Command gnact is the route CLI. Each module is a subcommand.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/yurimoinhos/go-n-act/cli"
	_ "github.com/yurimoinhos/go-n-act/cli/routegen"
	_ "github.com/yurimoinhos/go-n-act/cli/template"
	_ "github.com/yurimoinhos/go-n-act/cli/test"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		if errors.Is(err, cli.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, err.Error())
		var u cli.UsageError
		if errors.As(err, &u) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
