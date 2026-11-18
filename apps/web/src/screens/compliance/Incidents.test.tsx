import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, calls, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import type { Session } from "../../api/client";
import { IncidentsTab, incidentNext } from "./Incidents";

const opened = {
  id: "i-1", title: "An admin signed in from a new device", summary: "The new_device alert fired.", severity: "high", detected_at: "2026-11-11T02:14:00Z",
  opened_by: "user:u-1", opened_at: "2026-11-11T02:20:00Z", status: "contained",
  steps: [{ step: "contained", at: "2026-11-11T02:40:00Z", by: "user:u-1", note: "Signed out the device." }],
};
const closed = { ...opened, id: "i-2", title: "A false alarm", severity: "low", status: "reviewed", steps: ["contained", "assessed", "recovered", "reviewed"].map((step) => ({ step, at: "2026-11-01T00:00:00Z", by: "user:u-1", note: step })) };

const dpo = { ...ownerSession, roles: ["dpo"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;

// req: SEC-32
describe("the incident register", () => {
  it("lists each incident with its severity and status, opens one and records the next step, and axe finds nothing", async () => {
    answer({
      "GET /v1/compliance/incidents": { status: 200, body: { items: [closed, opened] } },
      "GET /v1/compliance/breaches": { status: 200, body: { items: [] } },
      "POST /v1/compliance/incidents": { status: 201, body: opened },
      "POST /v1/compliance/incidents/i-1/steps": { status: 201, body: opened },
    });
    renderPage(IncidentsTab, { session: dpo });
    expect(await screen.findByText("An admin signed in from a new device")).toBeTruthy();
    expect(screen.getByText("1 open")).toBeTruthy();
    expect(screen.getByText("High")).toBeTruthy();
    expect(screen.getByText(/Reviewed and closed/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Next step for A false alarm" })).toBeNull();
    await noViolations(document.body);

    fireEvent.click(screen.getByRole("button", { name: "Open an incident" }));
    const open = await screen.findByRole("dialog", { name: "Open an incident" });
    fireEvent.change(within(open).getByLabelText("What happened, in a few words"), { target: { value: "A leaked key" } });
    fireEvent.change(within(open).getByLabelText("What is known"), { target: { value: "A connector key in a public paste." } });
    fireEvent.click(within(open).getByLabelText("Critical"));
    await noViolations(document.body);
    fireEvent.click(within(open).getByRole("button", { name: "Open it" }));
    await waitFor(() => expect(calls()).toContain("POST /v1/compliance/incidents"));

    fireEvent.click(await screen.findByRole("button", { name: "Next step for An admin signed in from a new device" }));
    const next = await screen.findByRole("dialog", { name: "Next step" });
    const step = within(next).getByLabelText("Step") as HTMLSelectElement;
    expect(Array.from(step.options).map((o) => o.value)).toEqual(["assessed", "recovered"]);
    fireEvent.change(step, { target: { value: "assessed" } });
    expect(within(next).getByLabelText("The breach, if personal data was involved")).toBeTruthy();
    fireEvent.change(within(next).getByLabelText("What was found or done"), { target: { value: "No personal data was read." } });
    fireEvent.click(within(next).getByRole("button", { name: "Record the step" }));
    await waitFor(() => expect(calls()).toContain("POST /v1/compliance/incidents/i-1/steps"));
  });

  it("shows the register read-only to a reader who is not the DPO", async () => {
    answer({ "GET /v1/compliance/incidents": { status: 200, body: { items: [opened] } } });
    renderPage(IncidentsTab, { session: auditor });
    expect(await screen.findByText("An admin signed in from a new device")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Open an incident" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Next step/ })).toBeNull();
  });

  it("offers only the steps the database accepts next", () => {
    const at = (steps: string[]) => incidentNext({ ...opened, steps: steps.map((step) => ({ step, at: "", by: "", note: "" })) } as never);
    expect(at([])).toEqual(["contained", "assessed"]);
    expect(at(["assessed"])).toEqual(["contained", "notified"]);
    expect(at(["contained", "assessed", "notified", "recovered"])).toEqual(["reviewed"]);
    expect(at(["contained", "recovered", "reviewed"])).toEqual([]);
  });
});
