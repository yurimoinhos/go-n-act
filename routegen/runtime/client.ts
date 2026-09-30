// ClientError is a failed REST call. Message is the public problem detail.
export class ClientError extends Error {
  readonly code: string;
  readonly status: number;
  readonly title: string;
  readonly type: string;

  constructor(code: string, message: string, status: number, title = "", type = "") {
    super(message);
    this.name = "ClientError";
    this.code = code;
    this.status = status;
    this.title = title;
    this.type = type;
  }
}

type ClientConfig = {
  baseURL: string;
};

let config: ClientConfig = { baseURL: "" };

// configureRouteClient sets the origin used by call.
// An empty baseURL sends a same-origin request. A trailing slash is removed.
export function configureRouteClient(options: { baseURL?: string }): void {
  const base = options.baseURL ?? "";
  config = { baseURL: base.replace(/\/+$/, "") };
}

export type CallOptions = {
  method: string;
  path: string;
  body?: unknown;
  params?: Record<string, string>;
  query?: Record<string, string | number | boolean | Array<string | number | boolean> | undefined | null>;
};

function fillPath(path: string, params?: Record<string, string>): string {
  if (!params) return path;
  return path.replace(/\{([A-Za-z_][A-Za-z0-9_]*)\}/g, (_, name: string) => {
    const value = params[name];
    if (value === undefined) {
      throw new ClientError("invalid_argument", `missing path param ${name}`, 400);
    }
    return encodeURIComponent(value);
  });
}

function buildQuery(
  query?: CallOptions["query"],
): string {
  if (!query) return "";
  const sp = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null) continue;
    if (Array.isArray(value)) {
      for (const item of value) {
        sp.append(key, String(item));
      }
      continue;
    }
    sp.set(key, String(value));
  }
  const encoded = sp.toString();
  return encoded === "" ? "" : `?${encoded}`;
}

// call performs one allowlisted REST request and returns the JSON object.
// A non-JSON success body becomes "invalid response" and is not copied into the error.
export async function call<T>(options: CallOptions): Promise<T> {
  const base = config.baseURL;
  const method = options.method.toUpperCase();
  const path = fillPath(options.path, options.params) + buildQuery(options.query);
  const hasBody = method === "POST" || method === "PUT" || method === "PATCH";
  const headers: Record<string, string> = {
    "X-Route-Request": "1",
  };
  let body: string | undefined;
  if (hasBody) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(options.body ?? {});
  }
  const response = await fetch(`${base}${path}`, {
    method,
    headers,
    credentials: base === "" ? "same-origin" : "include",
    body,
  });
  const text = await response.text();
  if (response.status === 200) {
    if (text === "") return {} as T;
    try {
      return JSON.parse(text) as T;
    } catch {
      throw new ClientError("internal", "invalid response", response.status);
    }
  }
  let code = "internal";
  let message = "invalid response";
  let title = "";
  let type = "";
  try {
    const problem = JSON.parse(text) as {
      code?: unknown;
      detail?: unknown;
      message?: unknown;
      title?: unknown;
      type?: unknown;
    };
    if (typeof problem.code === "string") {
      code = problem.code;
    }
    if (typeof problem.detail === "string") {
      message = problem.detail;
    } else if (typeof problem.message === "string") {
      message = problem.message;
    }
    if (typeof problem.title === "string") {
      title = problem.title;
    }
    if (typeof problem.type === "string") {
      type = problem.type;
    }
  } catch {
    // The body is not a problem details document.
  }
  throw new ClientError(code, message, response.status, title, type);
}
