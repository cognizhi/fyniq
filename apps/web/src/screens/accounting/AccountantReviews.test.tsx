import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { AccountantReviewPage, AccountantReviews } from "./AccountantReviews";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const grace = { kind: "user" as const, id: "u-grace", name: "Grace Ho" };
const weiLing = { kind: "user" as const, id: ownerSession.user_id, name: "Wei Ling Tan" };
const sep = { id: "p-sep", name: "September 2026", starts_on: "2026-09-01", ends_on: "2026-09-30" };
const review = {
  id: "r-1", status: "in_review", period: sep, close: { id: "c-1", attested_at: "2026-10-12T02:00:00Z", snapshot_sha256: "ab".repeat(32) },
  opened_by: grace, opened_at: "2026-10-20T02:00:00Z",
  queries: [
    { id: "q-1", asked_by: grace, asked_at: "2026-10-20T03:00:00Z", question: "Why is the September rent on 6010 twice?" },
    { id: "q-2", asked_by: grace, asked_at: "2026-10-20T03:05:00Z", question: "Is the deposit refundable?", answer: "Yes, at the lease's end", answered_by: weiLing, answered_at: "2026-10-21T03:00:00Z" },
  ],
  unanswered: 1, journals: [{ journal_id: "j-1", number: "JNL-0214", status: "posted", memo: "Move the October rent to prepayments", linked_by: grace, linked_at: "2026-10-21T04:00:00Z" }],
  you_may_ask: false, you_may_answer: true, you_may_sign_off: false, version: 4,
};

// req: FM-7
// req: G4-109
// req: access/team-modes Review record per period
describe("accountant reviews", () => {
  it("lists each quarter with its review, and opens a review of a closed month over its period's version; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/accountant-reviews": {
        status: 200,
        body: {
          items: [review], to_review: [{ id: "p-oct", name: "October 2026", starts_on: "2026-10-01", ends_on: "2026-10-31" }], you_may_open: true,
          quarters: [
            { key: "2026-Q3", label: "Jul–Sep 2026", ends_on: "2026-09-30", due_by: "2026-11-29", status: "in_review", period_id: "p-sep", review_id: "r-1" },
            { key: "2026-Q2", label: "Apr–Jun 2026", ends_on: "2026-06-30", due_by: "2026-08-29", status: "overdue", period_id: "p-jun" },
          ],
        },
      },
      "GET /v1/periods/p-oct/close": { status: 200, body: {}, headers: { ETag: '"3-closed"' } },
      "POST /v1/periods/p-oct/accountant-review": { status: 201, body: { ...review, id: "r-2" } },
    });
    renderPage(AccountantReviews, { path: "/accounting/reviews", session: accountant });
    const quarters = await screen.findByRole("list", { name: "Quarters" });
    expect(within(quarters).getByText("Overdue")).toBeTruthy();
    expect(within(quarters).getByText("In review")).toBeTruthy();
    expect(screen.getByRole("link", { name: "September 2026" })).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Open a review" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST")).toBe(true));
    const post = seen.find((s) => s.method === "POST");
    expect(post?.path).toBe("/v1/periods/p-oct/accountant-review");
    expect(post?.headers.get("If-Match")).toBe('"3-closed"');
  });

  it("shows the queries and journals, and lets management answer a query over the review's version; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/accountant-reviews/r-1": { status: 200, body: review },
      "POST /v1/accountant-reviews/r-1/queries/q-1/answer": { status: 200, body: { ...review, unanswered: 0, version: 5 } },
    });
    renderPage(AccountantReviewPage, { path: "/accounting/reviews/r-1", route: "/accounting/reviews/$reviewId" });
    expect(await screen.findByRole("heading", { level: 1, name: "Review of September 2026" })).toBeTruthy();
    expect(screen.getByText(/Yes, at the lease's end/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "JNL-0214" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Sign off" })).toBeNull();
    await noViolations(document.body);
    fireEvent.change(screen.getByLabelText("Your answer"), { target: { value: "The October rent was paid early" } });
    fireEvent.click(screen.getByRole("button", { name: "Answer" }));
    expect(await screen.findByText("Answer saved.")).toBeTruthy();
    const post = seen.find((s) => s.method === "POST");
    expect(post?.headers.get("If-Match")).toBe('"4"');
    expect(post?.body).toEqual({ answer: "The October rent was paid early" });
  });

  it("lets the Accountant sign off only once every query has its answer, and shows a superseded review as kept", async () => {
    const answered = { ...review, queries: [review.queries[1]], unanswered: 0, you_may_ask: true, you_may_answer: false, you_may_sign_off: true };
    const seen = answer({
      "GET /v1/accountant-reviews/r-1": { status: 200, body: answered },
      "GET /v1/journals": { status: 200, body: { items: [] } },
      "POST /v1/accountant-reviews/r-1/sign-off": { status: 200, body: { ...answered, status: "signed_off", signed_by: grace, signed_at: "2026-10-24T02:00:00Z", statement: "Reviewed" } },
    });
    renderPage(AccountantReviewPage, { path: "/accounting/reviews/r-1", route: "/accounting/reviews/$reviewId", session: accountant });
    const button = await screen.findByRole("button", { name: "Sign off" });
    fireEvent.change(screen.getByLabelText("What you sign off"), { target: { value: "Reviewed the September close" } });
    fireEvent.click(button);
    expect(await screen.findByText("Signed off.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ statement: "Reviewed the September close" });
  });

  it("says a superseded review is kept as it was", async () => {
    answer({ "GET /v1/accountant-reviews/r-1": { status: 200, body: { ...review, status: "superseded", superseded_at: "2026-11-02T02:00:00Z", you_may_answer: false } } });
    renderPage(AccountantReviewPage, { path: "/accounting/reviews/r-1", route: "/accounting/reviews/$reviewId" });
    expect(await screen.findByText(/This review is kept as it was/)).toBeTruthy();
    expect(screen.queryByLabelText("Your answer")).toBeNull();
  });
});
