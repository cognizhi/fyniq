import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, calls, ownerSession, problem, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Dashboard } from "../Dashboard";
import { Checks } from "./Checks";

type ChecksOut = components["schemas"]["ChecksOut"];
type CheckOut = components["schemas"]["CheckOut"];
type KPIs = components["schemas"]["KPIs"];

const controller = { ...ownerSession, roles: ["controller"] } as Session;
const bookkeeper = { ...ownerSession, roles: ["bookkeeper"] } as Session;

const check = (key: string, area: string, status: CheckOut["status"], more: Partial<CheckOut> = {}): CheckOut => ({
  key,
  ref: `C.1 #${key.length}`,
  area,
  worst: "error",
  title: `Title of ${key}`,
  explain: `Why ${key} matters`,
  rules: "INV-01",
  nightly: false,
  status,
  count: 0,
  items: [],
  items_hidden: false,
  items_area: "Reports, dashboards",
  detail: `What ${key} found`,
  ledger_revision: 42,
  run_at: "2026-11-01T20:15:00Z",
  trigger: "nightly",
  owner: { role: "controller", version: 0 },
  ...more,
});

// What GET /v1/checks answers: the server decides each status, the items a reader opens and books health (FR-UX-22).
const answered: ChecksOut = {
  health: { score: "problems", problems: 1, warnings: 1, due: 0, ok: 4, not_assessed: 1 },
  may_name_owners: true,
  may_review: true,
  run_requested: false,
  last_run: { trigger: "nightly", started_at: "2026-11-01T20:15:00Z", finished_at: "2026-11-01T20:15:03Z", ledger_revision: 42, checks: 7 },
  last_nightly_run: { trigger: "nightly", started_at: "2026-11-01T20:15:00Z", finished_at: "2026-11-01T20:15:03Z", ledger_revision: 42, checks: 7 },
  checks: [
    check("books.balance-sheet-balances", "Books", "error", {
      title: "The balance sheet balances every month",
      count: 2,
      detail: "2 month(s) whose balance sheet does not balance",
      fix: "Report it: the ledger's own checks should have refused this",
      items: [
        { kind: "period", id: "p-oct", label: "October 2026", detail: "Debits and credits differ by 0.01 SGD" },
        { kind: "period", id: "p-nov", label: "November 2026", detail: "Debits and credits differ by 0.01 SGD" },
      ],
    }),
    check("books.debits-equal-credits", "Books", "ok", { title: "Debits equal credits", detail: "The trial balance of every open month nets to zero" }),
    check("books.cash-flow-ties", "Books", "ok", { title: "The cash flow agrees to the bank balances" }),
    check("setup.system-accounts-exist", "Setup", "warning", {
      title: "The system accounts exist",
      count: 24,
      items_hidden: true,
      items_area: "Chart of accounts, dimensions",
      detail: "24 system role(s) on no active account",
      owner: { role: "controller", version: 1, person: { kind: "user", id: "u-mei", name: "Mei Lin Tan" } },
    }),
    check("setup.opening-balances-balance", "Setup", "ok", { title: "The opening balances balance" }),
    check("purchases.bills-to-review", "Purchases", "warning", {
      title: "No posted bill waits for a review",
      count: 1,
      items_area: "Bills, vendor credits",
      detail: "1 posted bill(s) to review",
      items: [{ kind: "bill", id: "b1", label: "BL-0001 of 2026-10-14, Atlassian Pty Ltd", detail: "the stated GST 8.00 SGD differs by 1.00 SGD", version: "2" }],
    }),
    check("ledger.balances-match-lines", "Ledger", "ok", { title: "The balances equal their lines", nightly: true, detail: "Every balance equals its recomputation" }),
    check("audit.chain-intact", "Audit", "not_assessed", {
      title: "The audit trail is intact and anchored",
      nightly: true,
      needs: "the audit trail's nightly verification",
      setting: "the platform's schedule",
      detail: "Not assessed: it needs the audit trail's nightly verification",
      run_at: undefined,
    }),
  ],
};

