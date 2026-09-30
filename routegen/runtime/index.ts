import "./react.tsx";

export {
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
