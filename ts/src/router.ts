import { inferKind, matchRoutes, routeSegments, type Matchable, type RouteKind } from "./match.ts";

export type { RouteKind };

export type RouteContext = {
  params: Record<string, string>;
  search: unknown;
  href: string;
  pathname: string;
  signal: AbortSignal;
};

export type RouteOptions<TLoader, TSearch> = {
  component?: () => unknown;
  beforeLoad?: (ctx: RouteContext) => unknown | Promise<unknown>;
  loader?: (ctx: RouteContext) => TLoader | Promise<TLoader>;
  validateSearch?: (search: Record<string, string>) => TSearch | Promise<TSearch>;
};

type HookFns = {
  useLoaderData: (route: AnyRoute) => unknown;
  useParams: (route: AnyRoute) => Record<string, string>;
  useSearch: (route: AnyRoute) => unknown;
};

const missingHook = () => {
  throw new Error('import "@aggitech/route" before calling route hooks');
};

let hooks: HookFns = {
  useLoaderData: missingHook,
  useParams: missingHook,
  useSearch: missingHook,
};

// setRouteHooks installs the React bindings. The package index calls this.
export function setRouteHooks(next: HookFns): void {
  hooks = next;
}

export interface AnyRoute extends Matchable {
  options: RouteOptions<any, any>;
  addChildren(children: AnyRoute[]): AnyRoute;
  useLoaderData(): any;
  useParams(): Record<string, string>;
  useSearch(): any;
}

export interface Route<TLoader, TSearch, TParams extends Record<string, string>> extends AnyRoute {
  options: RouteOptions<TLoader, TSearch>;
  useLoaderData(): TLoader;
  useParams(): TParams;
  useSearch(): TSearch;
}

export type Match = {
  route: AnyRoute;
  params: Record<string, string>;
  loaderData: unknown;
  search: unknown;
};

export type ParsedLocation = {
  href: string;
  pathname: string;
  search: string;
  searchParams: Record<string, string>;
  hash: string;
};

export type RouterStatus = "idle" | "pending" | "error" | "notFound";

export type RouterState = {
  location: ParsedLocation;
  status: RouterStatus;
  matches: Match[];
  error: unknown;
};

export class Redirect extends Error {
  readonly href: string;
  readonly replace: boolean;

  constructor(href: string, replace = true) {
    super("redirect");
    this.name = "Redirect";
    this.href = href;
    this.replace = replace;
  }
}

// redirect aborts the current load. replace defaults to true.
export function redirect(href: string, opts?: { replace?: boolean }): never {
  throw new Redirect(href, opts?.replace !== false);
}

export class NotFoundError extends Error {
  constructor() {
    super("not found");
    this.name = "NotFoundError";
  }
}

export function notFound(): never {
  throw new NotFoundError();
}

class StaleLoad extends Error {
  constructor() {
    super("stale");
    this.name = "StaleLoad";
  }
}

const navigationBase = "http://route.local";

// normalizeHref accepts only a path on this origin.
// Absolute and protocol-relative URLs are rejected.
export function normalizeHref(href: string): string {
  let url: URL;
  try {
    url = new URL(href, navigationBase);
  } catch {
    throw new Error("navigation href must be a path");
  }
  if (url.origin !== navigationBase) {
    throw new Error("navigation href must be a path");
  }
  return url.pathname + url.search + url.hash;
}

export function parseLocation(href: string): ParsedLocation {
  const url = new URL(href, navigationBase);
  const searchParams: Record<string, string> = {};
  url.searchParams.forEach((value, key) => {
    searchParams[key] = value;
  });
  return {
    href: url.pathname + url.search + url.hash,
    pathname: url.pathname,
    search: url.search,
    searchParams,
    hash: url.hash,
  };
}

