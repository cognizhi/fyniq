import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { rating, RiskPolicies } from "./RiskPolicies";

const dpo = { ...ownerSession, roles: ["dpo"] } as Session;
const controller = { ...ownerSession, roles: ["controller"] } as Session;
const risk = (ref: string, over: object = {}) => ({
  id: `id-${ref}`, ref, title: `Risk ${ref}`, owner: "controller", likelihood: "medium", impact: "medium", mitigation: `Mitigation of ${ref}`,
  review_months: 3, next_review_on: "2027-03-31", review_overdue: false, state: "open", version: 1, saved_by: "user:u-1", saved_at: "2026-11-09T02:00:00Z", ...over,
});
const register = [risk("R1", { likelihood: "low", impact: "high" }), risk("R13", { likelihood: "high", impact: "medium", review_overdue: true, next_review_on: "2026-09-30" }), risk("R14", { likelihood: "low", impact: "low" })];

// req: CMP-06
describe("Compliance › Risks and policies", () => {
  it("rates each risk from its likelihood and impact", () => {
    expect(rating({ likelihood: "high", impact: "medium" })).toBe("high");
    expect(rating({ likelihood: "low", impact: "high" })).toBe("medium");
    expect(rating({ likelihood: "low", impact: "low" })).toBe("low");
  });

  it("lists the register highest rating first and calls out an overdue review; another role only reads; axe finds nothing", async () => {
    answer({ "GET /v1/compliance/risks": { status: 200, body: { items: register } } });
    renderPage(RiskPolicies, { path: "/compliance/risks", session: controller });
    const list = await screen.findByRole("list", { name: "Risk register" });
    expect(within(list).getAllByRole("listitem").map((li) => li.textContent?.match(/R\d+/)?.[0])).toEqual(["R13", "R1", "R14"]);
    expect(screen.getByText("1 risk review is overdue")).toBeTruthy();
    expect(within(list).getByText(/Review overdue/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Add a risk" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Review R13/ })).toBeNull();
    await noViolations();
  });

  it("lets the Compliance officer add a risk and review one with its version; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/compliance/risks": { status: 200, body: { items: register } },
      "POST /v1/compliance/risks": { status: 201, body: risk("R23") },
      "POST /v1/compliance/risks/id-R13/reviews": { status: 201, body: risk("R13", { version: 1 }) },
    });
    renderPage(RiskPolicies, { path: "/compliance/risks", session: dpo });
    fireEvent.click(await screen.findByRole("button", { name: "Add a risk" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Risk"), { target: { value: "A supplier's bank details are changed by a fraudster" } });
    fireEvent.change(within(dialog).getByLabelText("Mitigation"), { target: { value: "A second person approves every bank change" } });
    fireEvent.change(within(dialog).getByLabelText("Impact"), { target: { value: "high" } });
    await noViolations();
    fireEvent.click(within(dialog).getByRole("button", { name: "Add the risk" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/compliance/risks")).toBe(true));
    const added = seen.find((s) => s.method === "POST" && s.path === "/v1/compliance/risks")!;
    expect(added.body).toMatchObject({ title: "A supplier's bank details are changed by a fraudster", impact: "high", owner: "owner", review_months: 3 });
    expect(added.headers.get("Idempotency-Key")).toBeTruthy();

    fireEvent.click(await screen.findByRole("button", { name: "Review R13" }));
    const review = await screen.findByRole("dialog");
    fireEvent.change(within(review).getByLabelText("Note (optional)"), { target: { value: "Mitigations hold." } });
    fireEvent.click(within(review).getByRole("button", { name: "Record the review" }));
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/compliance/risks/id-R13/reviews")).toBe(true));
    const reviewed = seen.find((s) => s.path === "/v1/compliance/risks/id-R13/reviews")!;
    expect(reviewed.headers.get("If-Match")).toBe('"v1"');
    expect(reviewed.body).toEqual({ note: "Mitigations hold." });
  });
});
