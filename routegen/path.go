package routegen

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// Kind is how a file participates in the URL tree.
type Kind string

const (
	KindRoot     Kind = "root"
	KindLayout   Kind = "layout"
	KindIndex    Kind = "index"
	KindLeaf     Kind = "leaf"
	KindSplat    Kind = "splat"
	KindPathless Kind = "pathless"
)

// Route is one stem under the routes directory.
// A stem may have a .tsx page, a .go endpoint file, and optional styles.
type Route struct {
	Key       string
	Pattern   string
	Kind      Kind
	Dir       string
	LayoutOf  string
	ParentKey string
	Service   string
	HasTSX    bool
	HasGo     bool
	TSXRel    string
	GoRel     string
	Styles    []string
}

// Plan builds the route tree from slash-separated paths relative to the routes root.
// Generated files and Go tests are ignored. Parent links follow layout files the way
// a file router does: route.tsx wraps its directory, index.tsx is the trailing-slash
// page, $param is one segment, $ is a splat, and a _segment is pathless.
func Plan(rels []string) ([]Route, error) {
	grouped := map[string]*Route{}
	var order []string
	for _, rel := range rels {
		rel = strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(rel, "\\", "/")), "/")
		if rel == "." || rel == "" || skipRel(rel) {
			continue
		}
		ext := path.Ext(rel)
		switch ext {
		case ".tsx", ".go", ".css", ".scss", ".sass":
		default:
			continue
		}
		key := strings.TrimSuffix(rel, ext)
		if ext == ".go" {
			key = goStemToRoute(key)
		}
		route, ok := grouped[key]
		if !ok {
			meta, err := classify(key)
			if err != nil {
				return nil, err
			}
			route = &Route{
				Key:      key,
				Pattern:  meta.pattern,
				Kind:     meta.kind,
				Dir:      meta.dir,
				LayoutOf: meta.layoutOf,
				Service:  serviceName(meta.pattern, meta.kind),
			}
			grouped[key] = route
			order = append(order, key)
		}
		switch ext {
		case ".tsx":
			route.HasTSX = true
			route.TSXRel = rel
		case ".go":
			route.HasGo = true
			route.GoRel = rel
		default:
			route.Styles = append(route.Styles, rel)
		}
	}
	if _, ok := grouped["__root"]; !ok {
		grouped["__root"] = &Route{
			Key:     "__root",
			Kind:    KindRoot,
			Service: "route.root.v1",
		}
	} else {
		root := grouped["__root"]
		root.Kind = KindRoot
		root.Pattern = ""
		root.Service = "route.root.v1"
		root.LayoutOf = ""
	}

	for _, key := range order {
		route := grouped[key]
		if route.Kind == KindRoot {
			continue
		}
		route.ParentKey = parentOf(route, grouped)
	}
	if err := collisions(grouped); err != nil {
		return nil, err
	}

	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	for _, route := range grouped {
		sort.Slice(route.Styles, func(i, j int) bool {
			a, b := route.Styles[i], route.Styles[j]
			if ra, rb := styleRank(a), styleRank(b); ra != rb {
				return ra < rb
			}
			return a < b
		})
	}
	out := []Route{*grouped["__root"]}
	for _, key := range order {
		if key == "__root" {
			continue
		}
		out = append(out, *grouped[key])
	}
	if err := serviceCollisions(out); err != nil {
		return nil, err
	}
	return out, nil
}

// goStemToRoute maps a Go file stem onto the route key shared with .tsx files.
// The compiler rejects '$' in a file name, so param_<name>.go is the /$<name>
// route and splat.go is the splat segment. The go tool ignores a file whose
// name starts with '_', so pathless_<name>.go is the /_<name> route.
// A static segment literally named param_<name>, splat, or pathless_<name>
// is rewritten the same way.
func goStemToRoute(stem string) string {
	parts := strings.Split(stem, "/")
	for i, part := range parts {
		dots := strings.Split(part, ".")
		for j, token := range dots {
			dots[j] = goTokenToRoute(token)
		}
		parts[i] = strings.Join(dots, ".")
	}
	return strings.Join(parts, "/")
}

func goTokenToRoute(token string) string {
	if token == "splat" {
		return "$"
	}
	if rest, ok := strings.CutPrefix(token, "param_"); ok && routeIdent(rest) {
		return "$" + rest
	}
	if rest, ok := strings.CutPrefix(token, "pathless_"); ok && routeIdent(rest) {
		return "_" + rest
	}
	return token
}

func routeIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		ok := r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (i > 0 && r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}

