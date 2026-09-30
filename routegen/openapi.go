package routegen

import (
	"encoding/json"
	"fmt"
	"go/types"
	"sort"
	"strings"
)

func renderOpenAPI(ends []endpoint) ([]byte, error) {
	paths := map[string]map[string]any{}
	for _, end := range ends {
		pathItem := paths[end.handler.Path]
		if pathItem == nil {
			pathItem = map[string]any{}
			paths[end.handler.Path] = pathItem
		}
		op := map[string]any{
			"operationId": end.handler.Name,
			"responses": map[string]any{
				"200": map[string]any{
					"description": "OK",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": schemaFromType(end.handler.Out),
						},
					},
				},
				"default": map[string]any{
					"description": "Error",
					"content": map[string]any{
						"application/problem+json": map[string]any{
							"schema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"type":     map[string]any{"type": "string"},
									"title":    map[string]any{"type": "string"},
									"status":   map[string]any{"type": "integer"},
									"detail":   map[string]any{"type": "string"},
									"instance": map[string]any{"type": "string"},
									"code":     map[string]any{"type": "string"},
								},
								"required": []string{"type", "title", "status", "detail", "code"},
							},
						},
					},
				},
			},
		}
		if doc := firstSentence(end.handler.Doc); doc != "" {
			op["summary"] = doc
		}
		params, bodySchema := openAPIParams(end.handler.In)
		if len(params) > 0 {
			op["parameters"] = params
		}
		method := strings.ToLower(end.handler.Method)
		if method == "post" || method == "put" || method == "patch" {
			if bodySchema != nil {
				op["requestBody"] = map[string]any{
					"required": true,
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": bodySchema,
						},
					},
				}
			}
		}
		pathItem[method] = op
	}
	pathKeys := make([]string, 0, len(paths))
	for p := range paths {
		pathKeys = append(pathKeys, p)
	}
	sort.Strings(pathKeys)
	ordered := map[string]any{}
	for _, p := range pathKeys {
		ordered[p] = paths[p]
	}
	doc := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "gnact",
			"version": "0.0.0",
		},
		"paths": ordered,
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("routegen: openapi: %w", err)
	}
	out = append(out, '\n')
	return out, nil
}

func openAPIParams(t types.Type) (params []any, body map[string]any) {
	if t == nil {
		return nil, nil
	}
	st, err := structType(t)
	if err != nil {
		return nil, map[string]any{"type": "object"}
	}
	bodyProps := map[string]any{}
	var bodyRequired []string
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		tag := st.Tag(i)
		if !f.Exported() || jsonIgnored(tag) || routeSkipped(tag) || routeServer(tag) {
			continue
		}
		pathName := tagValue(tag, "path")
		queryName := tagValue(tag, "query")
		schema := schemaFromType(f.Type())
		switch {
		case pathName != "" && pathName != "-":
			params = append(params, map[string]any{
				"name":     pathName,
				"in":       "path",
				"required": true,
				"schema":   schema,
			})
		case queryName != "" && queryName != "-":
			params = append(params, map[string]any{
				"name":     queryName,
				"in":       "query",
				"required": routeRequired(tag),
				"schema":   schema,
			})
		default:
			name := jsonExplicit(tag)
			if name == "" {
				name = f.Name()
			}
			bodyProps[name] = schema
			if routeRequired(tag) {
				bodyRequired = append(bodyRequired, name)
			}
		}
	}
	if len(bodyProps) > 0 {
		body = map[string]any{
			"type":       "object",
			"properties": bodyProps,
		}
		if len(bodyRequired) > 0 {
			body["required"] = bodyRequired
		}
	}
	return params, body
}

func schemaFromType(t types.Type) map[string]any {
	if t == nil {
		return map[string]any{"type": "object"}
	}
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		return schemaFromType(p.Elem())
	}
	switch t := t.(type) {
	case *types.Named:
		if obj := t.Obj(); obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "time" && obj.Name() == "Time" {
			return map[string]any{"type": "string", "format": "date-time"}
		}
		return schemaFromType(t.Underlying())
	case *types.Basic:
		switch t.Kind() {
		case types.Bool:
			return map[string]any{"type": "boolean"}
		case types.String:
			return map[string]any{"type": "string"}
		case types.Float32, types.Float64:
			return map[string]any{"type": "number"}
		default:
			return map[string]any{"type": "integer"}
		}
	case *types.Slice:
		if isByteSlice(t) {
			return map[string]any{"type": "string", "format": "byte"}
		}
		return map[string]any{"type": "array", "items": schemaFromType(t.Elem())}
	case *types.Map:
		return map[string]any{"type": "object", "additionalProperties": schemaFromType(t.Elem())}
	case *types.Struct:
		props := map[string]any{}
		var required []string
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			tag := t.Tag(i)
			if !f.Exported() || jsonIgnored(tag) || routeSkipped(tag) {
				continue
			}
			name := jsonExplicit(tag)
			if name == "" {
				name = f.Name()
			}
			props[name] = schemaFromType(f.Type())
			if routeRequired(tag) {
				required = append(required, name)
			}
		}
		out := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			out["required"] = required
		}
		return out
	default:
		return map[string]any{"type": "object"}
	}
}
