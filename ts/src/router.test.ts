import { expect, test } from "bun:test";
import {
  MemoryHistory,
  createFileRoute,
  createRootRoute,
  createRouter,
  notFound,
  redirect,
} from "./router.ts";

function app() {
  const root = createRootRoute();
  const home = createFileRoute("/")({ loader: () => "home" });
  const about = createFileRoute("/about")({ loader: () => "about" });
  root.addChildren([home, about]);
  return { root, home, about };
}

test("push does not notify history and back does", async () => {
  const { root } = app();
  const history = new MemoryHistory("/");
  let notes = 0;
  history.subscribe(() => {
    notes += 1;
  });
  const router = createRouter({ routeTree: root, history });
  await router.navigate("/about");
  expect(notes).toBe(0);
  expect(history.href).toBe("/about");
  expect(router.state.location.pathname).toBe("/about");
  history.back();
  await Promise.resolve();
  expect(notes).toBe(1);
  expect(router.state.location.pathname).toBe("/");
});

test("pending keeps the previous matches", async () => {
  const { root } = app();
  let release: (value: string) => void = () => {};
  const slow = createFileRoute("/slow")({
    loader: () =>
      new Promise<string>((resolve) => {
        release = resolve;
      }),
  });
  root.addChildren([slow]);
  const router = createRouter({ routeTree: root });
  await router.navigate("/");
  const pending = router.navigate("/slow");
  expect(router.state.status).toBe("pending");
  expect(router.state.matches.at(-1)?.loaderData).toBe("home");
  release("done");
  await pending;
  expect(router.state.status).toBe("idle");
  expect(router.state.matches.at(-1)?.route.id).toBe("/slow");
  expect(router.state.matches.at(-1)?.loaderData).toBe("done");
});

test("a superseded load does not commit or redirect", async () => {
  const root = createRootRoute();
  let release: () => void = () => {};
  const slow = createFileRoute("/slow")({
    beforeLoad: () =>
      new Promise<void>((resolve) => {
        release = resolve;
      }).then(() => {
        redirect("/hijack");
      }),
  });
  const fast = createFileRoute("/fast")({ loader: () => "fast" });
  const hijack = createFileRoute("/hijack")({ loader: () => "hijack" });
  root.addChildren([slow, fast, hijack]);
  const router = createRouter({ routeTree: root });
  const first = router.navigate("/slow");
  await router.navigate("/fast");
  release();
  await first;
  expect(router.state.location.pathname).toBe("/fast");
  expect(router.state.status).toBe("idle");
  expect(router.history.href).toBe("/fast");
});

test("nine redirects fail and eight are followed", async () => {
  const root = createRootRoute();
  let hops = 0;
  const loop = createFileRoute("/loop")({
    beforeLoad: () => {
      hops += 1;
      redirect("/loop");
    },
  });
  const login = createFileRoute("/login")({ loader: () => "login" });
  root.addChildren([loop, login]);
  const failing = createRouter({ routeTree: root });
  await expect(failing.navigate("/loop")).rejects.toThrow("too many redirects");
  expect(failing.state.status).toBe("error");
  expect(hops).toBe(9);

  hops = 0;
  const once = createFileRoute("/start")({
    beforeLoad: () => {
      hops += 1;
      if (hops <= 8) redirect("/start");
    },
  });
  const root2 = createRootRoute();
  root2.addChildren([once]);
  const followed = createRouter({ routeTree: root2 });
  await followed.navigate("/start");
  expect(followed.state.status).toBe("idle");
  expect(hops).toBe(9);
});

test("redirect replaces the history entry by default", async () => {
  const root = createRootRoute();
  const gate = createFileRoute("/app")({
    beforeLoad: () => {
      redirect("/login");
    },
  });
  const login = createFileRoute("/login")({ loader: () => "login" });
  root.addChildren([gate, login]);
  const history = new MemoryHistory("/");
  const router = createRouter({ routeTree: root, history });
  await router.navigate("/app");
  expect(router.state.location.pathname).toBe("/login");
  history.back();
  await Promise.resolve();
  expect(router.state.location.pathname).toBe("/");
});

test("navigation rejects absolute and protocol-relative hrefs", () => {
  const { root } = app();
  const router = createRouter({ routeTree: root });
  expect(() => router.navigate("https://evil.test/a")).toThrow("navigation href must be a path");
  expect(() => router.navigate("//evil.test/a")).toThrow("navigation href must be a path");
  expect(router.state.location.pathname).toBe("/");
});

test("a missing path and notFound reject", async () => {
  const root = createRootRoute();
  const hidden = createFileRoute("/hidden")({
    beforeLoad: () => {
      notFound();
    },
  });
  root.addChildren([hidden]);
  const router = createRouter({ routeTree: root });
  await expect(router.navigate("/missing")).rejects.toThrow("not found");
  expect(router.state.status).toBe("notFound");
  await expect(router.navigate("/hidden")).rejects.toThrow("not found");
  expect(router.state.status).toBe("notFound");
});

test("validateSearch runs before beforeLoad and loaders", async () => {
  const order: string[] = [];
  const root = createRootRoute({
    validateSearch: () => {
      order.push("root-search");
      return { ok: true };
    },
    beforeLoad: () => {
      order.push("root-before");
    },
    loader: () => {
      order.push("root-loader");
    },
  });
  const page = createFileRoute("/page")({
    validateSearch: (search) => {
      order.push(`page-search:${search.q ?? ""}`);
      return { q: search.q ?? "" };
    },
    beforeLoad: () => {
      order.push("page-before");
    },
    loader: () => {
      order.push("page-loader");
      return "page";
    },
  });
  root.addChildren([page]);
  const router = createRouter({ routeTree: root });
  await router.navigate("/page?q=1");
  expect(order).toEqual([
    "root-search",
    "root-before",
    "root-loader",
    "page-search:1",
    "page-before",
    "page-loader",
  ]);
  expect(router.state.matches.at(-1)?.search).toEqual({ q: "1" });
});

test("loader cache follows staleTime and in-flight calls share one load", async () => {
  let calls = 0;
  let release: (value: string) => void = () => {};
  const root = createRootRoute();
  const page = createFileRoute("/cached")({
    loader: () => {
      calls += 1;
      return "cached";
    },
  });
  const other = createFileRoute("/other")({ loader: () => "other" });
  root.addChildren([page, other]);
  const fresh = createRouter({ routeTree: root, staleTime: 60_000 });
  await fresh.navigate("/cached");
  await fresh.navigate("/other");
  await fresh.navigate("/cached");
  expect(calls).toBe(1);

  calls = 0;
  const cold = createRouter({ routeTree: root, staleTime: 0 });
  await cold.navigate("/cached");
  await cold.navigate("/other");
  await cold.navigate("/cached");
  expect(calls).toBe(2);

  calls = 0;
  const gated = createFileRoute("/gated")({
    loader: () => {
      calls += 1;
      return new Promise<string>((resolve) => {
        release = resolve;
      });
    },
  });
  const root2 = createRootRoute();
  root2.addChildren([gated]);
  const shared = createRouter({ routeTree: root2 });
  const first = shared.navigate("/gated");
  const second = shared.navigate("/gated");
  expect(calls).toBe(1);
  release("ok");
  await first;
  await second;
  expect(shared.state.matches.at(-1)?.loaderData).toBe("ok");
});
