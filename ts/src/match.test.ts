import { expect, test } from "bun:test";
import { createFileRoute, createRootRoute, matchRoutes } from "./router.ts";

function tree() {
  const root = createRootRoute();
  const home = createFileRoute("/")({});
  const clientsLayout = createFileRoute("/clients")({});
  const clientsIndex = createFileRoute("/clients/")({});
  const clientId = createFileRoute("/clients/$id")({});
  const clientEdit = createFileRoute("/clients/$id/edit")({});
  const postsNew = createFileRoute("/posts/new")({});
  const postId = createFileRoute("/posts/$postId")({});
  const auth = createFileRoute("/_auth")({});
  const login = createFileRoute("/login")({});
  const splat = createFileRoute("/api/trpc/$")({});
  auth.addChildren([login]);
  clientsLayout.addChildren([clientsIndex, clientId.addChildren([clientEdit])]);
  root.addChildren([home, clientsLayout, postsNew, postId, auth, splat]);
  return { root, home, clientsLayout, clientsIndex, clientId, clientEdit, postsNew, postId, auth, login, splat };
}

test("index wins over the layout and static wins over a parameter", () => {
  const routes = tree();
  const clients = matchRoutes(routes.root, "/clients");
  expect(clients?.map((match) => match.route.id)).toEqual(["__root", "/clients", "/clients/"]);
  const slashed = matchRoutes(routes.root, "/clients/");
  expect(slashed?.at(-1)?.route.id).toBe("/clients/");
  const post = matchRoutes(routes.root, "/posts/new");
  expect(post?.at(-1)?.route.id).toBe("/posts/new");
  const dynamic = matchRoutes(routes.root, "/posts/ada");
  expect(dynamic?.at(-1)?.route.id).toBe("/posts/$postId");
  expect(dynamic?.at(-1)?.params).toEqual({ postId: "ada" });
});

test("params follow the parent chain and a pathless route is not a leaf", () => {
  const routes = tree();
  const edit = matchRoutes(routes.root, "/clients/ada/edit");
  expect(edit?.map((match) => match.route.id)).toEqual(["__root", "/clients", "/clients/$id", "/clients/$id/edit"]);
  expect(edit?.map((match) => match.params)).toEqual([{}, {}, { id: "ada" }, { id: "ada" }]);
  const login = matchRoutes(routes.root, "/login");
  expect(login?.map((match) => match.route.id)).toEqual(["__root", "/_auth", "/login"]);
  expect(login?.[1]?.params).toEqual({});
  expect(matchRoutes(routes.root, "/_auth")).toBeNull();
});

test("a splat consumes the rest and equal scores use the smaller id", () => {
  const routes = tree();
  const matched = matchRoutes(routes.root, "/api/trpc/a/b");
  expect(matched?.at(-1)?.route.id).toBe("/api/trpc/$");
  expect(matched?.at(-1)?.params).toEqual({ "*": "a/b" });
  const home = matchRoutes(routes.root, "/");
  expect(home?.at(-1)?.route.id).toBe("/");
  const root = createRootRoute();
  root.addChildren([createFileRoute("/posts/$postId")({}), createFileRoute("/posts/$id")({})]);
  expect(matchRoutes(root, "/posts/ada")?.at(-1)?.route.id).toBe("/posts/$id");
  expect(routes.splat.kind).toBe("splat");
  expect(routes.auth.kind).toBe("pathless");
  expect(routes.clientsIndex.kind).toBe("index");
});

test("missing paths do not match", () => {
  const routes = tree();
  expect(matchRoutes(routes.root, "/missing")).toBeNull();
});