func styleRank(rel string) int {
	switch path.Ext(rel) {
	case ".css":
		return 0
	case ".scss":
		return 1
	case ".sass":
		return 2
	default:
		return 9
	}
}

func serviceCollisions(routes []Route) error {
	seen := map[string]string{}
	for _, route := range routes {
		prev, ok := seen[route.Service]
		if !ok {
			seen[route.Service] = route.Key
			continue
		}
		a, b := prev, route.Key
		if b < a {
			a, b = b, a
		}
		return fmt.Errorf("routegen: routes %s and %s use the same service %s", a, b, route.Service)
	}
	return nil
}

func skipRel(rel string) bool {
	base := path.Base(rel)
	switch {
	case base == "doc.go", base == "routeTree.gen.tsx":
		return true
	case strings.HasSuffix(rel, "_test.go"):
		return true
	case strings.HasSuffix(rel, ".gen.go"), strings.HasSuffix(rel, ".gen.ts"), strings.HasSuffix(rel, ".gen.tsx"):
		return true
	default:
		return false
	}
}

type classified struct {
	pattern  string
	kind     Kind
	dir      string
	layoutOf string
}

func classify(key string) (classified, error) {
	if key == "__root" {
		return classified{kind: KindRoot}, nil
	}
	if strings.HasSuffix(key, "/__root") || strings.Contains(key, "__root/") {
		return classified{}, fmt.Errorf("routegen: __root must be the routes root, got %s", key)
	}
	if key == "route" {
		return classified{}, fmt.Errorf("routegen: the root layout is __root.tsx")
	}
	dir, base := path.Split(key)
	dir = strings.TrimSuffix(dir, "/")
	var tokens []string
	if dir != "" {
		tokens = append(tokens, strings.Split(dir, "/")...)
	}
	if base != "" {
		tokens = append(tokens, strings.Split(base, ".")...)
	}
	if len(tokens) == 0 {
		return classified{}, fmt.Errorf("routegen: empty route %s", key)
	}

	kind := KindLeaf
	layoutOf := ""
	if tokens[len(tokens)-1] == "route" {
		kind = KindLayout
		layoutOf = strings.Join(tokens[:len(tokens)-1], "/")
		tokens = tokens[:len(tokens)-1]
	}
	if len(tokens) > 0 && tokens[len(tokens)-1] == "index" {
		if kind == KindLayout {
			return classified{}, fmt.Errorf("routegen: %s cannot be both route and index", key)
		}
		kind = KindIndex
		tokens = tokens[:len(tokens)-1]
	}
	for i, token := range tokens {
		if token == "$" && i != len(tokens)-1 {
			return classified{}, fmt.Errorf("routegen: %s splat must be the last segment", key)
		}
	}
	if len(tokens) > 0 && tokens[len(tokens)-1] == "$" {
		kind = KindSplat
	}

	var url []string
	sawPathless := false
	for _, token := range tokens {
		if isPathless(token) {
			sawPathless = true
			continue
		}
		url = append(url, token)
	}
	if sawPathless && len(url) == 0 {
		kind = KindPathless
		if layoutOf == "" {
			layoutOf = strings.Join(tokens, "/")
		}
		return classified{
			pattern:  "/" + strings.Join(tokens, "/"),
			kind:     kind,
			dir:      dir,
			layoutOf: layoutOf,
		}, nil
	}

	pattern := "/" + strings.Join(url, "/")
	if pattern == "/" {
		pattern = "/"
	}
	if kind == KindIndex && pattern != "/" {
		pattern += "/"
	}
	if len(url) == 0 && (kind == KindLayout || kind == KindIndex) {
		pattern = "/"
	}
	return classified{pattern: pattern, kind: kind, dir: dir, layoutOf: layoutOf}, nil
}

func isPathless(token string) bool {
	return strings.HasPrefix(token, "_") && !strings.HasPrefix(token, "__") && token != "_"
}

func parentOf(route *Route, all map[string]*Route) string {
	if route.Kind == KindRoot {
		return ""
	}
	base := route.Key
	if route.Dir != "" {
		base = strings.TrimPrefix(route.Key, route.Dir+"/")
	}
	segs := strings.Split(base, ".")
	if len(segs) > 1 && isPathless(segs[0]) {
		if parent, ok := all[segs[0]]; ok && parent.Kind == KindPathless && parent.Key != route.Key {
			return parent.Key
		}
		if parent, ok := all[segs[0]+"/route"]; ok && parent.Kind == KindPathless && parent.Key != route.Key {
			return parent.Key
		}
	}

	start := route.Dir
	if route.LayoutOf != "" {
		if strings.Contains(route.LayoutOf, "/") {
			start = path.Dir(route.LayoutOf)
			if start == "." {
				start = ""
			}
		} else {
			start = ""
		}
	}
	dir := start
	for {
		if key, ok := layoutKey(dir, all); ok && key != route.Key {
			return key
		}
		if dir == "" {
			return "__root"
		}
		next := path.Dir(dir)
		if next == "." || next == dir {
			dir = ""
			continue
		}
		dir = next
	}
}

