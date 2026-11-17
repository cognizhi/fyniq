import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { expect, type Page, type Route } from "@playwright/test";

// The shell screens of p1-app-shell against the built web app in a real browser, every /api call answered by a fixture
// (the Go suites prove the API): an Owner with work waiting, a statement to trace, invoices, search results and their
// own security settings. A read no fixture names is refused with 403, as the API refuses a page the person's roles
// cannot read, so each page shows its own refusal rather than failing. Shared by devices.spec.ts (FR-UX-37),
// accessibility.spec.ts (NFR-A11Y-01 to 05) and performance.spec.ts (NFR-PERF-UI-01, 03).

export type Answer = { status: number; body?: unknown; headers?: Record<string, string> };
export type Handler = Answer | ((url: URL, body: unknown) => Answer);

export const session = {
  provider: "local",
  user_id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01",
  organisation_id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02",
  roles: ["owner", "controller", "administrator"],
  super_admin: false,
  step_up_method: "sign_in",
  stepped_up_until: "2099-10-20T06:05:00Z",
  features: { multi_currency: true, payroll: false, projects: false, portals: false, ai_assistant: false },
};
export const organisation = { id: session.organisation_id, name: "Cognizhi Pte Ltd", base_currency: "SGD", base_minor_units: 2, team_mode: "founder", version: 1 };

const m = (amount: string) => ({ amount, currency: "SGD" });
const ok = (body: unknown): Answer => ({ status: 200, body });
const empty = ok({ items: [] });

export const request = {
  id: "a-1",
  object_type: "organisation_setting",
  object_key: "retention.records_years",
  operation: "organisation_setting.change",
  summary: "Keep records for 10 years",
  policy: "founder/organisation_setting@2026-10-20.1",
  version: 2,
  status: "pending",
  requested_at: "2026-10-20T01:00:00Z",
  requested_by: "someone-else",
  requested_by_name: "Ben Ong",
  expires_at: "2099-10-27T01:00:00Z",
  current_step: 1,
  steps: [{ quorum: 1, roles: ["owner"], users: [], approvals: 0 }],
  signatures: [],
  diff: [{ field: "Records kept for", before: "7 years", after: "10 years" }],
  envelope: "{}",
  envelope_hash: "cd".repeat(32),
  you_may_approve: true,
};

const periods = ["2026-09", "2026-10"].map((x, i) => ({ id: `p${i}`, starts_on: `${x}-01`, ends_on: `${x}-${i ? 31 : 30}`, status: i ? "open" : "closed", financial_year: 1, version: 1 }));

// A P&L by month, wide enough that a phone scrolls it inside its own region (FR-UX-37).
const months = ["2026-05", "2026-06", "2026-07", "2026-08", "2026-09", "2026-10"];
const line = (code: string, name: string, base: number) => ({
  account_id: `a-${code}`,
  code,
  name,
  amounts: [...months.map((_, i) => m(`${base + i * 100}.00`)), m(`${months.length * base + 1500}.00`)],
  percents: [],
});
export const statement = {
  kind: "pl",
  statement_title: "Management statements",
  currency: "SGD",
  from: "2026-05-01",
  to: "2026-10-31",
  ledger_revision: 42,
  restated_presentation: false,
  platform_version: "0.1.0",
  columns: [...months.map((x) => ({ key: x, from: `${x}-01`, to: `${x}-28` })), { key: "total", from: "2026-05-01", to: "2026-10-31" }],
  checks: [],
  non_cash: [],
  blocks: [
    {
      key: "revenue",
      title: "Revenue",
      measure: false,
      totals: [...months.map((_, i) => m(`${41000 + i * 1000}.00`)), m("261000.00")],
      percents: [],
      groups: [{ name: "Subscription revenue", totals: [...months.map((_, i) => m(`${41000 + i * 1000}.00`)), m("261000.00")], percents: [], lines: [line("4010", "Subscription revenue", 41000)] }],
    },
    {
      key: "operating_expenses",
      title: "Operating expenses",
      measure: false,
      totals: [...months.map((_, i) => m(`${62000 + i * 100}.00`)), m("373500.00")],
      percents: [],
      groups: [
        {
          name: "Software",
          totals: [...months.map((_, i) => m(`${389 + i * 100}.30`)), m("3834.80")],
          percents: [],
          lines: [line("6120", "Software subscriptions", 389), line("6130", "Cloud hosting", 1200)],
        },
      ],
    },
    { key: "net_profit", title: "Net profit", measure: true, totals: [...months.map((_, i) => m(`-${21000 - i * 900}.00`)), m("-112500.00")], percents: [], groups: [] },
  ],
};

const ledger = {
  from: "2026-05-01",
  to: "2026-10-31",
  currency: "SGD",
  ledger_revision: 42,
  accounts: [
    {
      account_id: "a-6120",
      code: "6120",
      name: "Software subscriptions",
      opening: m("0.00"),
      debits: m("389.30"),
      credits: m("0.00"),
      closing: m("389.30"),
      lines: [
        { posting_date: "2026-09-05", entry_id: "e1", ledger_revision: 40, source_type: "bill", source_id: "b905", source_version: 1, number: "BILL-0905", reversal: false,
          memo: "GitHub Team, September", amount: { amount: "290.00", currency: "USD" }, base: m("389.30"), balance: m("389.30") },
      ],
    },
  ],
};

