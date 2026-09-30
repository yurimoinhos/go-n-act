export type RouteKind = "root" | "layout" | "index" | "leaf" | "splat" | "pathless";

export type Matchable = {
  id: string;
  path: string;
  kind: RouteKind;
  segments: string[];
  parent?: Matchable;
  children: Matchable[];
};

export type Matched = {
  route: Matchable;
  params: Record<string, string>;
};

export function routeSegments(path: string): string[] {
  const trimmed = path.length > 1 && path.endsWith("/") ? path.slice(0, -1) : path;
  if (trimmed === "" || trimmed === "/") return [];
  return trimmed.split("/").filter((part) => part.length > 0);
}

export function inferKind(id: string, path: string): RouteKind {
  if (id === "__root") return "root";
  const segments = routeSegments(path);
  if (segments.length > 0 && segments.every(isPathless)) return "pathless";
  if (segments.length > 0 && segments[segments.length - 1] === "$") return "splat";
  if (path === "/" || (path.length > 1 && path.endsWith("/"))) return "index";
  return "leaf";
}

export function isPathless(segment: string): boolean {
  return segment.startsWith("_") && !segment.startsWith("__") && segment !== "_";
}

function pathnameParts(pathname: string): string[] {
  const trimmed = pathname.length > 1 && pathname.endsWith("/") ? pathname.slice(0, -1) : pathname;
  if (trimmed === "" || trimmed === "/") return [];
  return trimmed.split("/").filter((part) => part.length > 0);
}

function exact(route: Matchable, parts: string[]): boolean {
  if (route.kind === "root") return parts.length === 0;
  let index = 0;
  const segments = route.segments;
  for (let i = 0; i < segments.length; i++) {
    const segment = segments[i] ?? "";
    if (isPathless(segment)) continue;
    if (segment === "$") return i === segments.length - 1;
    if (index >= parts.length) return false;
    if (segment.startsWith("$")) {
      index++;
      continue;
    }
    if (segment !== parts[index]) return false;
    index++;
  }
  return index === parts.length;
}

function scoreOf(route: Matchable): number {
  if (route.kind === "root") return 0;
  let score = 0;
  for (const segment of route.segments) {
    if (isPathless(segment)) continue;
    if (segment === "$") score += 1;
    else if (segment.startsWith("$")) score += 3;
    else score += 10;
  }
  if (route.kind === "index") score += 1;
  return score;
}

function paramsFor(route: Matchable, parts: string[]): Record<string, string> {
  if (route.kind === "root" || route.kind === "pathless") return {};
  const params: Record<string, string> = {};
  let index = 0;
  for (let i = 0; i < route.segments.length; i++) {
    const segment = route.segments[i] ?? "";
    if (isPathless(segment)) continue;
    if (segment === "$") {
      params["*"] = parts.slice(index).join("/");
      return params;
    }
    if (index >= parts.length) break;
    if (segment.startsWith("$")) params[segment.slice(1)] = parts[index] ?? "";
    index++;
  }
  return params;
}

function chain(leaf: Matchable): Matchable[] {
  const out: Matchable[] = [];
  let current: Matchable | undefined = leaf;
  while (current) {
    out.push(current);
    current = current.parent;
  }
  out.reverse();
  return out;
}

// matchRoutes picks one leaf for pathname and returns the parent chain, root first.
// Static segments score 10, parameters score 3, a splat scores 1, and an index scores one more.
// Equal scores keep the lexicographically smaller id. Pathless routes are never leaves.
export function matchRoutes(root: Matchable, pathname: string): Matched[] | null {
  const parts = pathnameParts(pathname);
  let best: Matchable | null = null;
  let bestScore = -1;
  const visit = (route: Matchable) => {
    if (route.kind !== "pathless" && exact(route, parts)) {
      const score = scoreOf(route);
      if (best === null || score > bestScore || (score === bestScore && route.id < best.id)) {
        best = route;
        bestScore = score;
      }
    }
    for (const child of route.children) visit(child);
  };
  visit(root);
  if (best === null) return null;
  return chain(best).map((route) => ({ route, params: paramsFor(route, parts) }));
}
