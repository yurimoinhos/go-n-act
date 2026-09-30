import { expect, test } from "bun:test";
import { act, create } from "react-test-renderer";
import { createFileRoute, createRootRoute, createRouter, RouterProvider, Link, Outlet } from "./index.ts";

test("Outlet renders the matched child and hooks read the load", async () => {
  const root = createRootRoute({
    component: () => <Outlet />,
  });
  const about = createFileRoute("/about")({
    loader: () => "sobre",
    component: function About() {
      return <p>{about.useLoaderData()}</p>;
    },
  });
  const client = createFileRoute("/clients/$id")({
    loader: () => "ok",
    component: function Client() {
      return <p>{client.useParams().id}</p>;
    },
  });
  root.addChildren([about, client]);
  const router = createRouter({ routeTree: root });
  await router.navigate("/about");
  const aboutHtml = create(
    <RouterProvider router={router} />,
  ).toJSON();
  expect(JSON.stringify(aboutHtml)).toContain("sobre");

  await router.navigate("/clients/ada");
  const clientHtml = create(<RouterProvider router={router} />).toJSON();
  expect(JSON.stringify(clientHtml)).toContain("ada");
});

test("Link prevents an unmodified left click and ignores a modified click", async () => {
  const root = createRootRoute({
    component: () => (
      <>
        <Link to="/about">about</Link>
        <Outlet />
      </>
    ),
  });
  const home = createFileRoute("/")({
    component: () => <p>home</p>,
  });
  const about = createFileRoute("/about")({
    component: () => <p>about</p>,
  });
  root.addChildren([home, about]);
  const router = createRouter({ routeTree: root });
  await router.navigate("/");
  let renderer = create(<RouterProvider router={router} />);
  const link = renderer.root.findByType("a");
  expect(link.props.href).toBe("/about");

  let prevented = false;
  await act(async () => {
    link.props.onClick({
      button: 0,
      metaKey: false,
      altKey: false,
      ctrlKey: false,
      shiftKey: false,
      defaultPrevented: false,
      preventDefault() {
        prevented = true;
      },
    });
  });
  expect(prevented).toBe(true);
  await Promise.resolve();
  expect(router.state.location.pathname).toBe("/about");

  prevented = false;
  link.props.onClick({
    button: 0,
    metaKey: true,
    altKey: false,
    ctrlKey: false,
    shiftKey: false,
    defaultPrevented: false,
    preventDefault() {
      prevented = true;
    },
  });
  expect(prevented).toBe(false);
});