// req: FR-CTL-01
// req: FR-CTL-02
// req: FR-CTL-05
// req: FR-UX-09
describe("Books health and checks", () => {
  it("shows books health and each area's checks: status in words, what each found, the items with links, the OK ones folded; axe finds nothing", async () => {
    answer({ "GET /v1/checks": { status: 200, body: answered } });
    renderPage(Checks, { path: "/checks", session: controller, mode: "basic" });
    const health = await screen.findByRole("region", { name: "Books health: problems" });
    const count = (status: string) => health.querySelector(`[data-status="${status}"] .fy-tile__value`)!.textContent;
    expect([count("error"), count("warning"), count("due"), count("ok")]).toEqual(["1", "1", "0", "4"]);
    expect(within(health).getByText(/1 check is not assessed: each says what it needs. They do not change the score./)).toBeTruthy();
    const row = (key: string) => document.querySelector(`tr[data-check="${key}"]`) as HTMLElement;
    const bs = row("books.balance-sheet-balances");
    expect(within(bs).getByText("Blocks the close")).toBeTruthy();
    expect(within(bs).getByRole("link", { name: "October 2026" }).getAttribute("href")).toBe("/accounting/close/p-oct");
    expect(bs.textContent).toContain("To fix it: Report it: the ledger's own checks should have refused this.");
    expect(within(bs).getByText("The controller")).toBeTruthy();
    // the OK checks of an area that found something are folded until asked for
    expect(row("books.debits-equal-credits")).toBeNull();
    const more = screen.getByRole("button", { name: "Show 2 more OK in Books" });
    expect(more.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(more);
    expect(within(row("books.debits-equal-credits")).getByText("OK")).toBeTruthy();
    // an area with nothing found shows its checks; a person named owns a check instead of the role
    expect(within(row("ledger.balances-match-lines")).getByText("OK")).toBeTruthy();
    expect(within(row("setup.system-accounts-exist")).getByText("Mei Lin Tan")).toBeTruthy();
    // items the reader's roles do not open are counted, not shown
    expect(row("setup.system-accounts-exist").textContent).toContain("24 items in Chart of accounts, dimensions: your roles do not open them.");
    // not assessed says what it needs
    expect(row("audit.chain-intact").textContent).toContain("It needs the audit trail's nightly verification, set in the platform's schedule.");
    expect(within(row("audit.chain-intact")).getByText("Not run yet")).toBeTruthy();
    // the nightly ledger verification beside them (FR-CTL-05)
    const nightly = screen.getByRole("region", { name: "Nightly ledger verification" });
    expect(within(nightly).getByText("Waiting")).toBeTruthy();
    expect(nightly.textContent).toContain("The balances equal their lines: Every balance equals its recomputation");
    await noViolations(document.body);
  });

  it("shows only the checks that need action when asked", async () => {
    answer({ "GET /v1/checks": { status: 200, body: answered } });
    renderPage(Checks, { path: "/checks", session: controller, mode: "basic" });
    await screen.findByRole("region", { name: "Books health: problems" });
    fireEvent.click(screen.getByRole("radio", { name: "Needs action" }));
    const shown = Array.from(document.querySelectorAll("tr[data-check]")).map((r) => r.getAttribute("data-check"));
    expect(shown).toEqual(["books.balance-sheet-balances", "setup.system-accounts-exist", "purchases.bills-to-review", "audit.chain-intact"]);
  });

  it("adds each check's Appendix C row and rules in Expert, and sends the same requests in both modes", async () => {
    answer({ "GET /v1/checks": { status: 200, body: answered } });
    const basic = renderPage(Checks, { path: "/checks", session: controller, mode: "basic" });
    await screen.findByRole("region", { name: "Books health: problems" });
    const inBasic = calls();
    basic.unmount();
    answer({ "GET /v1/checks": { status: 200, body: answered } });
    renderPage(Checks, { path: "/checks", session: controller, mode: "expert" });
    await screen.findByRole("region", { name: "Books health: problems" });
    expect(calls()).toEqual(inBasic);
    expect(document.querySelector('tr[data-check="books.balance-sheet-balances"] .fy-expert-only')!.textContent).toBe("C.1 #28 · INV-01");
  });

  it("runs every check now and says so; a second within the minute is refused with how long to wait", async () => {
    answer({
      "GET /v1/checks": { status: 200, body: answered },
      "POST /v1/checks/runs": { status: 202, body: { requested_at: "2026-11-02T01:00:00Z", already_waiting: false } },
    });
    renderPage(Checks, { path: "/checks", session: bookkeeper, mode: "basic" });
    fireEvent.click(await screen.findByRole("button", { name: "Run all checks now" }));
    expect(await screen.findByText("Every check is running now. This page updates once they have run.")).toBeTruthy();
    expect(seen.filter((s) => s.path === "/v1/checks").length).toBe(2); // read again
    answer({
      "GET /v1/checks": { status: 200, body: answered },
      "POST /v1/checks/runs": problem(429, "API-RATE-01", "The checks were run on request in this minute already.", "Wait 30 seconds, then run them again."),
    });
    fireEvent.click(screen.getByRole("button", { name: "Run all checks now" }));
    expect((await screen.findByRole("alert")).textContent).toContain("Wait 30 seconds");
  });

  it("says Running… while a run waits, and offers no owner's pencil to a role that may not name one", async () => {
    answer({ "GET /v1/checks": { status: 200, body: { ...answered, run_requested: true, may_name_owners: false } } });
    renderPage(Checks, { path: "/checks", session: bookkeeper, mode: "basic" });
    const button = await screen.findByRole("button", { name: "Running…" });
    expect(button.hasAttribute("disabled")).toBe(true);
    expect(screen.queryByRole("button", { name: /Name the owner of/ })).toBeNull();
  });

  // req: FR-CTL-01
  it("lets a Controller name a check's owner, with If-Match, or give it back to its role", async () => {
    answer({
      "GET /v1/checks": { status: 200, body: answered },
      "GET /v1/members": { status: 200, body: { items: [{ id: "m1", user_id: "u-mei", display_name: "Mei Lin Tan", roles: [] }, { id: "m2", user_id: "u-raj", display_name: "Raj Kumar", roles: [] }] } },
      "PUT /v1/checks/setup.system-accounts-exist/owner": { status: 200, body: answered.checks[3] },
    });
    renderPage(Checks, { path: "/checks", session: controller, mode: "basic" });
    fireEvent.click(await screen.findByRole("button", { name: "Name the owner of the system accounts exist" }));
    const dialog = await screen.findByRole("dialog", { name: 'Who owns "The system accounts exist"?' });
    const owner = within(dialog).getByLabelText("Owner") as HTMLSelectElement;
    await within(dialog).findByRole("option", { name: "Raj Kumar" });
    expect(owner.value).toBe("u-mei");
    fireEvent.change(owner, { target: { value: "" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await screen.findByRole("region", { name: "Books health: problems" });
    const put = seen.find((s) => s.method === "PUT")!;
    expect(put.headers.get("If-Match")).toBe('"1"');
    expect(put.body).toEqual({ person_user_id: null });
  });

  // req: FR-CTL-01
  it("marks an item to review as reviewed, with a note; offers it only on items to review", async () => {
    answer({
      "GET /v1/checks": { status: 200, body: answered },
      "POST /v1/checks/purchases.bills-to-review/reviews": { status: 201, body: { ...answered.checks[5], status: "ok", items: [], count: 0 } },
    });
    renderPage(Checks, { path: "/checks", session: controller, mode: "basic" });
    await screen.findByRole("region", { name: "Books health: problems" });
    // ✖ items are fixed, never reviewed
    expect(within(document.querySelector('tr[data-check="books.balance-sheet-balances"]') as HTMLElement).queryByRole("button", { name: /reviewed/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Mark BL-0001 of 2026-10-14, Atlassian Pty Ltd reviewed" }));
    const dialog = await screen.findByRole("dialog", { name: "Mark BL-0001 of 2026-10-14, Atlassian Pty Ltd reviewed?" });
    fireEvent.change(within(dialog).getByLabelText("What you found (optional)"), { target: { value: "Rounded by the supplier" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Mark reviewed" }));
    await screen.findByRole("region", { name: "Books health: problems" });
    expect(seen.find((s) => s.method === "POST")!.body).toEqual({ kind: "bill", id: "b1", note: "Rounded by the supplier" });
    await noViolations(document.body);
  });

  it("offers no review to a role that may not mark items reviewed", async () => {
    answer({ "GET /v1/checks": { status: 200, body: { ...answered, may_review: false } } });
    renderPage(Checks, { path: "/checks", session: { ...ownerSession, roles: ["auditor"] } as Session, mode: "basic" });
    await screen.findByRole("region", { name: "Books health: problems" });
    expect(screen.queryByRole("button", { name: /reviewed/ })).toBeNull();
  });

  it("shows the API's refusal of an owner", async () => {
    answer({
      "GET /v1/checks": { status: 200, body: answered },
      "GET /v1/members": { status: 200, body: { items: [{ id: "m2", user_id: "u-raj", display_name: "Raj Kumar", roles: [] }] } },
      "PUT /v1/checks/books.balance-sheet-balances/owner": problem(422, "CHECK-02", "That person cannot own this check: their roles do not open the check's items.", "Name another member."),
    });
    renderPage(Checks, { path: "/checks", session: controller, mode: "basic" });
    fireEvent.click(await screen.findByRole("button", { name: "Name the owner of the balance sheet balances every month" }));
    const dialog = await screen.findByRole("dialog");
    const owner = within(dialog).getByLabelText("Owner");
    await within(dialog).findByRole("option", { name: "Raj Kumar" });
    fireEvent.change(owner, { target: { value: "u-raj" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    expect((await within(dialog).findByRole("alert")).textContent).toContain("their roles do not open the check's items");
    expect(seen.find((s) => s.method === "PUT")!.headers.get("If-Match")).toBe('"0"');
  });

  it("says when none of the checks so far are of the reader's area", async () => {
    answer({ "GET /v1/checks": { status: 200, body: { ...answered, checks: [], health: { score: "ok", problems: 0, warnings: 0, due: 0, ok: 0, not_assessed: 0 }, may_name_owners: false } } });
    renderPage(Checks, { path: "/checks", session: { ...ownerSession, roles: ["ar_specialist"] } as Session, mode: "basic" });
    expect(await screen.findByText(/None of the checks so far are of your area/)).toBeTruthy();
  });
});

const kpis: KPIs = {
  month: "2026-10",
  from: "2026-10-01",
  to: "2026-10-31",
  currency: "SGD",
  ledger_revision: 42,
  platform_version: "0.1.0",
  kpis: [
    { key: "cash", name: "Cash", unit: "amount", source: "ledger", version: "cash@1", formula: "Cash", date_basis: "month end", month: { value: "92500.00" } },
    { key: "runway", name: "Runway", unit: "months", source: "ledger", version: "runway@1", formula: "Runway", date_basis: "month", month: { value: "24.7" } },
    { key: "net_burn", name: "Net burn", unit: "amount", source: "ledger", version: "net_burn@1", formula: "Burn", date_basis: "month", month: { value: "2500.00" } },
  ],
};
const periods = [{ id: "p-oct", starts_on: "2026-10-01", ends_on: "2026-10-31", status: "open", financial_year: 1, version: 1 }];

// req: FR-CTL-02
describe("books health on Home", () => {
  it("takes the pulse card's third place with the score and its counts, opening the checks; net burn moves to Basic's tiles", async () => {
    answer({ "GET /v1/periods": { status: 200, body: { items: periods } }, "GET /v1/kpis": { status: 200, body: kpis }, "GET /v1/checks": { status: 200, body: answered } });
    renderPage(Dashboard, { path: "/", session: controller, mode: "basic" });
    const pulse = await screen.findByRole("region", { name: "Company pulse, Oct 2026" });
    expect((await within(pulse).findByRole("link", { name: "Problems" })).getAttribute("href")).toBe("/checks");
    expect(within(pulse).getByText("1 blocking the close")).toBeTruthy();
    expect(within(pulse).getByText("1 to review")).toBeTruthy();
    expect(within(pulse).getByText("1 not assessed")).toBeTruthy();
    expect(pulse.querySelector('[data-kpi="net_burn"]')).toBeNull();
    expect(document.querySelector('.fy-grid--3 [data-kpi="net_burn"]')).toBeTruthy();
    await noViolations(document.body);
  });

  it("keeps net burn in the pulse card for someone who may not read the checks", async () => {
    answer({
      "GET /v1/periods": { status: 200, body: { items: periods } },
      "GET /v1/kpis": { status: 200, body: kpis },
      "GET /v1/checks": problem(403, "API-AUTHZ-01", "These roles hold no V right."),
    });
    renderPage(Dashboard, { path: "/", session: { ...ownerSession, roles: ["board_viewer"] } as Session, mode: "basic" });
    const pulse = await screen.findByRole("region", { name: "Company pulse, Oct 2026" });
    expect(pulse.querySelector('[data-kpi="net_burn"]')).toBeTruthy();
    expect(within(pulse).queryByText("Books health")).toBeNull();
  });
});
