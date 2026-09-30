package routegen

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strconv"
	"text/template"
)

//go:embed templates/*
var scaffoldTemplates embed.FS

type handlerTmplData struct {
	Package string
	Name    string
	Method  string
	Path    string
	Fields  []handlerTmplField
}

type handlerTmplField struct {
	Name string
	Tag  string
}

type mainTmplData struct {
	LibImport    string
	RoutesImport string
	LibName      string
	RoutesAlias  string
}

type gomodTmplData struct {
	Module  string
	Lib     string
	Replace string
}

func parseScaffoldTemplates() (*template.Template, error) {
	return template.New("scaffold").ParseFS(scaffoldTemplates,
		"templates/handler.go.tmpl",
		"templates/main.go.tmpl",
		"templates/gomod.tmpl",
	)
}

func execTemplate(name string, data any) ([]byte, error) {
	tmpl, err := parseScaffoldTemplates()
	if err != nil {
		return nil, fmt.Errorf("routegen: load templates: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return nil, fmt.Errorf("routegen: execute %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

func renderHandler(pkg, name, method, restPath string, fields []inputField) ([]byte, error) {
	data := handlerTmplData{
		Package: pkg,
		Name:    name,
		Method:  method,
		Path:    restPath,
	}
	for _, field := range fields {
		data.Fields = append(data.Fields, handlerTmplField{
			Name: field.Name,
			Tag:  structTag(field.JSON, field.Omit, field.Required, field.Path),
		})
	}
	raw, err := execTemplate("handler.go.tmpl", data)
	if err != nil {
		return nil, err
	}
	out, err := format.Source(raw)
	if err != nil {
		return nil, fmt.Errorf("routegen: format route: %w\n%s", err, raw)
	}
	return out, nil
}

func renderMain(libPath, routesPath, routesPkg string) (string, error) {
	libName := routeQualifier(libPath)
	alias := routesPkg
	if alias == libName {
		alias = "approutes"
	}
	libImport := strconv.Quote(libPath)
	routesImport := strconv.Quote(routesPath)
	if alias != routesPkg {
		routesImport = alias + " " + routesImport
	}
	raw, err := execTemplate("main.go.tmpl", mainTmplData{
		LibImport:    libImport,
		RoutesImport: routesImport,
		LibName:      libName,
		RoutesAlias:  alias,
	})
	if err != nil {
		return "", err
	}
	out, err := format.Source(raw)
	if err != nil {
		return "", fmt.Errorf("routegen: format server: %w\n%s", err, raw)
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
	replacePath := ""
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
		replacePath = goModPath(abs)
	}
	raw, err := execTemplate("gomod.tmpl", gomodTmplData{
		Module:  module,
		Lib:     lib,
		Replace: replacePath,
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
