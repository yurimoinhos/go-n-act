package routegen

import (
	"fmt"
	"go/constant"
	"go/types"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

var identRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type tsField struct {
	JSON     string
	Type     string
	Optional bool
	Doc      string
}

type tsDecl struct {
	Name   string
	Kind   string // iface or alias
	Alias  string
	Fields []tsField
	Doc    string
}

type handlerView struct {
	Name    string
	Doc     string
	InName  string
	OutName string
	HasIn   bool
	HasOut  bool
	Service string
	RouteID string
}

type tsFile struct {
	Decls    []*tsDecl
	Handlers []handlerView
}

type tsBuilder struct {
	decls []*tsDecl
	index map[string]*tsDecl
	seen  map[string]string
	canon map[string]string // pkg.Name -> ts name when the shape is the full struct
}

func newTSBuilder() *tsBuilder {
	return &tsBuilder{index: map[string]*tsDecl{}, seen: map[string]string{}, canon: map[string]string{}}
}

func (b *tsBuilder) file(handlers []goHandler) (tsFile, error) {
	var views []handlerView
	for _, h := range handlers {
		view := handlerView{
			Name:    h.Name,
			Doc:     firstSentence(h.Doc),
			Service: h.Service,
			RouteID: h.RouteID,
		}
		if h.In != nil {
			in := h.In
			if p, ok := in.(*types.Pointer); ok {
				in = p.Elem()
			}
			name, err := b.typeString(in, "in")
			if err != nil {
				return tsFile{}, fmt.Errorf("%s input: %w", h.Name, err)
			}
			view.HasIn = true
			view.InName = name
		}
		if h.Out != nil {
			name, err := b.typeString(h.Out, "out")
			if err != nil {
				return tsFile{}, fmt.Errorf("%s output: %w", h.Name, err)
			}
			view.HasOut = true
			view.OutName = name
		}
		views = append(views, view)
	}
	sort.Slice(b.decls, func(i, j int) bool { return b.decls[i].Name < b.decls[j].Name })
	return tsFile{Decls: b.decls, Handlers: views}, nil
}

func (b *tsBuilder) typeString(t types.Type, mode string) (string, error) {
	t = types.Unalias(t)
	switch t := t.(type) {
	case *types.Pointer:
		inner, err := b.typeString(t.Elem(), mode)
		if err != nil {
			return "", err
		}
		return inner + " | null", nil
	case *types.Slice:
		if isByteSlice(t) {
			return "string", nil
		}
		inner, err := b.typeString(t.Elem(), mode)
		if err != nil {
			return "", err
		}
		return arrayOf(inner), nil
	case *types.Array:
		inner, err := b.typeString(t.Elem(), mode)
		if err != nil {
			return "", err
		}
		return arrayOf(inner), nil
	case *types.Map:
		if _, err := mapKey(t.Key()); err != nil {
			return "", err
		}
		inner, err := b.typeString(t.Elem(), mode)
		if err != nil {
			return "", err
		}
		if strings.Contains(inner, "|") {
			inner = "(" + inner + ")"
		}
		return "Record<string, " + inner + ">", nil
	case *types.Basic:
		return basicTS(t)
	case *types.Named:
		return b.declareNamed(t, mode)
	case *types.Struct:
		return b.declareAnon(t, mode)
	case *types.Interface:
		return "", fmt.Errorf("interface %s is not part of the contract", t)
	default:
		return "", fmt.Errorf("unsupported type %s", t)
	}
}

func (b *tsBuilder) declareNamed(n *types.Named, mode string) (string, error) {
	obj := n.Obj()
	if obj.Pkg() == nil {
		return "", fmt.Errorf("unsupported type %s", obj.Name())
	}
	if obj.Pkg().Path() == "time" && obj.Name() == "Time" {
		return "string", nil
	}
	id := obj.Pkg().Path() + "." + obj.Name()
	key := id + "#" + mode
	if name, ok := b.seen[key]; ok {
		return name, nil
	}
	underlying := types.Unalias(n.Underlying())
	if st, ok := underlying.(*types.Struct); ok {
		omits := mode == "in" && structOmits(st)
		if !omits {
			if prev, ok := b.canon[id]; ok {
				b.seen[key] = prev
				return prev, nil
			}
		}
		base := obj.Name()
		if omits {
			base += "Input"
		}
		name := b.unique(base)
		decl := &tsDecl{Name: name, Kind: "iface", Doc: firstSentence(objComment(obj))}
		b.index[name] = decl
		b.decls = append(b.decls, decl)
		b.seen[key] = name
		if !omits {
			b.canon[id] = name
		}
		fields, _, err := b.fieldsOf(st, mode)
		if err != nil {
			return "", err
		}
		decl.Fields = fields
		return name, nil
	}
	name := b.unique(obj.Name())
	decl := &tsDecl{Name: name, Kind: "alias", Doc: firstSentence(objComment(obj))}
	b.add(decl)
	b.seen[key] = name
	if union := constUnion(n); union != "" {
		decl.Alias = union
		return name, nil
	}
	if s, ok := underlying.(*types.Slice); ok && isByteSlice(s) {
		decl.Alias = "string"
		return name, nil
	}
	inner, err := b.typeString(underlying, mode)
	if err != nil {
		return "", err
	}
	decl.Alias = inner
	return name, nil
}

func structOmits(st *types.Struct) bool {
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		tag := st.Tag(i)
		if routeServer(tag) {
			return true
		}
		if f.Embedded() && jsonExplicit(tag) == "" {
			inner, err := structType(f.Type())
			if err == nil && structOmits(inner) {
				return true
			}
		}
	}
	return false
}