function createRoute<TLoader, TSearch, TParams extends Record<string, string>>(
  id: string,
  path: string,
  options: RouteOptions<TLoader, TSearch>,
): Route<TLoader, TSearch, TParams> {
  const route: Route<TLoader, TSearch, TParams> = {
    id,
    path,
    kind: inferKind(id, path),
    segments: routeSegments(path),
    children: [],
    options,
    addChildren(children) {
      for (const child of children) {
        child.parent = route;
        route.children.push(child);
      }
      return route;
    },
    useLoaderData: () => hooks.useLoaderData(route) as TLoader,
    useParams: () => hooks.useParams(route) as TParams,
    useSearch: () => hooks.useSearch(route) as TSearch,
  };
  return route;
}

export function createFileRoute(path: string) {
  return function defineRoute<
    TLoader = unknown,
    TSearch = unknown,
    TParams extends Record<string, string> = Record<string, string>,
  >(options: RouteOptions<TLoader, TSearch> = {}): Route<TLoader, TSearch, TParams> {
    return createRoute<TLoader, TSearch, TParams>(path, path, options);
  };
}

export function createRootRoute<TLoader = unknown, TSearch = unknown>(
  options: RouteOptions<TLoader, TSearch> = {},
): Route<TLoader, TSearch, Record<string, string>> {
  return createRoute("__root", "", options);
}

type HistoryMode = "push" | "replace" | "ignore";

export type NavigateOptions = {
  replace?: boolean;
  history?: HistoryMode;
};

// MemoryHistory records navigation in memory.
// push and replace do not notify subscribers. Back and forward do.
export class MemoryHistory {
  private entries: string[];
  private cursor = 0;
  private listeners = new Set<(href: string) => void>();

  constructor(href = "/") {
    this.entries = [normalizeHref(href)];
  }

  get href(): string {
    return this.entries[this.cursor] ?? "/";
  }

  push(href: string): void {
    this.entries = this.entries.slice(0, this.cursor + 1);
    this.entries.push(href);
    this.cursor++;
  }

  replace(href: string): void {
    this.entries[this.cursor] = href;
  }

  back(): void {
    if (this.cursor === 0) return;
    this.cursor--;
    this.emit();
  }

  forward(): void {
    if (this.cursor >= this.entries.length - 1) return;
    this.cursor++;
    this.emit();
  }

  subscribe(listener: (href: string) => void): () => void {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  private emit(): void {
    for (const listener of this.listeners) listener(this.href);
  }
}

type CacheEntry = { data: unknown; at: number };

export class Router {
  readonly routeTree: AnyRoute;
  readonly history: MemoryHistory;
  readonly staleTime: number;
  state: RouterState;
  private generation = 0;
  private controller: AbortController | null = null;
  private inflight = new Map<string, Promise<void>>();
  private cache = new Map<string, CacheEntry>();
  private listeners = new Set<() => void>();

  getState = (): RouterState => this.state;
  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  getSnapshot = (): RouterState => this.state;

  constructor(opts: { routeTree: AnyRoute; history?: MemoryHistory; staleTime?: number }) {
    this.routeTree = opts.routeTree;
    this.history = opts.history ?? new MemoryHistory("/");
    this.staleTime = opts.staleTime ?? 0;
    this.state = {
      location: parseLocation(this.history.href),
      status: "idle",
      matches: [],
      error: undefined,
    };
    this.history.subscribe((href) => {
      void this.navigate(href, { history: "ignore" });
    });
  }

  private emit(): void {
    for (const listener of this.listeners) listener();
  }

  private setState(patch: Partial<RouterState>): void {
    this.state = { ...this.state, ...patch };
    this.emit();
  }

  load(): Promise<void> {
    return this.navigate(this.history.href, { history: "ignore" });
  }

