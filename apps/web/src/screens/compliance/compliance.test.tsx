import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { today } from "../purchases/BillsBoard";
import { DeadlinesSoon } from "./DeadlinesSoon";
import { GstReturns } from "./GstReturns";
import { addDays, StatutoryCalendar } from "./StatutoryCalendar";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const soon = addDays(today(), 10);
const later = addDays(today(), 120);
const item = (key: string, title: string, due: string, status: string, extra: Record<string, unknown> = {}) => ({
  key, obligation: key.split(":")[0], area: "tax", title, anchor: "2026-12-31", due_on: due, status, why: "Within 3 months from the end of the financial year", source: "iras-eci", ...extra,
});
const cal = (deadlines: unknown[]) => ({ from: today(), to: addDays(today(), 364), today: today(), pack: "sg-deadlines@2026.1", starts: "2026-11-07", deadlines });

// req: FR-TAX-07
// req: CMP-02
// req: QA-UI-04
describe("the statutory calendar", () => {
  it("lists what is not assessed first, then each month's deadlines with their status; axe finds nothing", async () => {
    answer({
      "GET /v1/compliance/calendar": { status: 200, body: cal([
        { key: "financial_year:-", obligation: "financial_year", title: "The financial years", status: "not_assessed", reason: "The books start and the first financial year's end are not set" },
        item("eci:2026-12-31", "Estimated chargeable income for the year ended 31 Dec 2026", soon, "due_soon"),
        item("annual_return:2026-12-31", "Annual return for the year ended 31 Dec 2026", later, "open", { area: "corporate", why: "Within seven months after FYE", source: "acra-annual-return" }),
        item("register_controller_entry:c-1:2025-01-09", "Enter Ah Kow in the register of registrable controllers", "2025-01-09", "overdue",
          { area: "corporate", register: true, link: "/equity/holders/c-1", why: undefined, source: undefined }),
      ]) },
    });
    renderPage(StatutoryCalendar, { path: "/compliance/calendar", route: "/compliance/calendar" });
    expect(await screen.findByText("Not assessed: The financial years")).toBeTruthy();
    expect(screen.getByText("Estimated chargeable income for the year ended 31 Dec 2026")).toBeTruthy();
    expect(screen.getByText("Due soon")).toBeTruthy();
    // A register's own deadline opens its record and is never recorded here.
    const reg = screen.getByText("Enter Ah Kow in the register of registrable controllers").closest("li")!;
    expect(within(reg).getByRole("link", { name: /Open its record/ }).getAttribute("href")).toBe("/equity/holders/c-1");
    expect(within(reg).queryByRole("button", { name: /Record as filed/ })).toBeNull();
    // The area filter keeps the corporate items only.
    fireEvent.click(screen.getByRole("button", { name: "Corporate" }));
    expect(screen.queryByText("Estimated chargeable income for the year ended 31 Dec 2026")).toBeNull();
    expect(screen.getByText("Annual return for the year ended 31 Dec 2026")).toBeTruthy();
    await noViolations(document.body);
  });

  it("records an item as not required at once, with the reason", async () => {
    const draft = { id: "f-1", status: "draft", version: 1, key: "eci:2026-12-31", outcome: "not_required", reason: "Waived", evidence: 0, checks: [] };
    const seen = answer({
      "GET /v1/compliance/calendar": { status: 200, body: cal([item("eci:2026-12-31", "Estimated chargeable income for the year ended 31 Dec 2026", soon, "due_soon")]) },
      "POST /v1/compliance/filings": { status: 201, body: draft },
      "POST /v1/compliance/filings/f-1/record": { status: 200, body: { ...draft, status: "recorded" } },
    });
    renderPage(StatutoryCalendar, { path: "/compliance/calendar", route: "/compliance/calendar", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Not required" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Why it is not required"), { target: { value: "Revenue under S$5 million and ECI nil: waived" } });
    await noViolations(document.body);
    fireEvent.click(within(dialog).getByRole("button", { name: "Record it" }));
    expect(await screen.findByText("Estimated chargeable income for the year ended 31 Dec 2026: recorded as not required.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/compliance/filings")?.body).toEqual({ key: "eci:2026-12-31", outcome: "not_required",
      reason: "Revenue under S$5 million and ECI nil: waived" });
    expect(seen.find((s) => s.path === "/v1/compliance/filings/f-1/record")?.headers.get("If-Match")).toBe('"1-draft"');
  });

  it("drafts a filing, then asks for the acknowledgement before it is recorded", async () => {
    const draft = { id: "f-2", status: "draft", version: 1, key: "eci:2026-12-31", outcome: "filed", filed_on: today(), reference: "ECI-1", evidence: 0,
      checks: [{ severity: "error", code: "DOC-10", field: "evidence", message: "it has no acknowledgement attached, so it cannot be recorded as filed", fix: "Attach the submission or payment acknowledgement" }] };
    const seen = answer({
      "GET /v1/compliance/calendar": { status: 200, body: cal([item("eci:2026-12-31", "Estimated chargeable income for the year ended 31 Dec 2026", soon, "due_soon")]) },
      "POST /v1/compliance/filings": { status: 201, body: draft },
      "GET /v1/compliance/filings/f-2/evidence": { status: 200, body: { items: [] } },
    });
    renderPage(StatutoryCalendar, { path: "/compliance/calendar", route: "/compliance/calendar", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: /Record as filed/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Acknowledgement or transaction number"), { target: { value: "ECI-1" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Next: the acknowledgement" }));
    expect(await within(dialog).findByText(/Attach the acknowledgement, then record it/)).toBeTruthy();
    expect((within(dialog).getByRole("button", { name: /^Record$/ }) as HTMLButtonElement).disabled).toBe(true);
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/compliance/filings/f-2/evidence")).toBe(true));
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/compliance/filings")?.body).toEqual({ key: "eci:2026-12-31", outcome: "filed", filed_on: today(), reference: "ECI-1" });
  });

  it("withdraws a live record with the reason, and shows no recording to a reader", async () => {
    const filed = item("eci:2026-12-31", "Estimated chargeable income for the year ended 31 Dec 2026", soon, "filed", {
      filing: { id: "f-3", status: "recorded", version: 1, key: "eci:2026-12-31", outcome: "filed", filed_on: today(), reference: "ECI-1", evidence: 1, checks: [] } });
    const seen = answer({
      "GET /v1/compliance/calendar": { status: 200, body: cal([filed]) },
      "POST /v1/compliance/filings/f-3/withdraw": { status: 200, body: { id: "f-4", status: "withdrawal", version: 1, key: "eci:2026-12-31", outcome: "withdrawn", evidence: 0, checks: [] } },
    });
    const view = renderPage(StatutoryCalendar, { path: "/compliance/calendar", route: "/compliance/calendar", session: accountant });
    expect(await screen.findByText(/Filed on .*, ECI-1/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Withdraw/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Why it was wrong"), { target: { value: "Recorded against the wrong year" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Withdraw it" }));
    expect(await screen.findByText(/the record is withdrawn, and the item is open again/)).toBeTruthy();
    const sent = seen.find((s) => s.path === "/v1/compliance/filings/f-3/withdraw");
    expect(sent?.headers.get("If-Match")).toBe('"1-posted"');
    expect(sent?.body).toEqual({ reason: "Recorded against the wrong year" });
    view.unmount();
    answer({ "GET /v1/compliance/calendar": { status: 200, body: cal([item("eci:2026-12-31", "Estimated chargeable income for the year ended 31 Dec 2026", soon, "due_soon")]) } });
    renderPage(StatutoryCalendar, { path: "/compliance/calendar", route: "/compliance/calendar", session: auditor });
    await screen.findByText("Estimated chargeable income for the year ended 31 Dec 2026");
    expect(screen.queryByRole("button", { name: /Record as filed/ })).toBeNull();
  });
});

