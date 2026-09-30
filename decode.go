package route

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

func decodeRequest(dst reflect.Value, body []byte, pathParams map[string]string, query url.Values, hasBody bool) error {
	if dst.Kind() != reflect.Pointer || dst.IsNil() {
		return NewError(CodeInternal, "internal error")
	}
	elemType := dst.Type().Elem()
	if elemType.Kind() == reflect.Pointer {
		elemType = elemType.Elem()
	}
	if elemType.Kind() != reflect.Struct {
		return NewError(CodeInternal, "internal error")
	}

	var raw map[string]json.RawMessage
	if hasBody {
		var err error
		var payload []byte
		raw, payload, err = parseObject(body)
		if err != nil {
			return err
		}
		for _, name := range serverFields(elemType) {
			if _, ok := raw[name]; ok {
				return WrapError(CodeInvalidArgument, "invalid request", Errorf(CodeInvalidArgument, "server field %s", name))
			}
		}
		if err := decodeJSONBody(payload, dst, raw, elemType); err != nil {
			return err
		}
	} else if len(bytes.TrimSpace(body)) > 0 {
		return NewError(CodeInvalidArgument, "invalid request")
	}

	elem := dst.Elem()
	if elem.Kind() == reflect.Pointer {
		if elem.IsNil() {
			elem.Set(reflect.New(elem.Type().Elem()))
		}
		elem = elem.Elem()
	}
	if err := applyPathParams(elem, pathParams); err != nil {
		return err
	}
	if err := applyQueryParams(elem, query); err != nil {
		return err
	}
	if err := requireBoundFields(elem, raw, pathParams, query, hasBody); err != nil {
		return err
	}
	return nil
}

func decodeJSONBody(body []byte, dst reflect.Value, raw map[string]json.RawMessage, elemType reflect.Type) error {
	pathQueryNames := map[string]bool{}
	walkBindFields(elemType, func(f reflect.StructField, bind fieldBind) {
		if bind.path != "" {
			pathQueryNames[bind.path] = true
			if bind.jsonName != "" {
				pathQueryNames[bind.jsonName] = true
			}
		}
		if bind.query != "" {
			pathQueryNames[bind.query] = true
			if bind.jsonName != "" {
				pathQueryNames[bind.jsonName] = true
			}
		}
	})
	if len(pathQueryNames) == 0 {
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(dst.Interface()); err != nil {
			return WrapError(CodeInvalidArgument, "invalid request", err)
		}
		return nil
	}

	filtered := map[string]json.RawMessage{}
	jsonNames := map[string]bool{}
	walkBindFields(elemType, func(f reflect.StructField, bind fieldBind) {
		if bind.path != "" || bind.query != "" {
			return
		}
		if bind.jsonName != "" {
			jsonNames[bind.jsonName] = true
		}
	})
	for k, v := range raw {
		if !jsonNames[k] {
			return WrapError(CodeInvalidArgument, "invalid request", Errorf(CodeInvalidArgument, "unknown field %s", k))
		}
		filtered[k] = v
	}
	enc, err := json.Marshal(filtered)
	if err != nil {
		return WrapError(CodeInvalidArgument, "invalid request", err)
	}
	dec := json.NewDecoder(bytes.NewReader(enc))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst.Interface()); err != nil {
		return WrapError(CodeInvalidArgument, "invalid request", err)
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
	walkBindFields(t, func(f reflect.StructField, bind fieldBind) {
		if bind.server && bind.jsonName != "" {
			names = append(names, bind.jsonName)
		}
	})
	return names
}

func requireBoundFields(v reflect.Value, raw map[string]json.RawMessage, pathParams map[string]string, query url.Values, hasBody bool) error {
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
	walkBindFields(v.Type(), func(f reflect.StructField, bind fieldBind) {
		if missing != "" || !bind.required {
			return
		}
		switch {
		case bind.path != "":
			if _, ok := pathParams[bind.path]; !ok {
				missing = bind.path
			}
		case bind.query != "":
			if _, ok := query[bind.query]; !ok {
				missing = bind.query
			}
		case bind.jsonName != "":
			if !hasBody {
				missing = bind.jsonName
				return
			}
			if _, ok := raw[bind.jsonName]; !ok {
				missing = bind.jsonName
			}
		}
	})
	if missing != "" {
		return Errorf(CodeInvalidArgument, "missing field %s", missing)
	}
	return nil
}