func (b *tsBuilder) declareAnon(st *types.Struct, mode string) (string, error) {
	fields, _, err := b.fieldsOf(st, mode)
	if err != nil {
		return "", err
	}
	name := b.unique("Anon")
	b.add(&tsDecl{Name: name, Kind: "iface", Fields: fields})
	return name, nil
}

func (b *tsBuilder) fieldsOf(st *types.Struct, mode string) ([]tsField, bool, error) {
	var fields []tsField
	omitted := false
	used := map[string]bool{}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		tag := st.Tag(i)
		if !f.Exported() && !f.Embedded() {
			continue
		}
		if jsonIgnored(tag) || routeSkipped(tag) {
			continue
		}
		if mode == "in" && routeServer(tag) {
			omitted = true
			continue
		}
		if f.Embedded() && jsonExplicit(tag) == "" {
			inner, err := structType(f.Type())
			if err != nil {
				return nil, false, fmt.Errorf("embedded %s: %w", f.Name(), err)
			}
			promoted, om, err := b.fieldsOf(inner, mode)
			if err != nil {
				return nil, false, err
			}
			omitted = omitted || om
			for _, pf := range promoted {
				if used[pf.JSON] {
					return nil, false, fmt.Errorf("duplicate json name %s", pf.JSON)
				}
				used[pf.JSON] = true
				fields = append(fields, pf)
			}
			continue
		}
		name := jsonExplicit(tag)
		if name == "" {
			name = f.Name()
		}
		if used[name] {
			return nil, false, fmt.Errorf("duplicate json name %s", name)
		}
		used[name] = true
		ts, err := b.typeString(f.Type(), mode)
		if err != nil {
			return nil, false, fmt.Errorf("field %s: %w", f.Name(), err)
		}
		optional := isOptional(f.Type(), tag)
		if routeRequired(tag) {
			optional = false
		}
		fields = append(fields, tsField{JSON: name, Type: ts, Optional: optional})
	}
	return fields, omitted, nil
}

func (b *tsBuilder) add(d *tsDecl) {
	if _, ok := b.index[d.Name]; ok {
		return
	}
	b.index[d.Name] = d
	b.decls = append(b.decls, d)
}

func (b *tsBuilder) unique(base string) string {
	if base == "" || !unicode.IsLetter(rune(base[0])) && base[0] != '_' {
		base = "T" + base
	}
	name := base
	for i := 2; ; i++ {
		if _, ok := b.index[name]; !ok {
			return name
		}
		name = fmt.Sprintf("%s%d", base, i)
	}
}

func structType(t types.Type) (*types.Struct, error) {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	if n, ok := t.(*types.Named); ok {
		t = types.Unalias(n.Underlying())
	}
	st, ok := t.(*types.Struct)
	if !ok {
		return nil, fmt.Errorf("embedded %s is not a struct", t)
	}
	return st, nil
}

func isByteSlice(s *types.Slice) bool {
	b, ok := types.Unalias(s.Elem()).Underlying().(*types.Basic)
	return ok && b.Kind() == types.Byte
}

func mapKey(t types.Type) (string, error) {
	t = types.Unalias(t)
	if n, ok := t.(*types.Named); ok {
		t = types.Unalias(n.Underlying())
	}
	b, ok := t.(*types.Basic)
	if !ok {
		return "", fmt.Errorf("map key %s is not a string or number", t)
	}
	switch b.Kind() {
	case types.String, types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
		types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64:
		return "string", nil
	default:
		return "", fmt.Errorf("map key %s is not a string or number", b.Name())
	}
}

