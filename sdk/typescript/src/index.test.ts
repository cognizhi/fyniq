import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import {
  createFyniqClient,
  etagOf,
  FyniqError,
  newKey,
  paginate,
  previewHeaders,
  rateLimitOf,
  retryAfterMs,
  unwrap,
  wasPreview,
  type Problem,
} from "./index.js";

// A fake API: each call is recorded, and answers come from the queue.
function fake(...answers: Response[]) {
  const seen: Request[] = [];
  const queue = [...answers];
  const fetch = async (request: Request) => {
    seen.push(request);
    return queue.shift() ?? new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
  };
  return { seen, fetch };
}
const json = (status: number, body: unknown, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json", ...headers } });
const problem = (status: number, p: Partial<Problem>) =>
  json(status, p, { "Content-Type": "application/problem+json" });

const create = { name: "Nightly canary", scopes: ["fyniq.read.reports"], expires_at: "2027-01-01T00:00:00Z", public_key: {} };

// req: FR-INT-17
describe("the TypeScript SDK", () => {
  it("sends the token to the base URL only, and an Idempotency-Key on a write, not on a read", async () => {
    const f = fake();
    const api = createFyniqClient({ baseUrl: "https://app.example/", token: () => "tok-1", fetch: f.fetch });
    await api.GET("/v1/integration-clients", {});
    await api.POST("/v1/integration-clients", { body: create as never });
    const [read, write] = f.seen;
    expect(read?.url).toBe("https://app.example/v1/integration-clients");
    expect(read?.headers.get("Authorization")).toBe("Bearer tok-1");
    expect(read?.headers.get("Idempotency-Key")).toBeNull();
    expect(write?.headers.get("Idempotency-Key")).toMatch(/^[0-9a-f-]{36}$/);
  });

  it("keeps the caller's own Idempotency-Key, and none when keys are off", async () => {
    const f = fake();
    const api = createFyniqClient({ baseUrl: "https://app.example", fetch: f.fetch });
    await api.POST("/v1/integration-clients", { body: create as never, params: { header: { "Idempotency-Key": "mine-1" } } as never });
    expect(f.seen[0]?.headers.get("Idempotency-Key")).toBe("mine-1");
    expect(f.seen[0]?.headers.get("Authorization")).toBeNull();
    const off = fake();
    await createFyniqClient({ baseUrl: "https://app.example", fetch: off.fetch, idempotencyKeys: false }).POST("/v1/integration-clients", {
      body: create as never,
    });
    expect(off.seen[0]?.headers.get("Idempotency-Key")).toBeNull();
  });

  it("retries a 429 after its Retry-After with the same key and body, and gives up after maxRetries", async () => {
    const waits: number[] = [];
    const f = fake(problem(429, { code: "API-RATE-01" }), problem(429, { code: "API-RATE-01" }), json(202, { id: "x" }));
    const api = createFyniqClient({ baseUrl: "https://app.example", fetch: f.fetch, sleep: async (ms) => void waits.push(ms) });
    const res = await api.POST("/v1/integration-clients", { body: create as never });
    expect(res.response.status).toBe(202);
    expect(f.seen).toHaveLength(3);
    expect(new Set(f.seen.map((r) => r.headers.get("Idempotency-Key"))).size).toBe(1);
    expect(await f.seen[2]?.json()).toMatchObject({ name: "Nightly canary" });
    expect(waits).toEqual([500, 1000]); // no Retry-After: back off

    const waited: number[] = [];
    const slow = fake(json(429, {}, { "Retry-After": "2" }), json(429, {}, { "Retry-After": "2" }));
    const r2 = await createFyniqClient({ baseUrl: "https://app.example", fetch: slow.fetch, maxRetries: 1, sleep: async (ms) => void waited.push(ms) }).GET(
      "/v1/integration-clients",
      {},
    );
    expect(r2.response.status).toBe(429);
    expect(waited).toEqual([2000]);
    expect(slow.seen).toHaveLength(2);
  });

  it("returns a 429 whose Retry-After is longer than it will wait, without sleeping", async () => {
    const f = fake(json(429, {}, { "Retry-After": "120" }));
    const api = createFyniqClient({ baseUrl: "https://app.example", fetch: f.fetch, sleep: async () => expect.unreachable() });
    expect((await api.GET("/v1/integration-clients", {})).response.status).toBe(429);
    expect(f.seen).toHaveLength(1);
  });

  it("retries a read's 503 but never a write's, whose outcome is unknown and which may take no Idempotency-Key", async () => {
    const reads = fake(json(503, {}, { "Retry-After": "1" }), json(200, { items: [] }));
    const r = await createFyniqClient({ baseUrl: "https://app.example", fetch: reads.fetch, sleep: async () => {} }).GET("/v1/integration-clients", {});
    expect([r.response.status, reads.seen.length]).toEqual([200, 2]);
    const writes = fake(json(503, {}, { "Retry-After": "1" }), json(202, {}));
    const w = await createFyniqClient({ baseUrl: "https://app.example", fetch: writes.fetch, sleep: async () => {} }).POST("/v1/integration-clients", { body: create as never });
    expect([w.response.status, writes.seen.length]).toEqual([503, 1]);
  });

  it("never retries a refusal that is not 429 or a read's 503", async () => {
    const f = fake(problem(422, { code: "X-01" }));
    await createFyniqClient({ baseUrl: "https://app.example", fetch: f.fetch }).GET("/v1/integration-clients", {});
    expect(f.seen).toHaveLength(1);
  });

  it("throws the problem of a refusal with its code, its fix and every field at once", async () => {
    const f = fake(
      problem(422, { status: 422, code: "JOURNAL-02", detail: "The journal is posted, not a draft.", fix: "Reverse it instead.", errors: [{ code: "A", field: "body.a", message: "m", fix: "f" }] as never }),
    );
    const api = createFyniqClient({ baseUrl: "https://app.example", fetch: f.fetch });
    const answer = await api.GET("/v1/integration-clients", {});
    let err: unknown;
    try {
      unwrap(answer);
    } catch (e) {
      err = e;
    }
    expect(err).toBeInstanceOf(FyniqError);
    const e = err as FyniqError;
    expect([e.status, e.code, e.fix, e.message, e.fields]).toEqual([422, "JOURNAL-02", "Reverse it instead.", "The journal is posted, not a draft.", [{ code: "A", field: "body.a", message: "m", fix: "f" }]]);
  });

  it("unwraps the data of a good answer", async () => {
    const f = fake(json(200, { items: [] }));
    const data = unwrap(await createFyniqClient({ baseUrl: "https://app.example", fetch: f.fetch }).GET("/v1/integration-clients", {}));
    expect(data).toEqual({ items: [] });
  });

  it("reads ETag, the preview marker and the rate limit from a response", () => {
    const r = new Response("{}", { headers: { ETag: '"v7"', "Preference-Applied": "return=preview", RateLimit: '"writes";r=119;t=42', "RateLimit-Policy": '"writes";q=120;w=60' } });
    expect(etagOf(r)).toBe('"v7"');
    expect(etagOf(undefined)).toBe("");
    expect(wasPreview(r)).toBe(true);
    expect(wasPreview(new Response("{}"))).toBe(false);
    expect(rateLimitOf(r)).toEqual({ policy: "writes", remaining: 119, resetSeconds: 42, quota: 120 });
    expect(rateLimitOf(new Response("{}"))).toBeUndefined();
    expect(previewHeaders).toEqual({ Prefer: "return=preview" });
  });

  it("reads Retry-After as seconds or as a date", () => {
    expect(retryAfterMs(new Response("", { headers: { "Retry-After": "3" } }))).toBe(3000);
    expect(retryAfterMs(new Response("", { headers: { "Retry-After": "Wed, 18 Nov 2026 00:00:10 GMT" } }), Date.parse("Wed, 18 Nov 2026 00:00:00 GMT"))).toBe(10_000);
    expect(retryAfterMs(new Response(""))).toBeUndefined();
  });

  it("walks every page of a cursor list", async () => {
    const pages: Record<string, { items: number[]; next_cursor?: string }> = { first: { items: [1, 2], next_cursor: "c2" }, c2: { items: [3], next_cursor: "c3" }, c3: { items: [4] } };
    const asked: (string | undefined)[] = [];
    const got: number[] = [];
    for await (const n of paginate<number>(async (cursor) => (asked.push(cursor), pages[cursor ?? "first"]!))) got.push(n);
    expect(got).toEqual([1, 2, 3, 4]);
    expect(asked).toEqual([undefined, "c2", "c3"]);
  });

  it("makes a fresh key each time", () => {
    expect(newKey()).not.toBe(newKey());
  });

  it("is packed with its compiled client and the generated types, and the web app takes its types from it", () => {
    const root = join(import.meta.dirname, "..");
    const dir = mkdtempSync(join(tmpdir(), "fyniq-sdk-"));
    try {
      const out = execFileSync("pnpm", ["pack", "--pack-destination", dir, "--json"], { cwd: root, encoding: "utf8" });
      const packed = JSON.parse(out.slice(out.indexOf("{"))) as { filename: string; files: { path: string }[] };
      const files = packed.files.map((f) => f.path);
      expect(files).toEqual(expect.arrayContaining(["package.json", "README.md", "dist/index.js", "dist/index.d.ts", "dist/schema.d.ts"]));
      expect(files.some((f) => f.startsWith("src/") || f.endsWith(".test.ts"))).toBe(false);
      const manifest = JSON.parse(execFileSync("tar", ["-xzOf", packed.filename, "package/package.json"], { encoding: "utf8" })) as {
        exports: Record<string, { types: string; default?: string }>;
      };
      expect(manifest.exports["."]).toEqual({ types: "./dist/index.d.ts", default: "./dist/index.js" });
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
    const shim = readFileSync(join(root, "../../apps/web/src/api/schema.ts"), "utf8");
    expect(shim).toContain('"@fyniq/sdk/schema"');
  });
});
