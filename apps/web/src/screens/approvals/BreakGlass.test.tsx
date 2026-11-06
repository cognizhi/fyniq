import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { BreakGlassPage } from "./BreakGlass";

const sgd = (amount: string) => ({ amount, currency: "SGD" });
const weiLing = { kind: "user" as const, id: ownerSession.user_id, name: "Wei Ling Tan" };
const ahKow = { kind: "user" as const, id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d09", name: "Ah Kow Lim" };
const glass = {
  id: "g-1", status: "active", started_by: weiLing, reason: "Ah Kow is on a flight until Friday; the rent is due", started_at: "2026-11-06T01:00:00Z",
  expires_at: "2026-11-07T01:00:00Z", cap: sgd("20000.00"), released: sgd("109.00"), left: sgd("19891.00"),
  releases: [{ batch_id: "b1", number: "PB-0004", total: sgd("109.00"), released_at: "2026-11-06T02:00:00Z" }], review_due_at: "2026-11-14T01:00:00Z",
  overdue: false, you_may_end: true, you_may_review: false, version: 1,
};
const none = { items: [], cap: sgd("20000.00"), you_may_start: true };

// req: FM-8
// req: access/team-modes Starting break-glass
// req: access/team-modes Banner and notifications
// req: access/team-modes Post-review
describe("Break-glass", () => {
  it("starts with a reason, after saying what it allows, and axe finds nothing", async () => {
    const seen = answer({ "GET /v1/break-glass": { status: 200, body: none }, "POST /v1/break-glass": { status: 201, body: glass } });
    renderPage(BreakGlassPage, { path: "/break-glass" });
    const form = await screen.findByRole("form", { name: "Start break-glass" });
    expect(within(form).getByText(/up to SGD 20000.00 in all/)).toBeTruthy();
    expect(within(form).getByText(/Never: repaying a director, paying yourself/)).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(within(form).getByRole("button", { name: "Start break-glass" }));
    expect(await within(form).findByText("Say who is unreachable and what must be paid.")).toBeTruthy();
    fireEvent.change(within(form).getByLabelText("Why"), { target: { value: " Ah Kow is on a flight until Friday " } });
    fireEvent.click(within(form).getByRole("button", { name: "Start break-glass" }));
    expect(await screen.findByText(/Break-glass started: every Owner and the Accountant have been told/)).toBeTruthy();
    expect(seen.find((s) => s.method === "POST")?.body).toEqual({ reason: "Ah Kow is on a flight until Friday" });
  });

  it("shows the active one with what it released, and ends it over its version", async () => {
    const seen = answer({
      "GET /v1/break-glass": { status: 200, body: { ...none, items: [glass], active: glass, you_may_start: false, blocked: { code: "BREAKGLASS-02", message: "A break-glass is active already.", fix: "Use it" } } },
      "POST /v1/break-glass/g-1/end": { status: 200, body: { ...glass, status: "ended", ended_at: "2026-11-06T09:00:00Z", ended_by: ahKow, version: 2 } },
    });
    renderPage(BreakGlassPage, { path: "/break-glass" });
    const on = await screen.findByRole("region", { name: /Break-glass is on until/ });
    expect(within(on).getByText(/SGD 19891.00 left of SGD 20000.00/)).toBeTruthy();
    expect(within(on).getByRole("link", { name: "PB-0004" })).toBeTruthy();
    expect(screen.queryByRole("form", { name: "Start break-glass" })).toBeNull();
    fireEvent.click(within(on).getByRole("button", { name: "End now" }));
    expect(await screen.findByText(/Break-glass ended: it releases nothing more/)).toBeTruthy();
    expect(seen.find((s) => s.method === "POST")?.headers.get("If-Match")).toBe('"1"');
  });

  it("reviews an ended one with a note, and shows an overdue one as cannot post until fixed", async () => {
    const ended = { ...glass, status: "ended", you_may_end: false, overdue: true, you_may_review: true };
    const seen = answer({
      "GET /v1/break-glass": { status: 200, body: { ...none, items: [ended], you_may_start: false, blocked: { code: "BREAKGLASS-02", message: "An earlier break-glass waits for its review, so another does not start yet.", fix: "Ask another Owner to review it" } } },
      "POST /v1/break-glass/g-1/review": { status: 200, body: { ...ended, overdue: false, you_may_review: false, review: { reviewed_by: ahKow, reviewed_at: "2026-11-09T02:00:00Z", note: "Checked" } } },
    });
    renderPage(BreakGlassPage, { path: "/break-glass" });
    expect(await screen.findByText("An earlier break-glass waits for its review, so another does not start yet.")).toBeTruthy();
    const list = screen.getByRole("list", { name: "Every break-glass" });
    expect(within(list).getByText("Review overdue")).toBeTruthy();
    expect(within(list).getByText(/the month's close waits for it/)).toBeTruthy();
    fireEvent.click(within(list).getByRole("button", { name: "Review" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Note (optional)"), { target: { value: "Checked the payment against the bill" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Review" }));
    await screen.findByText(/Review overdue/);
    const sent = seen.find((s) => s.method === "POST");
    expect(sent?.body).toEqual({ note: "Checked the payment against the bill" });
    expect(sent?.headers.get("If-Match")).toBe('"1"');
  });
});
