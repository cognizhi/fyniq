import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { MyShares } from "./MyShares";
import { OptionGrantNew, OptionGrantPage } from "./OptionGrant";
import { OptionPool } from "./OptionPool";
import { PoolResolutionPage } from "./PoolResolution";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const employee = { ...ownerSession, roles: ["employee"] } as Session;
const resolution = {
  id: "pr-1", status: "recorded" as const, version: 1, size: "1000000", class_account_id: "acc-3010", class_code: "3010",
  resolution_ref: "Ordinary resolution 20 Jan 2026", resolved_on: "2026-01-20", in_force: true, evidence: 1, checks: [],
};
const pool = {
  at: "2026-10-15", size: "1000000", class_account_id: "acc-3010", class_code: "3010", granted: "470000", returned: "45000", forfeited: "45000",
  expired: "0", exercised: "0", outstanding: "425000", exercisable: "40000", unallocated: "575000", resolutions: [resolution],
};
const grant = {
  id: "og-1", status: "recorded" as const, version: 1, holder_id: "c-daniel", holder_name: "Daniel Lim", number: "100000", exercise_price: "0.10",
  currency: "SGD", granted_on: "2025-10-01", cliff_months: 12, vesting_months: 48, acceleration: "double" as const, expires_on: "2035-09-30",
  class_account_id: "acc-3010", class_code: "3010", vested: "25000", exercisable: "25000", exercised: "0", forfeited: "0", expired: "0",
  outstanding: "100000", next_vests_on: "2026-11-01", next_vests: "2083", evidence: 1, checks: [], events: [],
};
const leaver = {
  ...grant, id: "og-2", holder_name: "Nurul Aziz", number: "60000", vested: "15000", exercisable: "15000", forfeited: "45000", outstanding: "15000",
  expires_on: "2026-10-29", next_vests_on: undefined, next_vests: undefined,
  events: [{ id: "oe-1", grant_id: "og-2", holder_name: "Nurul Aziz", kind: "forfeiture" as const, number: "45000", on: "2026-07-31", status: "recorded" as const,
    version: 1, evidence: 1, checks: [] }],
};
const capital = { id: "acc-3010", code: "3010", name: "Ordinary share capital", type: "equity", active: true, group: false, money_account: false, control: false,
  payroll_cost: false, revalue: false, parent_id: null, version: 1, role: "share-capital" };

