package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yurimoinhos/go-n-act/cli"
)

func TestRunUsage(t *testing.T) {
	err := cli.Run([]string{"nope"})
	var u cli.UsageError
	if !errors.As(err, &u) || !strings.Contains(err.Error(), "unknown module") {
		t.Fatalf("run(nope) = %v", err)
	}
	err = cli.Run([]string{"routegen", "generate", "extra"})
	if !errors.As(err, &u) || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("run(routegen generate extra) = %v", err)
	}
	err = cli.Run([]string{"template", "new"})
	if !errors.As(err, &u) || !strings.Contains(err.Error(), "one route") {
		t.Fatalf("run(template new) = %v", err)
	}
	err = cli.Run([]string{"template"})
	if !errors.As(err, &u) || !strings.Contains(err.Error(), "expected init or new") {
		t.Fatalf("run(template) = %v", err)
	}
}

func TestRunHelp(t *testing.T) {
	out := captureStdout(t, func() {
		err := cli.Run([]string{"-h"})
		if !errors.Is(err, cli.ErrHelp) {
			t.Fatalf("run(-h) = %v", err)
		}
	})
	for _, name := range []string{"routegen", "template", "test"} {
		if !strings.Contains(out, name) {
			t.Fatalf("help = %s", out)
		}
	}
	out = captureStdout(t, func() {
		err := cli.Run([]string{"help", "template"})
		if !errors.Is(err, cli.ErrHelp) {
			t.Fatalf("help template = %v", err)
		}
	})
	if !strings.Contains(out, "gnact template init") || !strings.Contains(out, "gnact template new") {
		t.Fatalf("template help = %s", out)
	}
}

func TestRunRoutegenNoArgsGenerates(t *testing.T) {
	err := cli.Run([]string{"routegen"})
	var u cli.UsageError
	if err == nil || errors.As(err, &u) || errors.Is(err, cli.ErrHelp) {
		t.Fatalf("run(routegen) = %v", err)
	}
}

func TestRunNewWritesRoute(t *testing.T) {
	dir := t.TempDir()
	routes := filepath.Join(dir, "routes")
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	out := captureStdout(t, func() {
		err = cli.Run([]string{"template", "new", "clients/$id", "-generate=false"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "clients/$id.tsx") || !strings.Contains(out, "clients/param_id.go") {
		t.Fatalf("stdout = %s", out)
	}
	if _, err := os.Stat(filepath.Join(routes, "clients", "param_id.go")); err != nil {
		t.Fatal(err)
	}
}

func TestRunTestChecksAndRunsGoTest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.23.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app_test.go"), []byte("package app\n\nimport \"testing\"\n\nfunc TestOK(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "routes"), 0o755); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := cli.Run([]string{"test"}); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "fail_test.go"), []byte("package app\n\nimport \"testing\"\n\nfunc TestFail(t *testing.T) { t.Fatal(\"nope\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = cli.Run([]string{"test"})
	if err == nil || !strings.Contains(err.Error(), "go test failed") {
		t.Fatalf("failing test = %v", err)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = old })
	fn()
	_ = w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
