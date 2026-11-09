import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, calls, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Compliance } from "./Compliance";
import { Controls, ControlsFailing } from "./Controls";

const controller = { ...ownerSession, roles: ["controller"] } as Session;
const bookkeeper = { ...ownerSession, roles: ["bookkeeper"] } as Session;
const at = "2027-01-03T20:35:00Z";
const ci = { frequency: "CI", how: "make test", kind: "ci", state: "running", status: "pass", detail: "Passed in CI on the commit of release v1.2.0.", evidence: "https://github.com/cognizhi/fyniq/releases/download/v1.2.0/test-report.zip", keeps: "CI artefact test-report" };
const control = (id: string, status: string, over: object = {}) => ({
  id, section: "F.1", control: `Control ${id}`, owner: "controller", frameworks: ["soc2"], mapping: "SOC2 PI1", frequency: "CI, N", status,
  detail: status === "pass" ? "Each of its 2 check(s) passed." : `${id} says why`, checked_at: at, checks: [ci], ...over,
});
const frameworks = [
  { key: "sg-statute", title: "Singapore statute", controls: 0 },
  { key: "pdpa", title: "PDPA", controls: 1 },
  { key: "soc2", title: "SOC 2", controls: 4 },
];
const dashboard = (items: object[], release: string | null = "v1.2.0") => ({ items, frameworks, ...(release ? { release } : {}) });

// req: CMP-01
// req: FR-CTL-05
// req: FR-UX-29
describe("Compliance › Controls", () => {
  it("shows every control passing, by framework, with the nightly verification; axe finds nothing", async () => {
    answer({
      "GET /v1/compliance/controls": {
        status: 200,
        body: dashboard([control("CTL-FIN-01", "pass"), control("CTL-FIN-04", "pass"), control("CTL-AUD-02", "pass"), control("CTL-DP-02", "pass", { frameworks: ["pdpa"] })]),
      },
    });
    renderPage(Controls, { path: "/compliance/controls", session: controller });
    const list = await screen.findByRole("list", { name: "Controls" });
    expect(within(list).getAllByRole("listitem")).toHaveLength(4);
    expect(screen.getByRole("button", { name: /Pass\s*4/ }).getAttribute("aria-pressed")).toBe("false");
    expect(screen.getByText("Balances recomputed from their lines")).toBeTruthy();
    expect(screen.getByText("4 of 4 · problems first")).toBeTruthy();
    expect(screen.queryByText(/A development build/)).toBeNull();
    // A framework with no control has no filter; one with controls keeps only its own.
    expect(screen.queryByRole("button", { name: "Singapore statute" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "PDPA" }));
    expect(within(screen.getByRole("list", { name: "Controls" })).getAllByRole("listitem")).toHaveLength(1);
    expect(screen.getByText("1 of 4 · problems first")).toBeTruthy();
    await noViolations();
  });

  it("puts a failing control first, and opens its checks with their evidence; axe finds nothing", async () => {
    const verify = { frequency: "N", how: "verify payments.dual-control", kind: "verify", state: "running", status: "fail", detail: "1 found in 2 approval(s)" };
    const planned = { frequency: "Q", kind: "none", state: "planned", arrives: "p1-mcp-first-party", status: "not_assessed", detail: "Not built yet." };
    answer({
      "GET /v1/compliance/controls": {
        status: 200,
        body: dashboard([control("CTL-FIN-05", "pass"), control("CTL-FIN-08", "fail", { failing_since: at, checks: [ci, verify, planned] }), control("CTL-FIN-07", "overdue")]),
      },
    });
    renderPage(Controls, { path: "/compliance/controls", session: controller });
    const list = await screen.findByRole("list", { name: "Controls" });
    const items = within(list).getAllByRole("listitem");
    expect(items[0]!.textContent).toContain("CTL-FIN-08");
    expect(within(items[0]!).getByText("Fail")).toBeTruthy();
    expect(items[1]!.textContent).toContain("CTL-FIN-07");
    expect(within(items[1]!).getByText("Overdue")).toBeTruthy();
    fireEvent.click(within(items[0]!).getByRole("button"));
    const checks = await screen.findByRole("list", { name: "Checks of CTL-FIN-08" });
    expect(within(checks).getByText("1 found in 2 approval(s)")).toBeTruthy();
    expect(within(checks).getByText(/It arrives with p1-mcp-first-party/)).toBeTruthy();
    expect(within(checks).getByRole("link", { name: "Open the evidence" }).getAttribute("href")).toBe(ci.evidence);
    expect(screen.getByText("Controller")).toBeTruthy();
    // The status tiles filter the list.
    fireEvent.click(screen.getByRole("button", { name: /Overdue\s*1/ }));
    expect(within(screen.getByRole("list", { name: "Controls" })).getAllByRole("listitem")).toHaveLength(1);
    await noViolations();
  });

  it("says a development build's CI checks are not assessed; axe finds nothing", async () => {
    answer({
      "GET /v1/compliance/controls": {
        status: 200,
        body: dashboard([control("CTL-CHG-08", "not_assessed", { detail: "A development build: its checks passed in CI, but it carries no release's evidence.", checks: [{ ...ci, status: "not_assessed", evidence: undefined }] })], null),
      },
    });
    renderPage(Controls, { path: "/compliance/controls", session: controller });
    expect(await screen.findByText(/A development build: the checks that run in CI passed there/)).toBeTruthy();
    expect(screen.getByRole("button", { name: /Not assessed\s*1/ })).toBeTruthy();
    await noViolations();
  });
});