const states = (extra: object = {}) => ({ approval: "approved", ledger: "posted", delivery: "not_sent", settlement: "open", due: "not_due", ...extra });
const invoice = (n: number, extra: object = {}) => ({
  id: `v${n}`,
  number: `INV-00${40 + n}`,
  kind: "invoice",
  opening_item: false,
  status: "posted",
  version: 1,
  customer: { id: "c1", name: "Acme Analytics Pte Ltd" },
  reference: `PO-882${n}`,
  date: "2026-10-14",
  due_date: "2026-11-13",
  currency: "SGD",
  lines: [],
  gst_charged: true,
  gst_subtotals: [],
  net: m("3000.00"),
  gst: m("270.00"),
  total: m("3270.00"),
  deposits: [],
  receivable: m("3270.00"),
  open: m("3270.00"),
  states: states(extra),
  releases: [],
  checks: [],
  ledger_impact: [],
  sends: [],
  links: [],
});

// The fixtures of the shell screens, by "METHOD /v1/path".
export const fixtures: Record<string, Handler> = {
  "GET /v1/setup": ok({ done: true }),
  "GET /v1/session": ok(session),
  "GET /v1/organisation": ok(organisation),
  "GET /v1/organisation/brand": ok({ version: 0, accent_options: [] }),
  "GET /v1/banners": empty,
  "GET /v1/operations": empty,
  "GET /v1/report-exports": empty,
  "GET /v1/notifications": ok({
    items: [{ id: "n1", type: "approval.requested", title: "Ben Ong asks you to approve: Keep records for 10 years", link: "/approvals/a-1", created_at: "2026-10-20T01:00:00Z", read: false }],
    unread: 1,
  }),
  "GET /v1/platform/version": ok({ version: "0.1.0" }),
  "GET /v1/approvals": ok({ items: [request] }),
  "GET /v1/approvals/a-1": { status: 200, body: request, headers: { ETag: '"2"' } },
  "GET /v1/review-log": ok({ items: [], unreviewed: 0, overdue: 0 }),
  "GET /v1/journals/waiting-reversals": empty,
  "GET /v1/organisation/profile": ok({ legal_name: "Cognizhi Pte Ltd", uen: "202612345K", fy_start_month: 1, time_zone: "Asia/Singapore", version: 1 }),
  "GET /v1/periods": ok({ items: periods }),
  "GET /v1/opening-balances": ok({ status: "none", version: 0, lines: [] }),
  "GET /v1/accounts": ok({
    items: [
      { id: "a-1010", code: "1010", name: "DBS current", type: "asset", active: true, group: false, version: 1, money_account: true, currency: "SGD" },
      { id: "a-4010", code: "4010", name: "Subscription revenue", type: "revenue", active: true, group: false, version: 1 },
      { id: "a-6120", code: "6120", name: "Software subscriptions", type: "expense", active: true, group: false, version: 1 },
    ],
  }),
  "GET /v1/contacts": empty,
  "GET /v1/dimensions": empty,
  "GET /v1/dimension-values": empty,
  "GET /v1/members": ok({ items: [{ id: "m1" }] }),
  "GET /v1/members/invitations": empty,
  "GET /v1/session/passkeys": empty,
  "GET /v1/session/devices": empty,
  "GET /v1/session/identities": empty,
  "GET /v1/sign-in-providers": ok({ items: [{ id: "local", name: "Local sign-in (development)" }] }),
  "GET /v1/reports/statements/pl": ok(statement),
  "GET /v1/reports/general-ledger": ok(ledger),
  "GET /v1/search": (url) =>
    ok({
      query: url.searchParams.get("q") ?? "",
      results: [
        { group: "invoices", type: "invoice", id: "v1", title: "INV-0041 · Acme Analytics Pte Ltd", detail: "PO-8821", date: "2026-10-14", status: "posted", amount: m("3270.00"), link: "/sales/invoices/v1" },
        { group: "contacts", type: "contact", id: "c1", title: "Acme Analytics Pte Ltd", detail: "202312345K", link: "/contacts/c1" },
      ],
      groups: [
        { key: "invoices", label: "Invoices and credit notes", count: 1, more: false },
        { key: "contacts", label: "Contacts", count: 1, more: true },
      ],
    }),
  "GET /v1/invoices": ok({ items: [invoice(1, { delivery: "sent" }), invoice(2, { due: "overdue" }), invoice(3, { settlement: "settled" })] }),
  "GET /v1/me/option-grants": empty,
};

// serve answers the page's /api calls from the fixtures, with `extra` over them; a read no fixture names gets 403. It
// returns what the page asked for, as "METHOD /v1/path".
// asAccountant signs the person in as an Accountant alone, who prepares journals (Appendix D).
export const asAccountant: Record<string, Handler> = { "GET /v1/session": ok({ ...session, roles: ["accountant"] }) };

