// A fake API for component tests: the generated client (api/client.ts) calls fetch, and setup.ts
// points fetch here before any module creates the client. A test lists the answers it expects
// ("METHOD /v1/path"); anything else gets status 599, so a stray call fails loudly.

export interface Answer {
  status: number;
  body?: unknown;
  headers?: Record<string, string>;
}

export type Handler = Answer | ((body: unknown, req: Request) => Answer);

export interface Seen {
  method: string;
  path: string;
  query: string;
  body: unknown;
  headers: Headers;
}

let handlers: Record<string, Handler> = {};
export const seen: Seen[] = [];

// answer sets the fake API's answers for this test and clears what it saw.
export function answer(h: Record<string, Handler>): Seen[] {
  handlers = h;
  seen.length = 0;
  return seen;
}

// calls lists what the page sent, as "METHOD /v1/path".
export function calls(): string[] {
  return seen.map((s) => `${s.method} ${s.path}`);
}

export async function fakeFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const req = input instanceof Request ? input : new Request(input, init);
  const url = new URL(req.url);
  const path = decodeURIComponent(url.pathname).replace(/^\/api/, "");
  const text = await req.text();
  const body: unknown = text ? JSON.parse(text) : undefined;
  seen.push({ method: req.method, path, query: url.search, body, headers: req.headers });
  const h = handlers[`${req.method} ${path}`];
  if (!h) return new Response(`no fixture for ${req.method} ${path}`, { status: 599 });
  const a = typeof h === "function" ? h(body, req) : h;
  return new Response(a.body === undefined ? null : JSON.stringify(a.body), {
    status: a.status,
    headers: { "Content-Type": a.status >= 400 ? "application/problem+json" : "application/json", ...a.headers },
  });
}

// problem is an RFC 9457 answer, as the API sends a refusal.
export function problem(status: number, code: string, detail = "Refused.", fix = "Fix it."): Answer {
  return { status, body: { type: "about:blank", title: "Refused", status, code, detail, fix } };
}

// A session as GET /v1/session returns it: an Owner who passed a second factor at sign-in.
export const ownerSession = {
  provider: "local",
  user_id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01",
  organisation_id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02",
  roles: ["owner", "controller", "administrator"],
  super_admin: false,
  step_up_method: "sign_in" as const,
  stepped_up_until: "2026-10-20T06:05:00Z",
  features: { multi_currency: true, payroll: false, projects: false, portals: false, ai_assistant: false },
};
