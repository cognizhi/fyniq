// The Fyniq TypeScript SDK (PRD-001 FR-INT-17, p1-public-api task 3.3): a client typed from the /v1 contract
// (contracts/openapi/fyniq.v1.yaml; ./schema.d.ts is generated from it by `pnpm --filter @fyniq/sdk gen`, and `make test`
// fails while it is out of date) with the conventions of §8.18.1 built in:
//
//   - an Idempotency-Key on every state-changing call, kept across retries (FR-INT-08);
//   - a retry of a 429 or 503 after the Retry-After the API names, when that is short (FR-INT-16);
//   - the RFC 9457 problem of a refusal as a FyniqError with its stable code and its fix (FR-INT-07);
//   - the ETag of a read to send back in If-Match (FR-INT-09), and Prefer: return=preview (FR-INT-14);
//   - cursor-paginated lists as one async iterator (FR-INT-10).
//
// It holds no credential of its own: the token is the caller's, an integration client's or a personal access token's
// (FR-INT-02), and is sent to the base URL only. Money stays a decimal string, as the contract has it (ENG-10).
import createClient, { type Client } from "openapi-fetch";

import type { components, paths } from "./schema.js";

export type { components, operations, paths } from "./schema.js";

/** The RFC 9457 problem the API answers a refusal with. */
export type Problem = components["schemas"]["Problem"];

/** newKey is an Idempotency-Key: send the same key when a call is retried, so it runs at most once (FR-INT-08). */
export function newKey(): string {
  return crypto.randomUUID();
}

/** The header that asks a write to check itself and save nothing (FR-INT-14). */
export const previewHeaders = { Prefer: "return=preview" } as const;

export interface FyniqOptions {
  /** The API's origin, such as https://app.example, with or without a trailing slash; paths already start with /v1. */
  baseUrl: string;
  /** The access token, or a function that gets a fresh one at each call. */
  token?: string | (() => string | Promise<string>);
  /** The fetch to use: the platform's by default. */
  fetch?: (request: Request) => Promise<Response>;
  /** How many times a 429 or 503 is retried: 2 by default; 0 never retries. */
  maxRetries?: number;
  /** The longest the client waits to retry, in milliseconds: 10 seconds by default. A longer Retry-After is returned to the caller. */
  maxWaitMs?: number;
  /** Sets Idempotency-Key on every write that has none: on by default. Off, a key is sent only where the caller sends one. */
  idempotencyKeys?: boolean;
  /** Waits, in milliseconds; the timer by default. For tests. */
  sleep?: (ms: number) => Promise<void>;
}

const safe = new Set(["GET", "HEAD", "OPTIONS"]);

/** retryAfterMs is the wait a 429 or 503 asks for in Retry-After (seconds or an HTTP date); undefined when it names none. */
export function retryAfterMs(response: Response, now: number = Date.now()): number | undefined {
  const raw = response.headers.get("Retry-After")?.trim();
  if (!raw) return undefined;
  if (/^\d+$/.test(raw)) return Number(raw) * 1000;
  const at = Date.parse(raw);
  return Number.isNaN(at) ? undefined : Math.max(at - now, 0);
}

/**
 * createFyniqClient is the typed client of the /v1 API. Each call answers { data, error, response } (openapi-fetch); pass
 * the answer to unwrap to get the data or a thrown FyniqError.
 */
export function createFyniqClient(options: FyniqOptions): Client<paths> {
  const base = options.fetch ?? ((request: Request) => fetch(request));
  const maxRetries = options.maxRetries ?? 2;
  const maxWait = options.maxWaitMs ?? 10_000;
  const sleep = options.sleep ?? ((ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms)));
  const send = async (input: Request): Promise<Response> => {
    const headers = new Headers(input.headers);
    const token = typeof options.token === "function" ? await options.token() : options.token;
    if (token) headers.set("Authorization", `Bearer ${token}`);
    if ((options.idempotencyKeys ?? true) && !safe.has(input.method) && !headers.has("Idempotency-Key")) {
      headers.set("Idempotency-Key", newKey());
    }
    const request = new Request(input, { headers });
    for (let attempt = 0; ; attempt++) {
      const response = await base(request.clone());
      if ((response.status !== 429 && response.status !== 503) || attempt >= maxRetries) return response;
      const wait = retryAfterMs(response) ?? Math.min(500 * 2 ** attempt, maxWait);
      if (wait > maxWait) return response;
      await sleep(wait);
    }
  };
  return createClient<paths>({ baseUrl: options.baseUrl.replace(/\/+$/, ""), fetch: send });
}