type fieldBind struct {
	server   bool
	required bool
	skip     bool
	jsonName string
	path     string
	query    string
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

func fieldBinding(f reflect.StructField) fieldBind {
	rt := routeTag(f)
	bind := fieldBind{server: rt.server, required: rt.required, skip: rt.skip}
	if bind.skip {
		return bind
	}
	if path := strings.TrimSpace(f.Tag.Get("path")); path != "" && path != "-" {
		bind.path = path
	}
	if q := strings.TrimSpace(f.Tag.Get("query")); q != "" && q != "-" {
		bind.query = q
	}
	if bind.path == "" && bind.query == "" {
		if name, ok := jsonFieldName(f); ok {
			bind.jsonName = name
		}
	}
	return bind
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
	if f.Tag.Get("path") != "" || f.Tag.Get("query") != "" {
		return false
	}
	tag := f.Tag.Get("json")
	if tag == "" || strings.HasPrefix(tag, ",") {
		return true
	}
	return false
}

func walkBindFields(t reflect.Type, fn func(reflect.StructField, fieldBind)) {
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
		bind := fieldBinding(f)
		if bind.skip {
			continue
		}
		if promotes(f) {
			walkBindFields(f.Type, fn)
			continue
		}
		fn(f, bind)
	}
}

func applyPathParams(v reflect.Value, params map[string]string) error {
	if len(params) == 0 {
		return nil
	}
	var first error
	walkBindFields(v.Type(), func(f reflect.StructField, bind fieldBind) {
		if first != nil || bind.path == "" {
			return
		}
		raw, ok := params[bind.path]
		if !ok {
			return
		}
		field := v.FieldByName(f.Name)
		if !field.IsValid() || !field.CanSet() {
			return
		}
		if err := setScalar(field, raw); err != nil {
			first = WrapError(CodeInvalidArgument, "invalid request", err)
		}
	})
	return first
}

func applyQueryParams(v reflect.Value, query url.Values) error {
	if len(query) == 0 {
		return nil
	}
	var first error
	walkBindFields(v.Type(), func(f reflect.StructField, bind fieldBind) {
		if first != nil || bind.query == "" {
			return
		}
		vals, ok := query[bind.query]
		if !ok || len(vals) == 0 {
			return
		}
		field := v.FieldByName(f.Name)
		if !field.IsValid() || !field.CanSet() {
			return
		}
		if field.Kind() == reflect.Slice {
			slice := reflect.MakeSlice(field.Type(), 0, len(vals))
			for _, raw := range vals {
				elem := reflect.New(field.Type().Elem()).Elem()
				if err := setScalar(elem, raw); err != nil {
					first = WrapError(CodeInvalidArgument, "invalid request", err)
					return
				}
				slice = reflect.Append(slice, elem)
			}
			field.Set(slice)
			return
		}
		if err := setScalar(field, vals[0]); err != nil {
			first = WrapError(CodeInvalidArgument, "invalid request", err)
		}
	})
	return first
}

func setScalar(field reflect.Value, raw string) error {
	if field.Kind() == reflect.Pointer {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		return setScalar(field.Elem(), raw)
	}
	switch field.Kind() {
	case reflect.String:
		field.SetString(raw)
		return nil
	case reflect.Bool:
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		field.SetBool(v)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := strconv.ParseInt(raw, 10, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetInt(v)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v, err := strconv.ParseUint(raw, 10, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetUint(v)
		return nil
	case reflect.Float32, reflect.Float64:
		v, err := strconv.ParseFloat(raw, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetFloat(v)
		return nil
	default:
		return fmt.Errorf("unsupported path/query type %s", field.Type())
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
