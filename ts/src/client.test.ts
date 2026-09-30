import { afterEach, expect, test } from "bun:test";
import { ClientError, call, configureRouteClient } from "./client.ts";

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
  configureRouteClient({ baseURL: "" });
});

function mockFetch(status: number, body: string) {
  const seen: { url: string; init: RequestInit }[] = [];
  globalThis.fetch = (async (url: string | URL | Request, init?: RequestInit) => {
    seen.push({ url: String(url), init: init ?? {} });
    return new Response(body, { status });
  }) as typeof fetch;
  return seen;
}

test("call posts json to the rest path", async () => {
  const seen = mockFetch(200, "");
  const out = await call({ method: "POST", path: "/clients", body: { page: 1 } });
  expect(out).toEqual({});
  expect(seen[0]?.url).toBe("/clients");
  expect(seen[0]?.init.method).toBe("POST");
  expect(seen[0]?.init.credentials).toBe("same-origin");
  expect(seen[0]?.init.body).toBe('{"page":1}');
  const headers = seen[0]?.init.headers as Record<string, string>;
  expect(headers["Content-Type"]).toBe("application/json");
  expect(headers["X-Route-Request"]).toBe("1");
});

test("configureRouteClient strips the trailing slash and includes credentials", async () => {
  configureRouteClient({ baseURL: "https://api.example/" });
  const seen = mockFetch(200, '{"name":"Ada"}');
  const out = await call<{ name: string }>({ method: "POST", path: "/about" });
  expect(out).toEqual({ name: "Ada" });
  expect(seen[0]?.url).toBe("https://api.example/about");
  expect(seen[0]?.init.credentials).toBe("include");
  expect(seen[0]?.init.body).toBe("{}");
});

test("get fills path params and query", async () => {
  const seen = mockFetch(200, '{"id":"42"}');
  await call({
    method: "GET",
    path: "/clients/{id}",
    params: { id: "42" },
    query: { extra: "hi" },
  });
  expect(seen[0]?.url).toBe("/clients/42?extra=hi");
  expect(seen[0]?.init.method).toBe("GET");
  expect(seen[0]?.init.body).toBeUndefined();
});

test("a problem details error keeps code and detail", async () => {
  mockFetch(
    400,
    '{"type":"urn:gnact:error:invalid_argument","title":"Bad Request","status":400,"detail":"missing field id","code":"invalid_argument"}',
  );
  try {
    await call({ method: "GET", path: "/clients/{id}", params: { id: "x" } });
    throw new Error("expected failure");
  } catch (error) {
    expect(error).toBeInstanceOf(ClientError);
    const client = error as ClientError;
    expect(client.code).toBe("invalid_argument");
    expect(client.message).toBe("missing field id");
    expect(client.status).toBe(400);
    expect(client.title).toBe("Bad Request");
    expect(client.type).toBe("urn:gnact:error:invalid_argument");
  }
});

test("a non-json error does not echo the body", async () => {
  const secret = "secret-token-do-not-echo";
  mockFetch(500, `<html>${secret}</html>`);
  try {
    await call({ method: "POST", path: "/about", body: {} });
    throw new Error("expected failure");
  } catch (error) {
    expect(error).toBeInstanceOf(ClientError);
    const client = error as ClientError;
    expect(client.message).toBe("invalid response");
    expect(client.code).toBe("internal");
    expect(client.message.includes(secret)).toBe(false);
    expect(JSON.stringify(client).includes(secret)).toBe(false);
  }
});
