import { cleanup } from "@testing-library/react";
import { afterEach, beforeEach } from "vitest";

// openapi-fetch builds a Request from a path on this origin, as the browser does; the fake API answers it.
const NodeRequest = globalThis.Request;
globalThis.Request = class extends NodeRequest {
  constructor(input: RequestInfo | URL, init?: RequestInit) {
    super(typeof input === "string" && input.startsWith("/") ? new URL(input, "http://localhost").href : input, init);
  }
} as typeof Request;

type Answer = { status: number; body?: unknown };
let answers = new Map<string, Answer>();
export const calls: string[] = [];

// answer sets what the fake API replies to "GET /v1/session" and the like; an unset call is a 404.
export function answer(route: string, a: Answer): void {
  answers.set(route, a);
}

globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
  const req = input instanceof Request ? input : new Request(input, init);
  const route = `${req.method} ${new URL(req.url).pathname.replace(/^\/api/, "")}`;
  calls.push(route);
  const a = answers.get(route) ?? { status: 404, body: { code: "NM_NOT_FOUND" } };
  return new Response(a.body === undefined ? null : JSON.stringify(a.body), { status: a.status, headers: { "Content-Type": a.status >= 400 ? "application/problem+json" : "application/json" } });
}) as typeof fetch;

beforeEach(() => {
  answers = new Map();
  calls.length = 0;
  window.history.replaceState(null, "", "/");
});

afterEach(() => cleanup());
