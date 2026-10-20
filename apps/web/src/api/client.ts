// The only way the web app reaches the API (FR-INT-01, QA-API-03): a client typed from the
// contract, contracts/openapi/fyniq.v1.yaml. schema.d.ts is generated: after `make openapi`, run
// `pnpm --filter @fyniq/web gen:api`; `make test` fails while it is out of date. A test
// (internal/archtest) refuses hand-written fetch calls anywhere else in the app.
import createClient from "openapi-fetch";
import type { components, paths } from "./schema";

export type Problem = components["schemas"]["Problem"];
export type Organisation = components["schemas"]["Organisation"];
export type Money = components["schemas"]["Money"]; // decimal strings: never parse into a number for arithmetic
export type Session = components["schemas"]["SessionInfo"];
export type SignInProvider = components["schemas"]["SignInProvider"];

// In `make dev` Vite proxies /api to the API; with `make up` and in the cloud one origin serves both.
// The browser sends the session cookie with every call (same origin); the app never reads it.
export const api = createClient<paths>({ baseUrl: "/api" });

// newKey is an Idempotency-Key for one action the person takes (FR-INT-08): send the same key when
// the action is retried, such as after confirming it's you, so it runs at most once. The API saves
// nothing for a refused request, so a retry with the key runs it.
export function newKey(): string {
  return crypto.randomUUID();
}

// etagOf is a response's ETag, to send back in If-Match (FR-INT-09); "" when there is none.
export function etagOf(response: Response | undefined): string {
  return response?.headers.get("ETag") ?? "";
}
