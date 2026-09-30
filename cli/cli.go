// Package cli dispatches the gnact command line.
//
// A module registers itself from init. The gnact binary imports the modules
// it ships. Help, unknown modules, and flag errors stay in this package so
// every module exits the same way.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	// DefaultImport is the Go import path written into generated registers.
	DefaultImport = "github.com/yurimoinhos/go-n-act"
	// DefaultTS is empty: codegen embeds the runtime under routes/gnact/.
	// Pass an explicit npm specifier only when you intentionally override that.
	DefaultTS = ""
)

// ErrHelp means the process should exit 0 after usage was written to stdout.
var ErrHelp = errors.New("help")

// UsageError is a bad invocation. The process exits 2.
type UsageError struct{ Msg string }

func (e UsageError) Error() string { return e.Msg }

// Module is one gnact subcommand.
type Module struct {
	Name    string
	Summary string
	Usage   string
	Run     func(args []string) error
}

var (
	modules []Module
	byName  = map[string]Module{}
)

// Register adds a module. A duplicate name panics: registration happens at init.
func Register(m Module) {
	if m.Name == "" || m.Run == nil {
		panic("gnact: module is incomplete")
	}
	if _, ok := byName[m.Name]; ok {
		panic("gnact: duplicate module " + m.Name)
	}
	byName[m.Name] = m
	modules = append(modules, m)
}

// Run executes args, which do not include the program name.
func Run(args []string) error {
	if len(args) == 0 || isHelp(args[0]) && len(args) == 1 {
		fmt.Fprint(os.Stdout, globalUsage())
		return ErrHelp
	}
	if args[0] == "help" {
		if len(args) == 1 {
			fmt.Fprint(os.Stdout, globalUsage())
			return ErrHelp
		}
		if len(args) != 2 || isHelp(args[1]) {
			return UsageError{Msg: "gnact: help expects one module"}
		}
		m, ok := byName[args[1]]
		if !ok {
			return UsageError{Msg: "gnact: unknown module " + args[1]}
		}
		fmt.Fprint(os.Stdout, m.Usage)
		return ErrHelp
	}
	m, ok := byName[args[0]]
	if !ok {
		return UsageError{Msg: "gnact: unknown module " + args[0]}
	}
	return m.Run(args[1:])
}

func globalUsage() string {
	var b strings.Builder
	b.WriteString("gnact <module> [arguments]\n\nmodules:\n")
	for _, m := range modules {
		fmt.Fprintf(&b, "  %s\t%s\n", m.Name, m.Summary)
	}
	b.WriteString("\ngnact help <module> writes that module's flags.\n")
	return b.String()
}

func isHelp(s string) bool {
	switch s {
	case "help", "-h", "-help", "--help":
		return true
	default:
		return false
	}
}

// Parse binds flags and parses args. Flags may follow positional arguments.
// -h writes usage to stdout and returns [ErrHelp].
func Parse(name, usage string, args []string, bind func(*flag.FlagSet)) (*flag.FlagSet, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	bind(fs)
	if err := fs.Parse(reorder(fs, args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(os.Stdout, usage)
			return nil, ErrHelp
		}
		return nil, UsageError{Msg: name + ": " + err.Error()}
	}
	return fs, nil
}

func reorder(fs *flag.FlagSet, args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		name, ok := flagName(a)
		if !ok {
			pos = append(pos, a)
			continue
		}
		flags = append(flags, a)
		if strings.Contains(a, "=") || !takesValue(fs, name) || i+1 >= len(args) {
			continue
		}
		i++
		flags = append(flags, args[i])
	}
	return append(flags, pos...)
}

func flagName(a string) (string, bool) {
	if a == "-" || !strings.HasPrefix(a, "-") {
		return "", false
	}
	a = strings.TrimLeft(a, "-")
	if a == "" {
		return "", false
	}
	if i := strings.IndexByte(a, '='); i >= 0 {
		a = a[:i]
	}
	return a, true
}

func takesValue(fs *flag.FlagSet, name string) bool {
	f := fs.Lookup(name)
	if f == nil {
		return false
	}
	type boolFlag interface{ IsBoolFlag() bool }
	bf, ok := f.Value.(boolFlag)
	return !ok || !bf.IsBoolFlag()
}
