// The only way the web app reaches the API (FR-INT-01, QA-API-03): a client typed from the
// contract, contracts/openapi/fyniq.v1.yaml. schema.d.ts is generated: after `make openapi`, run
// `pnpm --filter @fyniq/web gen:api`; `make test` fails while it is out of date. A test
// (internal/archtest) refuses hand-written fetch calls anywhere else in the app.
import createClient from "openapi-fetch";
import type { components, paths } from "./schema";

export type Problem = components["schemas"]["Problem"];
export type Organisation = components["schemas"]["Organisation"];
export type Money = components["schemas"]["Money"]; // decimal strings: never parse into a number for arithmetic

// In `make dev` Vite proxies /api to the API; with `make up` and in the cloud one origin serves both.
export const api = createClient<paths>({ baseUrl: "/api" });