// req: FR-EQ-19
// req: FR-UX-39
describe("the option pool", () => {
  it("shows the pool, every grant and what comes up in 30 days; axe finds nothing", async () => {
    answer({
      "GET /v1/option-pool": { status: 200, body: pool },
      "GET /v1/option-grants": { status: 200, body: { items: [grant, leaver, { ...grant, id: "og-3", status: "draft", holder_name: "Sarah Koh", approval_id: "ap-1" }] } },
    });
    renderPage(OptionPool, { path: "/equity/options", route: "/equity/options", session: accountant });
    expect(await screen.findByText("1,000,000 shares set aside for options by Ordinary resolution 20 Jan 2026 of 20 Jan 2026.")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "The pool · 575,000 available" })).toBeTruthy();
    expect(screen.getByText(/1,000,000 − 470,000 granted \+ 45,000 given back = 575,000 available/)).toBeTruthy();
    const table = await screen.findByRole("table", { name: "Grants" });
    const rows = within(table).getAllByRole("row") as [HTMLElement, HTMLElement, HTMLElement, HTMLElement];
    expect(within(rows[1]).getByText("4 years monthly with a 12-month cliff, from 1 Oct 2025; double-trigger acceleration")).toBeTruthy();
    expect(within(rows[2]).getByText("Recorded")).toBeTruthy();
    expect(within(rows[3]).getByText("Waiting for approval")).toBeTruthy();
    const coming = screen.getByRole("region", { name: "Coming up" });
    expect(within(coming).getByText(/Daniel Lim: 2,083 vest on 1 Nov 2026/)).toBeTruthy();
    expect(within(coming).getByText(/Nurul Aziz: 15,000 vested options lapse on 29 Oct 2026 unless exercised/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "New grant" }).getAttribute("href")).toBe("/equity/options/grants/new");
    expect(screen.getByRole("link", { name: "Record a resolution" }).getAttribute("href")).toBe("/equity/options/resolutions/new");
    await noViolations(document.body);
  });

  it("drafts a grant with its terms, a field left empty left out", async () => {
    const seen = answer({
      "GET /v1/holders": { status: 200, body: { items: [{ contact_id: "c-priya", name: "Priya Nair", director: false, shareholder: false, investor: true, recorded: false, holdings: [] }] } },
      "GET /v1/accounts": { status: 200, body: { items: [capital] } },
      "POST /v1/option-grants": { status: 201, body: { ...grant, id: "og-9", status: "draft" } },
    });
    renderPage(OptionGrantNew, { path: "/equity/options/grants/new", route: "/equity/options/grants/new", session: accountant });
    await screen.findByRole("option", { name: "Priya Nair" });
    fireEvent.change(screen.getByLabelText("Holder"), { target: { value: "c-priya" } });
    fireEvent.change(screen.getByLabelText("Options"), { target: { value: "100,000" } });
    fireEvent.change(screen.getByLabelText("Exercise price per share"), { target: { value: "0.10" } });
    fireEvent.change(screen.getByLabelText("Granted on"), { target: { value: "2026-02-01" } });
    fireEvent.change(screen.getByLabelText("Expires on"), { target: { value: "2036-01-31" } });
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Draft the grant" }));
    await new Promise((r) => setTimeout(r, 0));
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/option-grants")?.body).toEqual({
      holder_id: "c-priya", number: "100000", exercise_price: "0.10", granted_on: "2026-02-01", cliff_months: 12, vesting_months: 48,
      acceleration: "none", expires_on: "2036-01-31", class_account_id: "acc-3010",
    });
  });

  it("shows a grant's standing and drafts a forfeiture at the grant's version; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/option-grants/og-2": { status: 200, body: leaver, headers: { ETag: '"1-posted"' } },
      "GET /v1/option-grants/og-2/evidence": { status: 200, body: { items: [] } },
      "POST /v1/option-grants/og-2/events": { status: 201, body: { ...leaver.events[0], id: "oe-2", status: "draft", kind: "expiry", number: "15000", on: "2026-10-29" } },
    });
    renderPage(OptionGrantPage, { path: "/equity/options/grants/og-2", route: "/equity/options/grants/$grantId", session: accountant });
    expect(await screen.findByRole("heading", { name: "Grant of options: Nurul Aziz" })).toBeTruthy();
    const today = screen.getByRole("region", { name: "Today" });
    expect(within(today).getByText("45,000")).toBeTruthy();
    expect(within(today).getByText("Nothing more vests")).toBeTruthy();
    expect(screen.getByRole("article", { name: "Forfeiture of 45,000 on 31 Jul 2026" })).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Forfeit or let lapse" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("What happens"), { target: { value: "expiry" } });
    fireEvent.change(within(dialog).getByLabelText("Options"), { target: { value: "15,000" } });
    fireEvent.change(within(dialog).getByLabelText("On"), { target: { value: "2026-10-29" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Draft it" }));
    expect(await screen.findByText("Drafted. Attach the notice, then ask for approval.")).toBeTruthy();
    const post = seen.find((s) => s.method === "POST" && s.path === "/v1/option-grants/og-2/events");
    expect(post?.headers.get("If-Match")).toBe('"1-posted"');
    expect(post?.body).toEqual({ kind: "expiry", number: "15000", on: "2026-10-29" });
  });

  it("asks for a drafted resolution's approval at its version", async () => {
    const seen = answer({
      "GET /v1/option-pool/resolutions/pr-2": { status: 200, body: { ...resolution, id: "pr-2", status: "draft", in_force: false }, headers: { ETag: '"1-draft"' } },
      "GET /v1/option-pool/resolutions/pr-2/evidence": { status: 200, body: { items: [] } },
      "POST /v1/option-pool/resolutions/pr-2/approval": { status: 202, body: { id: "ap-5", status: "pending" } },
    });
    renderPage(PoolResolutionPage, { path: "/equity/options/resolutions/pr-2", route: "/equity/options/resolutions/$resolutionId", session: accountant });
    expect(await screen.findByRole("heading", { name: "Pool of 1,000,000 from 20 Jan 2026" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Ask for approval" }));
    expect(await screen.findByText("Asked for approval. Another Owner or the Controller approves it.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/option-pool/resolutions/pr-2/approval")?.headers.get("If-Match")).toBe('"1-draft"');
  });

  it("shows an employee their own grant and its next vesting, and nothing when none is theirs; axe finds nothing", async () => {
    answer({ "GET /v1/me/option-grants": { status: 200, body: { linked: true, grants: [grant] } } });
    renderPage(MyShares, { path: "/me/shares", route: "/me/shares", session: employee });
    expect(await screen.findByRole("heading", { name: "Granted 1 Oct 2025" })).toBeTruthy();
    expect(screen.getByText("2,083 on 1 Nov 2026")).toBeTruthy();
    expect(screen.getByText("SGD 0.10 a share")).toBeTruthy();
    expect(screen.queryByText(/Daniel Lim/)).toBeNull(); // their own: no holder named, no one else's
    await noViolations(document.body);
    cleanup();
    answer({ "GET /v1/me/option-grants": { status: 200, body: { linked: false, grants: [] } } });
    renderPage(MyShares, { path: "/me/shares", route: "/me/shares", session: employee });
    expect(await screen.findByText(/No grant of options is linked to you/)).toBeTruthy();
  });
});
