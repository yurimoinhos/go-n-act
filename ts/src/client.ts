// ClientError is a failed procedure call. Message is the public text only.
export class ClientError extends Error {
  readonly code: string;
  readonly status: number;

  constructor(code: string, message: string, status: number) {
    super(message);
    this.name = "ClientError";
    this.code = code;
    this.status = status;
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

// call posts one allowlisted procedure and returns the JSON object.
// A non-JSON body becomes "invalid response" and is not copied into the error.
export async function call<T>(service: string, method: string, input: unknown = {}): Promise<T> {
  const base = config.baseURL;
  const response = await fetch(`${base}/rpc/${service}/${method}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-Route-Request": "1",
    },
    credentials: base === "" ? "same-origin" : "include",
    body: JSON.stringify(input ?? {}),
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
  try {
    const body = JSON.parse(text) as { code?: unknown; message?: unknown };
    if (typeof body.code === "string" && typeof body.message === "string") {
      code = body.code;
      message = body.message;
    }
  } catch {
    // The body is not a public error envelope.
  }
  throw new ClientError(code, message, response.status);
}
