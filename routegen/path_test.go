package routegen

import (
	"strings"
	"testing"
)

func TestPlanTree(t *testing.T) {
	routes, err := Plan([]string{
		"__root.tsx",
		"index.tsx",
		"about.sass",
		"about.tsx",
		"about.go",
		"about.css",
		"about.scss",
		"dashboard/route.tsx",
		"dashboard/index.tsx",
		"dashboard/clients/index.tsx",
		"dashboard/clients/$id.tsx",
		"api/trpc/$.tsx",
		"posts.$postId.tsx",
		"_auth.tsx",
		"_auth/login.tsx",
		"clients/$id/route.tsx",
		"doc.go",
		"helper_test.go",
		"skip.gen.go",
		"skip.gen.ts",
		"routeTree.gen.tsx",
	})
	if err != nil {
		t.Fatal(err)
	}
	if routes[0].Key != "__root" || routes[0].Kind != KindRoot || routes[0].Service != "route.root.v1" {
		t.Fatalf("root = %+v", routes[0])
	}
	by := map[string]Route{}
	for _, route := range routes {
		by[route.Key] = route
	}
	for _, key := range []string{"doc", "helper_test", "skip.gen", "routeTree.gen"} {
		if _, ok := by[key]; ok {
			t.Fatalf("kept ignored file %s", key)
		}
	}

	index := by["index"]
	if index.Pattern != "/" || index.Kind != KindIndex || index.ParentKey != "__root" || index.Service != "route.index.v1" {
		t.Fatalf("index = %+v", index)
	}
	about := by["about"]
	if about.Pattern != "/about" || about.Kind != KindLeaf || about.ParentKey != "__root" || about.Service != "route.about.v1" || !about.HasTSX || !about.HasGo {
		t.Fatalf("about = %+v", about)
	}
	if strings.Join(about.Styles, ",") != "about.css,about.scss,about.sass" {
		t.Fatalf("styles = %v", about.Styles)
	}
	layout := by["dashboard/route"]
	if layout.Pattern != "/dashboard" || layout.Kind != KindLayout || layout.LayoutOf != "dashboard" || layout.ParentKey != "__root" || layout.Service != "route.dashboard.layout.v1" {
		t.Fatalf("layout = %+v", layout)
	}
	dashIndex := by["dashboard/index"]
	if dashIndex.Pattern != "/dashboard/" || dashIndex.Kind != KindIndex || dashIndex.ParentKey != "dashboard/route" || dashIndex.Service != "route.dashboard.index.v1" {
		t.Fatalf("dashboard index = %+v", dashIndex)
	}
	clients := by["dashboard/clients/index"]
	if clients.Pattern != "/dashboard/clients/" || clients.ParentKey != "dashboard/route" || clients.Service != "route.dashboard.clients.index.v1" {
		t.Fatalf("clients = %+v", clients)
	}
	id := by["dashboard/clients/$id"]
	if id.Pattern != "/dashboard/clients/$id" || id.Kind != KindLeaf || id.ParentKey != "dashboard/route" || id.Service != "route.dashboard.clients.param_id.v1" {
		t.Fatalf("id = %+v", id)
	}
	splat := by["api/trpc/$"]
	if splat.Pattern != "/api/trpc/$" || splat.Kind != KindSplat || splat.Service != "route.api.trpc.splat.v1" {
		t.Fatalf("splat = %+v", splat)
	}
	post := by["posts.$postId"]
	if post.Pattern != "/posts/$postId" || post.ParentKey != "__root" || post.Service != "route.posts.param_postId.v1" {
		t.Fatalf("post = %+v", post)
	}
	auth := by["_auth"]
	if auth.Pattern != "/_auth" || auth.Kind != KindPathless || auth.Service != "route.pathless_auth.v1" {
		t.Fatalf("auth = %+v", auth)
	}
	login := by["_auth/login"]
	if login.Pattern != "/login" || login.Kind != KindLeaf || login.ParentKey != "_auth" || login.Service != "route.login.v1" {
		t.Fatalf("login = %+v", login)
	}
	paramLayout := by["clients/$id/route"]
	if paramLayout.Pattern != "/clients/$id" || paramLayout.Kind != KindLayout || paramLayout.ParentKey != "__root" || paramLayout.Service != "route.clients.param_id.layout.v1" {
		t.Fatalf("param layout = %+v", paramLayout)
	}
}

