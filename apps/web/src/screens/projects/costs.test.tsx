import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { answer, problem } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { BillableFields, markupError } from "./BillableFields";

const project = { id: "p1", code: "ACME-01", name: "Acme rollout", status: "active", billing_method: "time_and_materials" };

// req: FR-PRJ-08
describe("billing a cost on to a customer", () => {
  it("shows the fields only on a line that carries a project, and lets a markup be typed", async () => {
    answer({ "GET /v1/projects": { status: 200, body: { items: [project] } } });
    const change = vi.fn();
    const Page = () => <BillableFields ids={["p1"]} billable markup="10" onChange={change} />;
    renderPage(Page, { path: "/x" });
    expect(await screen.findByLabelText("Bill this cost to the customer")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Markup (%)"), { target: { value: "12.5" } });
    expect(change).toHaveBeenCalledWith({ billable: true, markup_percent: "12.5" });
    fireEvent.click(screen.getByLabelText("Bill this cost to the customer"));
    expect(change).toHaveBeenLastCalledWith({ billable: false, markup_percent: "" });
    await noViolations(document.body);
  });
  it("shows nothing for a line with no project, or while projects are off", async () => {
    answer({ "GET /v1/projects": { status: 200, body: { items: [project] } } });
    const Page = () => <BillableFields ids={["other"]} onChange={() => {}} />;
    renderPage(Page, { path: "/x" });
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByLabelText("Bill this cost to the customer")).toBeNull();
    answer({ "GET /v1/projects": problem(403, "PRJ-09", "Projects and time are switched off for this organisation.") });
    const Off = () => <BillableFields ids={["p1"]} onChange={() => {}} />;
    renderPage(Off, { path: "/y" });
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryAllByLabelText("Bill this cost to the customer").length).toBe(0);
  });
  it("says what is wrong with a markup", () => {
    expect(markupError(true, "10")).toBeNull();
    expect(markupError(true, "12.5")).toBeNull();
    expect(markupError(true, "1000")).toMatch(/0 to 999.99/);
    expect(markupError(true, "10%")).toMatch(/percentage/);
    expect(markupError(false, "abc")).toBeNull();
  });
});
