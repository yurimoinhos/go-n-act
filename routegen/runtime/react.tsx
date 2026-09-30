import { createContext, useContext, useSyncExternalStore, type ReactNode } from "react";
import { setRouteHooks, type AnyRoute, type Router } from "./router.ts";

const RouterContext = createContext<Router | null>(null);
const MatchDepthContext = createContext(0);

export function useRouter(): Router {
  const router = useContext(RouterContext);
  if (!router) throw new Error("useRouter must be used inside RouterProvider");
  return router;
}

export function useRouterState() {
  const router = useRouter();
  return useSyncExternalStore(router.subscribe, router.getSnapshot, router.getSnapshot);
}

function useRouteMatch(route: AnyRoute) {
  const state = useRouterState();
  return state.matches.find((match) => match.route.id === route.id);
}

setRouteHooks({
  useLoaderData(route) {
    const match = useRouteMatch(route);
    if (!match) throw new Error(`route ${route.id} is not matched`);
    return match.loaderData;
  },
  useParams(route) {
    const match = useRouteMatch(route);
    if (!match) throw new Error(`route ${route.id} is not matched`);
    return match.params;
  },
  useSearch(route) {
    const match = useRouteMatch(route);
    if (!match) throw new Error(`route ${route.id} is not matched`);
    return match.search;
  },
});

type Component = () => ReactNode;

export function RouterProvider({ router }: { router: Router }) {
  const state = useSyncExternalStore(router.subscribe, router.getSnapshot, router.getSnapshot);
  const root = state.matches[0];
  const Component = root?.route.options.component as Component | undefined;
  return (
    <RouterContext.Provider value={router}>
      <MatchDepthContext.Provider value={0}>
        {Component ? <Component /> : root ? <Outlet /> : null}
      </MatchDepthContext.Provider>
    </RouterContext.Provider>
  );
}

export function Outlet() {
  const depth = useContext(MatchDepthContext);
  const state = useRouterState();
  const match = state.matches[depth + 1];
  if (!match) return null;
  const Component = match.route.options.component as Component | undefined;
  return (
    <MatchDepthContext.Provider value={depth + 1}>
      {Component ? <Component /> : <Outlet />}
    </MatchDepthContext.Provider>
  );
}

function unmodifiedLeftClick(event: {
  button: number;
  metaKey: boolean;
  altKey: boolean;
  ctrlKey: boolean;
  shiftKey: boolean;
  defaultPrevented: boolean;
}): boolean {
  return event.button === 0 && !event.metaKey && !event.altKey && !event.ctrlKey && !event.shiftKey && !event.defaultPrevented;
}

export function Link({ to, replace, children }: { to: string; replace?: boolean; children?: ReactNode }) {
  const router = useRouter();
  return (
    <a
      href={to}
      onClick={(event) => {
        if (!unmodifiedLeftClick(event)) return;
        event.preventDefault();
        void router.navigate(to, { replace });
      }}
    >
      {children}
    </a>
  );
}