func TestPlanPathlessDirectory(t *testing.T) {
	routes, err := Plan([]string{"_auth/route.tsx", "_auth/login.tsx"})
	if err != nil {
		t.Fatal(err)
	}
	by := indexRoutes(routes)
	auth := by["_auth/route"]
	if auth.Kind != KindPathless || auth.Pattern != "/_auth" || auth.Service != "route.pathless_auth.v1" {
		t.Fatalf("auth = %+v", auth)
	}
	if by["_auth/login"].ParentKey != "_auth/route" || by["_auth/login"].Pattern != "/login" {
		t.Fatalf("login = %+v", by["_auth/login"])
	}
}

func TestPlanDottedPathlessParent(t *testing.T) {
	routes, err := Plan([]string{"_auth.tsx", "_auth.login.tsx"})
	if err != nil {
		t.Fatal(err)
	}
	by := indexRoutes(routes)
	if by["_auth.login"].Pattern != "/login" || by["_auth.login"].ParentKey != "_auth" {
		t.Fatalf("login = %+v", by["_auth.login"])
	}
}

func TestPlanRejects(t *testing.T) {
	cases := []struct {
		name string
		rels []string
		want string
	}{
		{name: "nested root", rels: []string{"dashboard/__root.tsx"}, want: "__root"},
		{name: "root route file", rels: []string{"route.tsx"}, want: "__root.tsx"},
		{name: "splat", rels: []string{"a/$/b.tsx"}, want: "splat"},
		{name: "same url", rels: []string{"about.tsx", "about/index.tsx"}, want: "same URL"},
		{name: "same service", rels: []string{"clients/$id.tsx", "clients/param_id.tsx"}, want: "same service"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Plan(tc.rels)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestAlias(t *testing.T) {
	if Alias("__root") != "RootRoute" || Alias("index") != "IndexRoute" || Alias("clients/$id") != "ClientsParamIdRoute" {
		t.Fatalf("aliases %s %s %s", Alias("__root"), Alias("index"), Alias("clients/$id"))
	}
	if Alias("$") != "SplatRoute" || Alias("_auth") != "PathlessAuthRoute" {
		t.Fatalf("splat %s pathless %s", Alias("$"), Alias("_auth"))
	}
}

func TestPlanGoParamFile(t *testing.T) {
	routes, err := Plan([]string{
		"clients/$id.tsx",
		"clients/param_id.go",
		"api/trpc/$.tsx",
		"api/trpc/splat.go",
		"posts.param_postId.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	by := indexRoutes(routes)
	id := by["clients/$id"]
	if !id.HasTSX || !id.HasGo || id.GoRel != "clients/param_id.go" || id.Service != "route.clients.param_id.v1" || id.Kind != KindLeaf {
		t.Fatalf("param file = %+v", id)
	}
	splat := by["api/trpc/$"]
	if !splat.HasTSX || !splat.HasGo || splat.GoRel != "api/trpc/splat.go" || splat.Kind != KindSplat {
		t.Fatalf("splat file = %+v", splat)
	}
	post := by["posts.$postId"]
	if !post.HasGo || post.GoRel != "posts.param_postId.go" || post.Service != "route.posts.param_postId.v1" {
		t.Fatalf("dotted param = %+v", post)
	}
}

func indexRoutes(routes []Route) map[string]Route {
	by := map[string]Route{}
	for _, route := range routes {
		by[route.Key] = route
	}
	return by
}
