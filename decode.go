package route

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
)

func decodeInput(body []byte, dst reflect.Value, raw map[string]json.RawMessage) error {
	if dst.Kind() != reflect.Pointer || dst.IsNil() {
		return NewError(CodeInternal, "internal error")
	}
	for _, name := range serverFields(dst.Type().Elem()) {
		if _, ok := raw[name]; ok {
			return WrapError(CodeInvalidArgument, "invalid request", Errorf(CodeInvalidArgument, "server field %s", name))
		}
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst.Interface()); err != nil {
		return WrapError(CodeInvalidArgument, "invalid request", err)
	}
	if err := requireFields(dst.Elem(), raw); err != nil {
		return err
	}
	return nil
}

func parseObject(body []byte) (map[string]json.RawMessage, []byte, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return map[string]json.RawMessage{}, []byte("{}"), nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil || raw == nil {
		return nil, nil, WrapError(CodeInvalidArgument, "invalid request", err)
	}
	return raw, trimmed, nil
}

func serverFields(t reflect.Type) []string {
	var names []string
	walkFields(t, func(f reflect.StructField, jsonName string) {
		if routeTag(f).server {
			names = append(names, jsonName)
		}
	})
	return names
}

func requireFields(v reflect.Value, raw map[string]json.RawMessage) error {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	var missing string
	walkFields(v.Type(), func(f reflect.StructField, jsonName string) {
		if missing != "" || !routeTag(f).required {
			return
		}
		if _, ok := raw[jsonName]; !ok {
			missing = jsonName
		}
	})
	if missing != "" {
		return Errorf(CodeInvalidArgument, "missing field %s", missing)
	}
	return nil
}

type fieldTag struct {
	server   bool
	required bool
	skip     bool
}

func routeTag(f reflect.StructField) fieldTag {
	var t fieldTag
	raw := f.Tag.Get("route")
	if raw == "-" {
		t.skip = true
		return t
	}
	for _, part := range strings.Split(raw, ",") {
		switch strings.TrimSpace(part) {
		case "server":
			t.server = true
		case "required":
			t.required = true
		case "-":
			t.skip = true
		}
	}
	return t
}

func jsonFieldName(f reflect.StructField) (string, bool) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", false
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		name = f.Name
	}
	if name == "-" {
		return "", false
	}
	return name, true
}

func promotes(f reflect.StructField) bool {
	if !f.Anonymous || routeTag(f).skip {
		return false
	}
	tag := f.Tag.Get("json")
	if tag == "" || strings.HasPrefix(tag, ",") {
		return true
	}
	return false
}

func walkFields(t reflect.Type, fn func(reflect.StructField, string)) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" && !f.Anonymous {
			continue
		}
		if routeTag(f).skip {
			continue
		}
		if promotes(f) {
			walkFields(f.Type, fn)
			continue
		}
		name, ok := jsonFieldName(f)
		if !ok {
			continue
		}
		fn(f, name)
	}
}

func normalizeEmpty(v reflect.Value) {
	if !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			normalizeEmpty(v.Elem())
		}
	case reflect.Interface:
		if !v.IsNil() {
			normalizeEmpty(v.Elem())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			if f.CanSet() {
				normalizeEmpty(f)
			}
		}
	case reflect.Slice:
		if v.IsNil() && v.CanSet() {
			v.Set(reflect.MakeSlice(v.Type(), 0, 0))
		}
		for i := 0; i < v.Len(); i++ {
			normalizeEmpty(v.Index(i))
		}
	case reflect.Map:
		if v.IsNil() && v.CanSet() {
			v.Set(reflect.MakeMap(v.Type()))
		}
		for _, key := range v.MapKeys() {
			normalizeEmpty(v.MapIndex(key))
		}
	}
}