// req: FR-TAX-07
describe("the Inbox's deadlines", () => {
  it("shows what is overdue or due in the next 30 days, and nothing later", async () => {
    answer({
      "GET /v1/compliance/calendar": { status: 200, body: cal([
        item("cpf:2026-10-31", "CPF contributions for October 2026", "2026-11-16", "overdue"),
        item("eci:2026-12-31", "Estimated chargeable income for the year ended 31 Dec 2026", soon, "due_soon"),
        item("annual_return:2026-12-31", "Annual return for the year ended 31 Dec 2026", later, "open"),
      ]) },
    });
    renderPage(DeadlinesSoon, { path: "/inbox", route: "/inbox" });
    expect(await screen.findByText("CPF contributions for October 2026")).toBeTruthy();
    expect(screen.getByText("Estimated chargeable income for the year ended 31 Dec 2026")).toBeTruthy();
    expect(screen.queryByText("Annual return for the year ended 31 Dec 2026")).toBeNull();
    expect(screen.getByText("Overdue")).toBeTruthy();
    await noViolations(document.body);
  });
});

const year = (y: number, amount: string, extra: Record<string, unknown> = {}) => ({ year: y, from: `${y}-01-01`, to: `${y}-12-31`, taxable_supplies: { amount, currency: "SGD" },
  entries: 3, unmatched: 0, exceeded: false, ...extra });