  navigate(href: string, opts: NavigateOptions = {}): Promise<void> {
    const next = normalizeHref(href);
    const existing = this.inflight.get(next);
    if (existing) return existing;

    const mode: HistoryMode = opts.history ?? (opts.replace ? "replace" : "push");
    if (mode === "push") this.history.push(next);
    else if (mode === "replace") this.history.replace(next);

    this.generation += 1;
    this.controller?.abort();
    const generation = this.generation;
    const controller = new AbortController();
    this.controller = controller;
    this.setState({
      location: parseLocation(next),
      status: "pending",
      error: undefined,
    });

    let resolveInflight: () => void = () => {};
    let rejectInflight: (error: unknown) => void = () => {};
    const promise = new Promise<void>((resolve, reject) => {
      resolveInflight = resolve;
      rejectInflight = reject;
    });
    this.inflight.set(next, promise);
    void this.loadInner(next, generation, controller.signal).then(
      () => {
        if (this.inflight.get(next) === promise) this.inflight.delete(next);
        resolveInflight();
      },
      (error: unknown) => {
        if (this.inflight.get(next) === promise) this.inflight.delete(next);
        if (error instanceof StaleLoad) resolveInflight();
        else rejectInflight(error);
      },
    );
    return promise;
  }

  private current(generation: number, signal: AbortSignal): void {
    if (signal.aborted || generation !== this.generation) throw new StaleLoad();
  }

  private async loadInner(href: string, generation: number, signal: AbortSignal): Promise<void> {
    let current = href;
    let redirects = 0;
    while (true) {
      this.current(generation, signal);
      const location = parseLocation(current);
      const matched = matchRoutes(this.routeTree, location.pathname);
      if (matched === null) {
        this.current(generation, signal);
        const error = new NotFoundError();
        this.setState({ status: "notFound", error, location });
        throw error;
      }
      try {
        const matches = await this.resolve(matched, location, generation, signal);
        this.current(generation, signal);
        this.setState({ status: "idle", matches: matches, error: undefined, location });
        return;
      } catch (error) {
        if (error instanceof StaleLoad || signal.aborted || generation !== this.generation) {
          throw error instanceof StaleLoad ? error : new StaleLoad();
        }
        if (error instanceof Redirect) {
          redirects += 1;
          if (redirects > 8) {
            const failure = new Error("too many redirects");
            this.setState({ status: "error", error: failure, location });
            throw failure;
          }
          let target: string;
          try {
            target = normalizeHref(error.href);
          } catch (invalid) {
            this.setState({ status: "error", error: invalid, location });
            throw invalid;
          }
          this.current(generation, signal);
          if (error.replace) this.history.replace(target);
          else this.history.push(target);
          current = target;
          this.setState({ location: parseLocation(target), status: "pending" });
          continue;
        }
        if (error instanceof NotFoundError) {
          this.setState({ status: "notFound", error, location });
          throw error;
        }
        this.setState({ status: "error", error, location });
        throw error;
      }
    }
  }

  private async resolve(
    matched: { route: Matchable; params: Record<string, string> }[],
    location: ParsedLocation,
    generation: number,
    signal: AbortSignal,
  ): Promise<Match[]> {
    const matches: Match[] = [];
    let search: unknown = location.searchParams;
    for (const entry of matched) {
      this.current(generation, signal);
      const route = entry.route as AnyRoute;
      if (route.options.validateSearch) {
        search = await route.options.validateSearch(location.searchParams);
        this.current(generation, signal);
      }
      const ctx: RouteContext = {
        params: entry.params,
        search,
        href: location.href,
        pathname: location.pathname,
        signal,
      };
      if (route.options.beforeLoad) {
        await route.options.beforeLoad(ctx);
        this.current(generation, signal);
      }
      let loaderData: unknown;
      const cacheKey = route.id + "\0" + location.href;
      const cached = this.cache.get(cacheKey);
      const fresh = cached !== undefined && this.staleTime > 0 && Date.now() - cached.at < this.staleTime;
      if (fresh && cached) {
        loaderData = cached.data;
      } else if (route.options.loader) {
        loaderData = await route.options.loader(ctx);
        this.current(generation, signal);
        this.cache.set(cacheKey, { data: loaderData, at: Date.now() });
      }
      matches.push({ route, params: entry.params, loaderData, search });
    }
    return matches;
  }
}

export function createRouter(opts: {
  routeTree: AnyRoute;
  history?: MemoryHistory;
  staleTime?: number;
}): Router {
  return new Router(opts);
}

export { matchRoutes };