func basicTS(b *types.Basic) (string, error) {
	switch b.Kind() {
	case types.Bool:
		return "boolean", nil
	case types.String:
		return "string", nil
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
		types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64,
		types.Float32, types.Float64:
		return "number", nil
	default:
		return "", fmt.Errorf("unsupported basic type %s", b.Name())
	}
}

func arrayOf(inner string) string {
	if identRE.MatchString(inner) || strings.HasSuffix(inner, "[]") {
		return inner + "[]"
	}
	return "(" + inner + ")[]"
}

func isOptional(t types.Type, tag string) bool {
	if _, ok := types.Unalias(t).(*types.Pointer); ok {
		return true
	}
	_, opts, _ := strings.Cut(tagValue(tag, "json"), ",")
	return strings.Contains(","+opts+",", ",omitempty,")
}

func jsonIgnored(tag string) bool {
	name := tagValue(tag, "json")
	return name == "-" || strings.HasPrefix(name, "-,")
}

func jsonExplicit(tag string) string {
	raw := tagValue(tag, "json")
	if raw == "" || raw == "-" {
		return ""
	}
	name, _, _ := strings.Cut(raw, ",")
	if name == "-" {
		return ""
	}
	return name
}

func routeSkipped(tag string) bool { return routeHas(tag, "-") }
func routeServer(tag string) bool  { return routeHas(tag, "server") }
func routeRequired(tag string) bool {
	return routeHas(tag, "required")
}

func routeHas(tag, flag string) bool {
	raw := tagValue(tag, "route")
	if raw == "" {
		return false
	}
	for _, part := range strings.Split(raw, ",") {
		if strings.TrimSpace(part) == flag {
			return true
		}
	}
	return false
}

func tagValue(tag, key string) string {
	return reflect.StructTag(tag).Get(key)
}

func constUnion(n *types.Named) string {
	pkg := n.Obj().Pkg()
	if pkg == nil {
		return ""
	}
	var parts []string
	for _, name := range pkg.Scope().Names() {
		c, ok := pkg.Scope().Lookup(name).(*types.Const)
		if !ok || !c.Exported() || !types.Identical(c.Type(), n) {
			continue
		}
		switch c.Val().Kind() {
		case constant.String:
			parts = append(parts, strconv.Quote(constant.StringVal(c.Val())))
		case constant.Int:
			parts = append(parts, c.Val().ExactString())
		}
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return strings.Join(parts, " | ")
}

func objComment(obj types.Object) string { return "" }

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func renderTS(tsImport string, file tsFile) string {
	var b strings.Builder
	b.WriteString("// Code generated by routegen. DO NOT EDIT.\n\n")
	b.WriteString("import { call } from " + strconv.Quote(tsImport+"/client") + ";\n\n")
	for _, decl := range file.Decls {
		if doc := jsDoc(decl.Doc); doc != "" {
			b.WriteString(doc)
		}
		switch decl.Kind {
		case "alias":
			b.WriteString("export type " + decl.Name + " = " + decl.Alias + ";\n\n")
		default:
			b.WriteString("export interface " + decl.Name + " {\n")
			for _, field := range decl.Fields {
				opt := ""
				if field.Optional {
					opt = "?"
				}
				b.WriteString("  " + tsKey(field.JSON) + opt + ": " + field.Type + ";\n")
			}
			b.WriteString("}\n\n")
		}
	}
	b.WriteString("export const server = {\n")
	for i, h := range file.Handlers {
		if doc := jsDoc(h.Doc); doc != "" {
			b.WriteString("  ")
			b.WriteString(strings.ReplaceAll(doc, "\n", "\n  "))
		}
		inType := ""
		arg := "{}"
		params := ""
		if h.HasIn {
			inType = h.InName
			params = "input: " + inType
			arg = "input"
		}
		outType := "Record<string, never>"
		if h.HasOut {
			outType = h.OutName
		}
		fmt.Fprintf(&b, "  %s(%s): Promise<%s> {\n", h.Name, params, outType)
		fmt.Fprintf(&b, "    return call<%s>(%s, %s, %s);\n", outType, strconv.Quote(h.Service), strconv.Quote(h.Name), arg)
		b.WriteString("  },\n")
		if i == len(file.Handlers)-1 {
			break
		}
	}
	b.WriteString("};\n")
	return b.String()
}

func jsDoc(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "*/", "* /")
	return "/** " + s + " */\n"
}

func tsKey(name string) string {
	if identRE.MatchString(name) {
		return name
	}
	return strconv.Quote(name)
}