// req: FR-TAX-05
describe("the GST registration monitor", () => {
  it("shows the year so far near the threshold, and the forecast; axe finds nothing", async () => {
    answer({
      "GET /v1/compliance/gst-monitor": { status: 200, body: { on: "2026-11-20", status: "near", threshold: { amount: "1000000.00", currency: "SGD" }, warning_percent: "80", share: "80.5",
        last_year: year(2025, "0.00"), this_year: { ...year(2026, "805000.00"), to: "2026-11-20" }, ledger_revision: 4 } },
      "GET /v1/gst-forecasts": { status: 200, body: { items: [] } },
    });
    renderPage(GstReturns, { path: "/compliance/gst", route: "/compliance/gst", session: accountant });
    expect(await screen.findByText("Near the threshold")).toBeTruthy();
    expect(screen.getByText("S$805,000.00")).toBeTruthy();
    expect(screen.getByText("80.5% of the threshold")).toBeTruthy();
    expect(screen.getByRole("progressbar", { name: "This year so far, against the threshold" }).getAttribute("aria-valuetext")).toBe("80.5% of the threshold");
    expect(screen.getByText("None recorded for today")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Record a forecast" })).toBeTruthy();
    await noViolations(document.body);
  });

  it("says when to apply once a test is met, and records a forecast only with its evidence", async () => {
    const draft = { id: "fc-1", status: "draft", version: 1, forecast_on: "2026-11-20", until: "2027-11-19", value: { amount: "1200000.00", currency: "SGD" }, basis: "Signed contracts",
      evidence: 0, checks: [{ severity: "error", code: "GST-31", field: "evidence", message: "it has no evidence attached, so it cannot be recorded", fix: "Attach what it rests on" }] };
    answer({
      "GET /v1/compliance/gst-monitor": { status: 200, body: { on: "2027-01-10", status: "liable", threshold: { amount: "1000000.00", currency: "SGD" }, warning_percent: "80", share: "0.0",
        last_year: year(2026, "1105000.00", { exceeded: true, apply_by: "2027-01-30", registered_from: "2027-03-01" }), this_year: year(2027, "0.00") } },
      "GET /v1/gst-forecasts": { status: 200, body: { items: [draft] } },
      "GET /v1/gst-forecasts/fc-1/evidence": { status: 200, body: { items: [] } },
    });
    renderPage(GstReturns, { path: "/compliance/gst", route: "/compliance/gst", session: accountant });
    expect(await screen.findByText("Must register")).toBeTruthy();
    expect(screen.getByText(/Taxable supplies of 2026: apply by 30 Jan 2027, registered from 1 Mar 2027/)).toBeTruthy();
    expect((screen.getByRole("button", { name: "Record" }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText(/it has no evidence attached, so it cannot be recorded/)).toBeTruthy();
  });
});
