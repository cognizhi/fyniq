import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, problem, seen } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Accounting, dayAfter } from "./Accounting";

type Year = components["schemas"]["AccountingYear"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const areas: [string, string][] = [
  ["revenue", "Revenue"],
  ["leases", "Leases"],
  ["capitalisation_threshold", "Capitalisation threshold"],
];
const year = (extra: Partial<Year> = {}): Year => ({
  starts_on: "2026-01-01",
  ends_on: "2026-12-31",
  status: "not_approved",
  statement_title: "Management statements",
  recommended_framework: "sfrs_se",
  recommended_edition: "effective for periods beginning on or after 1 January 2024",
  policies: areas.map(([area, title]) => ({ area: area as never, title, decides: `What ${title} decides.`, ...(area === "capitalisation_threshold" ? { setting: "assets.capitalisation_threshold" } : {}) })),
  checks: [{ code: "ACCOUNTING-NO-FRAMEWORK", title: "No framework recorded", detail: "No accounting framework is recorded for the financial year from 2026-01-01.", fix: "Record the framework and its edition from this year or an earlier one" }],
  ...extra,
});

// req: FR-ORG-10
describe("the accounting framework and policies", () => {
  it("counts the day after a year's end into the next year", () => {
    expect(dayAfter("2026-12-31")).toBe("2027-01-01");
    expect(dayAfter("2028-02-28")).toBe("2028-02-29");
  });

  it("says the statements are management statements until everything is signed, and records the framework; axe finds nothing", async () => {
    answer({
      "GET /v1/organisation/accounting": { status: 200, body: year() },
      "POST /v1/organisation/accounting/framework": {
        status: 201,
        body: { version: 1, from_on: "2026-01-01", framework: "sfrs_se", name: "SFRS for Small Entities", edition: "x", written_at: "2026-10-21T01:00:00Z", you_may_confirm: false },
      },
    });
    renderPage(Accounting, { path: "/settings/accounting", session: accountant });
    expect(await screen.findByText("Statements: Management statements")).toBeTruthy();
    expect(screen.getByText(/None recorded\. Fyniq recommends SFRS for Small Entities/)).toBeTruthy();
    expect(screen.getAllByText("Not recorded")).toHaveLength(3);
    expect(screen.getByRole("link", { name: "The amount is under Settings › Organisation › Defaults" }).getAttribute("href")).toBe("/settings/organisation");
    await noViolations(document.body);

    fireEvent.click(screen.getByRole("button", { name: "Record the framework" }));
    const dialog = await screen.findByRole("dialog");
    expect((within(dialog).getByLabelText("Edition") as HTMLInputElement).value).toBe("effective for periods beginning on or after 1 January 2024");
    fireEvent.click(within(dialog).getByRole("button", { name: "Record the framework" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/organisation/accounting/framework" && s.headers.get("Idempotency-Key"))).toBe(true));
    const sent = seen.find((s) => s.method === "POST" && s.path === "/v1/organisation/accounting/framework");
    expect(sent?.body).toMatchObject({ framework: "sfrs_se", from_on: "2026-01-01" });
    expect(await screen.findByText(/version 1 recorded from 1 Jan 2026\. It waits for the accountant's confirmation\./)).toBeTruthy();
  });

  it("shows the basis once approved, and lets another accountant approve a policy", async () => {
    answer({
      "GET /v1/organisation/accounting": {
        status: 200,
        body: year({
          framework: {
            version: 1,
            from_on: "2026-01-01",
            framework: "sfrs_se",
            name: "SFRS for Small Entities",
            edition: "Third Edition",
            written_at: "2026-10-21T01:00:00Z",
            confirmed_by: { kind: "user", id: "u2", name: "Mei Ling" },
            confirmed_at: "2026-10-21T02:00:00Z",
            you_may_confirm: false,
          },
          policies: [
            {
              area: "revenue",
              title: "Revenue",
              decides: "When revenue is recognised.",
              policy: { area: "revenue", version: 2, from_on: "2026-01-01", wording: "Evenly over each billing period.", written_by: { kind: "user", id: "u1", name: "Ah Kow" }, written_at: "2026-10-21T01:00:00Z", you_may_approve: true },
            },
          ],
          checks: [{ code: "ACCOUNTING-UNAPPROVED", area: "revenue", title: "Revenue: not approved", detail: "Version 2 waits.", fix: "Approve it" }],
        }),
      },
      "POST /v1/organisation/accounting/policies/revenue/2/approve": {
        status: 200,
        body: { area: "revenue", version: 2, from_on: "2026-01-01", wording: "Evenly over each billing period.", written_at: "2026-10-21T01:00:00Z", you_may_approve: false },
      },
    });
    renderPage(Accounting, { path: "/settings/accounting", session: accountant });
    expect(await screen.findByText("SFRS for Small Entities, Third Edition")).toBeTruthy();
    expect(screen.getByText("Mei Ling, 21 Oct 2026")).toBeTruthy();
    expect(screen.getByText("Not approved")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Approve: Revenue" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/organisation/accounting/policies/revenue/2/approve" && s.headers.get("Idempotency-Key"))).toBe(true));
    expect(await screen.findByText("Revenue: approved.")).toBeTruthy();
  });

  it("asks for a second factor when the API says signing needs one (rev 0.63)", async () => {
    answer({
      "GET /v1/organisation/accounting": {
        status: 200,
        body: year({
          policies: [
            {
              area: "leases",
              title: "Leases",
              decides: "How leases are recognised.",
              policy: { area: "leases", version: 1, from_on: "2026-01-01", wording: "The company has no leases.", written_at: "2026-10-21T01:00:00Z", you_may_approve: true },
            },
          ],
        }),
      },
      "POST /v1/organisation/accounting/policies/leases/1/approve": problem(403, "API-AUTHZ-05", "Approving a policy needs step-up authentication in the last 5 minutes."),
    });
    renderPage(Accounting, { path: "/settings/accounting", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Approve: Leases" }));
    expect(await screen.findByRole("heading", { name: "This needs a second factor" })).toBeTruthy();
  });

  it("says what the statements are prepared under once the year is approved", async () => {
    answer({
      "GET /v1/organisation/accounting": {
        status: 200,
        body: year({ status: "approved", statement_title: "Financial statements", statement_basis: "Prepared under SFRS for Small Entities (Third Edition)", checks: [] }),
      },
    });
    renderPage(Accounting, { path: "/settings/accounting", session: { ...ownerSession, roles: ["auditor"] } as Session });
    expect(await screen.findByText("Statements: Financial statements")).toBeTruthy();
    expect(screen.getByText(/Prepared under SFRS for Small Entities \(Third Edition\)\./)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Record the framework|Write/ })).toBeNull();
  });
});