// req: CMP-01
// req: compliance/compliance-center The Compliance Center shows controls and raises tasks
describe("the Inbox's Compliance section", () => {
  it("lists the failing and overdue controls the viewer's roles answer for, and nothing else", async () => {
    answer({
      "GET /v1/compliance/controls": {
        status: 200,
        body: dashboard([control("CTL-FIN-08", "fail"), control("CTL-AUD-05", "fail", { owner: "owner" }), control("CTL-FIN-07", "overdue"), control("CTL-FIN-05", "pass")]),
      },
    });
    renderPage(ControlsFailing, { path: "/inbox", session: controller, mode: "basic" });
    const list = await screen.findByRole("list", { name: "Compliance" });
    expect(within(list).getAllByRole("listitem").map((li) => li.textContent?.slice(0, 10))).toEqual(["CTL-FIN-08", "CTL-FIN-07"]);
    await noViolations();
  });

  it("asks nothing for a role that cannot read the Compliance Center", async () => {
    answer({});
    renderPage(ControlsFailing, { path: "/inbox", session: bookkeeper, mode: "basic" });
    await new Promise((r) => setTimeout(r, 20));
    expect(calls()).not.toContain("GET /v1/compliance/controls");
  });
});

// req: FR-UX-29
describe("the Compliance page", () => {
  it("lists the Basic pages in Basic mode", async () => {
    renderPage(Compliance, { path: "/compliance", mode: "basic" });
    const nav = await screen.findByRole("navigation", { name: "Compliance pages" });
    expect(within(nav).getAllByRole("link").map((a) => a.getAttribute("href"))).toEqual(["/compliance/calendar", "/compliance/attestations"]);
  });

  it("lists the controls, the reviews, the risks, GST, evidence and the audit trail in Expert mode", async () => {
    renderPage(Compliance, { path: "/compliance", mode: "expert" });
    const nav = await screen.findByRole("navigation", { name: "Compliance pages" });
    expect(within(nav).getAllByRole("link").map((a) => a.getAttribute("href"))).toEqual([
      "/compliance/calendar",
      "/compliance/controls",
      "/compliance/attestations",
      "/compliance/risks",
      "/compliance/privacy/notices",
      "/compliance/gst",
      "/compliance/evidence",
      "/compliance/audit-trail",
    ]);
  });
});
