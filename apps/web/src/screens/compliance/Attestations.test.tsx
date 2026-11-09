import { screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Attestations, ReviewsDue } from "./Attestations";

const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const controller = { ...ownerSession, roles: ["controller"] } as Session;
const review = (kind: string, title: string, over: object = {}) => ({
  kind, title, owner: "owner", cadence: "quarterly", status: "signed", waiting: 0, overdue: 0, detail: `${title}: nothing waits.`, where: "/settings/access-review",
  evidence: "Each attestation", controls: [], ...over,
});
const reviews = [
  review("review-log", "Another founder reviews each logged action (Founder mode, weekly)", { status: "nothing_due", cadence: "weekly", where: "/review-log" }),
  review("access-review", "An Owner attests the access review (each quarter)", {
    status: "signed", controls: ["CTL-IAM-02"], last_sign_off: { by: "user:u-1", by_name: "Ada Tan", at: "2026-10-03T02:00:00Z" },
  }),
  review("close-signoff", "Someone other than the preparer signs off the close (monthly)", {
    owner: "controller", cadence: "monthly", status: "overdue", waiting: 2, overdue: 1, period: "August 2026", due_on: "2026-09-15",
    detail: "2 month(s) that ended wait for their close, the earliest August 2026, due by 15 Sep 2026.", where: "/accounting/close", controls: ["CTL-FIN-09"],
  }),
  review("accountant-review", "The external accountant reviews each quarter's books, journals and unusual entries (Founder mode)", {
    owner: "accountant", status: "due", waiting: 1, period: "Q3 2026", due_on: "2099-11-29", where: "/accounting/reviews",
  }),
  review("risk-review", "Each risk is reviewed by its review date", { owner: "dpo", cadence: "by review date", status: "not_applicable", where: "/compliance/risks" }),
];

// req: CMP-03
// req: FR-CTL-06
describe("Compliance › Reviews and attestations", () => {
  it("lists the reviews overdue first, a long-overdue one marked, signed off and nothing due; axe finds nothing", async () => {
    answer({
      "GET /v1/compliance/reviews": { status: 200, body: { items: reviews } },
      "GET /v1/compliance/policies": { status: 200, body: { items: [] } },
    });
    renderPage(Attestations, { path: "/compliance/attestations", session: auditor });
    const list = await screen.findByRole("list", { name: "Recurring reviews" });
    const items = within(list).getAllByRole("listitem");
    expect(items.map((li) => li.querySelector(".fy-list__title")?.textContent)).toEqual([
      "Someone other than the preparer signs off the close (monthly)",
      "The external accountant reviews each quarter's books, journals and unusual entries (Founder mode)",
      "An Owner attests the access review (each quarter)",
      "Another founder reviews each logged action (Founder mode, weekly)",
      "Each risk is reviewed by its review date",
    ]);
    const [close, accountant, access, log, risk] = [items[0]!, items[1]!, items[2]!, items[3]!, items[4]!];
    expect(screen.getByText("1 review is overdue")).toBeTruthy();
    expect(within(close).getByText("Overdue")).toBeTruthy();
    expect(within(close).getByText("Over 14 days")).toBeTruthy();
    expect(within(close).getByText(/Controller · monthly · August 2026 · due/)).toBeTruthy();
    expect(within(close).getByRole("link").getAttribute("href")).toBe("/accounting/close");
    expect(within(accountant).getByText("Due")).toBeTruthy();
    expect(within(accountant).queryByText("Over 14 days")).toBeNull();
    expect(within(access).getByText("Signed off")).toBeTruthy();
    expect(within(access).getByText(/Last signed off .* by Ada Tan · evidence: Each attestation · CTL-IAM-02/)).toBeTruthy();
    expect(within(log).getByText("Nothing due")).toBeTruthy();
    expect(within(log).getByText(/Never signed off yet/)).toBeTruthy();
    expect(within(risk).getByText("Not needed")).toBeTruthy();
    await noViolations();
  });

  it("keeps the evidence and the controls to Expert mode", async () => {
    answer({
      "GET /v1/compliance/reviews": { status: 200, body: { items: reviews } },
      "GET /v1/compliance/policies": { status: 200, body: { items: [] } },
    });
    renderPage(Attestations, { path: "/compliance/attestations", session: auditor, mode: "basic" });
    const list = await screen.findByRole("list", { name: "Recurring reviews" });
    expect(within(list).queryByText(/evidence:/)).toBeNull();
    expect(within(list).getAllByText("Overdue").length).toBe(1);
  });

  it("puts the reviews due or overdue that the viewer's roles sign off in the Inbox; axe finds nothing", async () => {
    answer({ "GET /v1/compliance/reviews": { status: 200, body: { items: reviews } } });
    renderPage(ReviewsDue, { path: "/inbox", session: controller });
    const list = await screen.findByRole("list", { name: "Reviews to sign off" });
    expect(within(list).getAllByRole("listitem").map((li) => li.querySelector(".fy-list__title")?.textContent)).toEqual([
      "Someone other than the preparer signs off the close (monthly)",
    ]);
    await noViolations();
  });

  it("shows nothing in the Inbox when nothing waits for the viewer's roles", async () => {
    answer({ "GET /v1/compliance/reviews": { status: 200, body: { items: reviews } } });
    const { container } = renderPage(ReviewsDue, { path: "/inbox", session: auditor });
    await waitFor(() => expect(container.querySelector(".fy-card")).toBeNull());
  });
});
