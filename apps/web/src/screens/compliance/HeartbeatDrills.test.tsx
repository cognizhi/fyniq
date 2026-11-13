import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, calls, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import type { Session } from "../../api/client";
import { HeartbeatDrillsTab } from "./HeartbeatDrills";

const passed = {
  id: "h-1", held_on: "2026-11-15", environment: "staging", paused_minutes: 75, first_alert_minutes: 28, escalation_minutes: 58, passed: true,
  findings: "Both alerts came on time.", recorded_by: "user:u-1", recorded_at: "2026-11-15T09:30:00Z",
};
const late = { ...passed, id: "h-0", held_on: "2026-08-10", first_alert_minutes: 31, escalation_minutes: undefined, passed: false, findings: "No escalation." };

const dpo = { ...ownerSession, roles: ["dpo"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;

// req: NFR-OBS-02
// req: CTL-RES-05
describe("the heartbeat drill register", () => {
  it("lists each drill with when its alerts came and whether it passed, records one, and axe finds nothing", async () => {
    answer({
      "GET /v1/compliance/heartbeat-drills": { status: 200, body: { items: [late, passed] } },
      "POST /v1/compliance/heartbeat-drills": { status: 201, body: passed },
    });
    renderPage(HeartbeatDrillsTab, { session: dpo });
    expect(await screen.findByText(/15 Nov 2026: the tick paused 1 hour 15 minutes in staging/)).toBeTruthy();
    expect(screen.getByText("Passed")).toBeTruthy();
    expect(screen.getByText("Not passed")).toBeTruthy();
    expect(screen.getByText("The first alert came after 31 minutes · the escalation never came")).toBeTruthy();
    await noViolations(document.body);

    fireEvent.click(screen.getByRole("button", { name: "Record a drill" }));
    const form = await screen.findByRole("dialog", { name: "Record a drill" });
    const record = within(form).getByRole("button", { name: "Record the drill" }) as HTMLButtonElement;
    expect(record.disabled).toBe(true);
    fireEvent.change(within(form).getByLabelText("Held on"), { target: { value: "2026-11-15" } });
    fireEvent.change(within(form).getByLabelText("Paused for, in minutes"), { target: { value: "75" } });
    fireEvent.change(within(form).getByLabelText("The first alert, minutes after the pause"), { target: { value: "28" } });
    fireEvent.change(within(form).getByLabelText("What it found and what changed"), { target: { value: "No escalation came." } });
    await noViolations(document.body);
    expect(record.disabled).toBe(false);
    fireEvent.click(record);
    await waitFor(() => expect(calls()).toContain("POST /v1/compliance/heartbeat-drills"));
  });

  it("shows the register read-only to a reader who is not the DPO", async () => {
    answer({ "GET /v1/compliance/heartbeat-drills": { status: 200, body: { items: [] } } });
    renderPage(HeartbeatDrillsTab, { session: auditor });
    expect(await screen.findByText("No heartbeat drill recorded yet.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Record a drill" })).toBeNull();
  });
});