func layoutKey(dir string, all map[string]*Route) (string, bool) {
	cand := "route"
	if dir != "" {
		cand = dir + "/route"
	}
	if route, ok := all[cand]; ok && (route.Kind == KindLayout || route.Kind == KindPathless) {
		return cand, true
	}
	if dir != "" {
		if route, ok := all[dir]; ok && route.Kind == KindPathless {
			return dir, true
		}
	}
	return "", false
}

func collisions(all map[string]*Route) error {
	type claim struct {
		key  string
		kind Kind
	}
	claims := map[string][]claim{}
	for _, route := range all {
		if route.Kind == KindRoot || route.Kind == KindPathless {
			continue
		}
		norm := strings.TrimSuffix(route.Pattern, "/")
		if norm == "" {
			norm = "/"
		}
		claims[norm] = append(claims[norm], claim{route.Key, route.Kind})
	}
	var norms []string
	for norm := range claims {
		norms = append(norms, norm)
	}
	sort.Strings(norms)
	for _, norm := range norms {
		group := claims[norm]
		if len(group) < 2 {
			continue
		}
		if len(group) == 2 && (group[0].kind == KindLayout && group[1].kind == KindIndex || group[0].kind == KindIndex && group[1].kind == KindLayout) {
			continue
		}
		names := make([]string, len(group))
		for i, c := range group {
			names[i] = c.key
		}
		sort.Strings(names)
		return fmt.Errorf("routegen: routes %s claim the same URL %s", strings.Join(names, " and "), norm)
	}
	return nil
}

func serviceName(pattern string, kind Kind) string {
	if kind == KindRoot {
		return "route.root.v1"
	}
	if kind == KindPathless {
		id := strings.Trim(pattern, "/")
		var parts []string
		if id != "" {
			for _, part := range strings.Split(id, "/") {
				raw := strings.TrimLeft(part, "_")
				if raw == "" {
					raw = "path"
				}
				parts = append(parts, "pathless_"+sanitize(raw))
			}
		}
		if len(parts) == 0 {
			return "route.pathless.v1"
		}
		return "route." + strings.Join(parts, ".") + ".v1"
	}
	trimmed := strings.Trim(pattern, "/")
	var parts []string
	if trimmed != "" {
		for _, part := range strings.Split(trimmed, "/") {
			switch {
			case part == "$":
				parts = append(parts, "splat")
			case strings.HasPrefix(part, "$"):
				parts = append(parts, "param_"+sanitize(part[1:]))
			default:
				parts = append(parts, sanitize(part))
			}
		}
	}
	name := strings.Join(parts, ".")
	if kind == KindIndex || kind == KindLayout {
		if name == "" {
			return "route." + string(kind) + ".v1"
		}
		return "route." + name + "." + string(kind) + ".v1"
	}
	if name == "" {
		return "route." + string(kind) + ".v1"
	}
	return "route." + name + ".v1"
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	out := b.String()
	if out == "" {
		return "_"
	}
	if out[0] >= '0' && out[0] <= '9' {
		return "_" + out
	}
	return out
}

// Alias is the generated TypeScript binding name for a route key.
func Alias(key string) string {
	if key == "__root" {
		return "RootRoute"
	}
	var b strings.Builder
	for _, part := range strings.Split(key, "/") {
		for _, piece := range strings.Split(part, ".") {
			b.WriteString(exportIdent(piece))
		}
	}
	out := b.String() + "Route"
	if out == "Route" {
		return "IndexRoute"
	}
	return out
}

func exportIdent(s string) string {
	switch {
	case s == "$":
		return "Splat"
	case strings.HasPrefix(s, "$"):
		return "Param" + exportIdent(s[1:])
	case strings.HasPrefix(s, "_"):
		return "Pathless" + exportIdent(s[1:])
	case s == "":
		return ""
	default:
		r := []rune(s)
		return strings.ToUpper(string(r[0])) + string(r[1:])
	}
}