export async function serve(page: Page, extra: Record<string, Handler> = {}): Promise<string[]> {
  const seen: string[] = [];
  const all = { ...fixtures, ...extra };
  await page.route("**/api/v1/**", async (route: Route) => {
    const req = route.request();
    const url = new URL(req.url());
    const key = `${req.method()} ${decodeURIComponent(url.pathname).replace(/^\/api/, "")}`;
    seen.push(key);
    const h = all[key];
    const body = req.postData() ? (JSON.parse(req.postData() as string) as unknown) : undefined;
    const a: Answer = h ? (typeof h === "function" ? h(url, body) : h) : { status: 403, body: { type: "about:blank", title: "Forbidden", status: 403, code: "API-AUTHZ-01", detail: "Your roles do not include this.", fix: "Ask an Administrator." } };
    await route.fulfill({ status: a.status, headers: a.headers, contentType: a.status >= 400 ? "application/problem+json" : "application/json", body: a.body === undefined ? "" : JSON.stringify(a.body) });
  });
  return seen;
}

// signedOut answers as the API does for a browser with no session.
export const signedOut: Record<string, Handler> = {
  "GET /v1/session": { status: 401, body: { code: "API-AUTH-01", status: 401, title: "Unauthorized" } },
};

// The shell screens (p1-app-shell's 18 as built; docs/design/screens): each with its address, the mode it is shown in,
// and what shows once it has loaded.
export interface Screen {
  name: string;
  path: string;
  mode?: "basic" | "expert";
  ready: (page: Page) => Promise<void>;
  open?: (page: Page) => Promise<void>; // what opens over the page, such as a menu or a panel
  extra?: Record<string, Handler>;
}

const h1 = (name: string | RegExp) => async (page: Page) => {
  await expect(page.getByRole("heading", { level: 1, name })).toBeVisible();
};

export const screens: Screen[] = [
  { name: "Home (app shell, set-up checklist)", path: "/", ready: async (page) => void (await expect(page.getByRole("list", { name: "Set up the company" })).toBeVisible()) },
  { name: "Inbox", path: "/inbox", ready: async (page) => void (await expect(page.getByRole("list", { name: "To approve" })).toBeVisible()) },
  { name: "An approval request", path: "/approvals/a-1", ready: h1("Keep records for 10 years") },
  { name: "Global search", path: "/search?q=acme", ready: async (page) => void (await expect(page.getByRole("list", { name: "Search results" })).toBeVisible()) },
  { name: "Invoices list", path: "/sales/invoices", ready: async (page) => void (await expect(page.getByRole("table", { name: "Invoices" })).toBeVisible()) },
  { name: "A financial statement", path: "/reports/statements/pl", mode: "expert", ready: async (page) => void (await expect(page.getByRole("table", { name: /Profit and loss/ })).toBeVisible()) },
  { name: "Journal editor", path: "/accounting/journals/new", mode: "expert", extra: asAccountant, ready: async (page) => void (await expect(page.getByLabel("Description").first()).toBeVisible()) },
  { name: "My shares", path: "/me/shares", ready: h1(/shares/i) },
  { name: "Settings", path: "/settings", ready: h1("Settings") },
  { name: "Profile and security", path: "/settings/profile", ready: h1(/Profile/) },
  { name: "A page your roles cannot read", path: "/accounting/fx", mode: "expert", ready: async (page) => void (await expect(page.getByText(/API-AUTHZ-01|not part of your access|roles do not/i).first()).toBeVisible()) },
  { name: "No page here", path: "/no-such-page", ready: h1("There is no page here") },
];

// withMode starts the browser in a mode, as the header's switch keeps it.
export async function withMode(page: Page, mode: "basic" | "expert" = "basic"): Promise<void> {
  await page.addInitScript((m) => window.localStorage.setItem("fyniq.mode", m), mode);
}

// open serves the fixtures, sets the mode and opens a screen, waiting until it has loaded.
export async function open(page: Page, s: Screen): Promise<string[]> {
  const seen = await serve(page, s.extra);
  await withMode(page, s.mode);
  await page.goto(s.path);
  await s.ready(page);
  if (s.open) await s.open(page);
  return seen;
}

// axe-core's own build, injected into the page: the same rules the component tests run (src/test/axe.ts), here with
// layout and colour, so contrast is checked as drawn (NFR-A11Y-01).
const axeSource = readFileSync(createRequire(import.meta.url).resolve("axe-core/axe.min.js"), "utf8");

export async function axe(page: Page, include = "body"): Promise<string[]> {
  await page.addScriptTag({ content: axeSource });
  return page.evaluate(async (sel) => {
    const run = (window as unknown as { axe: { run: (n: Element, o: object) => Promise<{ violations: { id: string; help: string; nodes: { target: string[] }[] }[] }> } }).axe.run;
    const r = await run(document.querySelector(sel) ?? document.body, {
      runOnly: { type: "tag", values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa", "best-practice"] },
      rules: { region: { enabled: false } },
    });
    return r.violations.map((v) => `${v.id}: ${v.help} (${v.nodes.map((n) => n.target.join(" ")).join("; ")})`);
  }, include);
}