/** A refusal: the API's problem, with its stable code, its plain-words fix and every invalid field (FR-INT-07, ENG-23). */
export class FyniqError extends Error {
  readonly status: number;
  readonly code: string;
  readonly fix: string;
  readonly problem: Problem;
  readonly requestId: string;

  constructor(status: number, problem: Partial<Problem>, requestId = "") {
    super(problem.detail || problem.title || `The API answered ${status}`);
    this.name = "FyniqError";
    this.status = status;
    this.code = problem.code ?? "";
    this.fix = problem.fix ?? "";
    this.problem = problem as Problem;
    this.requestId = requestId;
  }

  /** The invalid fields, all at once (empty when the problem names none). */
  get fields(): NonNullable<Problem["errors"]> {
    return this.problem.errors ?? [];
  }
}

/** unwrap is the data of an answer, or throws the FyniqError of its problem. */
export function unwrap<T>(result: { data?: T; error?: unknown; response: Response }): T {
  if (result.error !== undefined || !result.response.ok) {
    const e = (typeof result.error === "object" && result.error !== null ? result.error : {}) as Partial<Problem>;
    throw new FyniqError(result.response.status, e, result.response.headers.get("X-Request-Id") ?? "");
  }
  return result.data as T;
}

/** etagOf is a response's ETag, to send back in If-Match (FR-INT-09); "" when there is none. */
export function etagOf(response: Response | undefined): string {
  return response?.headers.get("ETag") ?? "";
}

/** wasPreview reports whether the API only checked the write and saved nothing (FR-INT-14). */
export function wasPreview(response: Response | undefined): boolean {
  return (response?.headers.get("Preference-Applied") ?? "").includes("return=preview");
}

export interface RateLimitState {
  /** The policy's name: reads or writes. */
  policy: string;
  /** What is left of the limit in this window. */
  remaining: number;
  /** The seconds until the window ends. */
  resetSeconds: number;
  /** The limit a window allows; 0 when the answer carried no RateLimit-Policy. */
  quota: number;
}

/** rateLimitOf reads RateLimit and RateLimit-Policy (FR-INT-16) from a response; undefined when it has none. */
export function rateLimitOf(response: Response | undefined): RateLimitState | undefined {
  const limit = response?.headers.get("RateLimit");
  if (!limit) return undefined;
  const field = (s: string, k: string) => Number(new RegExp(`;\\s*${k}=(\\d+)`).exec(s)?.[1] ?? "0");
  const name = /^"?([^";]+)"?/.exec(limit)?.[1] ?? "";
  return {
    policy: name,
    remaining: field(limit, "r"),
    resetSeconds: field(limit, "t"),
    quota: field(response?.headers.get("RateLimit-Policy") ?? "", "q"),
  };
}

/** A page of a cursor-paginated list (FR-INT-10). */
export interface Page<T> {
  items: T[];
  next_cursor?: string;
}

/**
 * paginate yields every item of a list, a page at a time, following next_cursor until the last page:
 *
 *   for await (const c of paginate((cursor) => api.GET("/v1/integration-clients", { params: { query: { cursor } } }).then(unwrap))) { … }
 */
export async function* paginate<T>(page: (cursor: string | undefined) => Promise<Page<T>>): AsyncGenerator<T, void, undefined> {
  let cursor: string | undefined;
  do {
    const p = await page(cursor);
    yield* p.items;
    cursor = p.next_cursor || undefined;
  } while (cursor);
}
