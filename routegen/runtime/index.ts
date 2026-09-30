import "./react.tsx";

export {
  BrowserHistory,
  MemoryHistory,
  NotFoundError,
  Redirect,
  createFileRoute,
  createRootRoute,
  createRouter,
  matchRoutes,
  normalizeHref,
  notFound,
  parseLocation,
  redirect,
  type AnyRoute,
  type BrowserWindow,
  type RouterHistory,
  type Match,
  type NavigateOptions,
  type ParsedLocation,
  Router,
  type Route,
  type RouteContext,
  type RouteKind,
  type RouteOptions,
  type RouterState,
  type RouterStatus,
} from "./router.ts";
export { Link, Outlet, RouterProvider, useRouter, useRouterState } from "./react.tsx";
export { ClientError, call, configureRouteClient } from "./client.ts";
