import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { answer, calls, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import type { Session } from "../../api/client";
import { RestoreDrillsTab, minutes } from "./RestoreDrills";

const all = { ledger: true, audit_chain: true, documents: true, keys: true, sign_in: true, tick: true, outbox: true };
const recovered = {
  id: "d-1", held_on: "2026-11-15", scenario: "project_loss", restored_from: "The weekly export of 2026-11-08", data_lost_minutes: 10080, took_minutes: 185,
  ...all, recovered: true, findings: "Importing the export took 40 minutes.", recorded_by: "user:u-1", recorded_at: "2026-11-15T09:30:00Z",
};
const failed = { ...recovered, id: "d-0", held_on: "2026-08-10", scenario: "corruption", restored_from: "2026-08-10 02:00", data_lost_minutes: 5, took_minutes: 60, outbox: false, recovered: false, findings: "The outbox step was missed." };

const dpo = { ...ownerSession, roles: ["dpo"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;

// req: DR-06
// req: CTL-RES-02
describe("the restore drill register", () => {
  it("lists each drill with what failed and whether it recovered, records one, and axe finds nothing", async () => {
    answer({
      "GET /v1/compliance/restore-drills": { status: 200, body: { items: [failed, recovered] } },
      "POST /v1/compliance/restore-drills": { status: 201, body: recovered },
    });
    renderPage(RestoreDrillsTab, { session: dpo });
    expect(await screen.findByText(/Loss of the production project/)).toBeTruthy();
    expect(screen.getByText("Recovered")).toBeTruthy();
    expect(screen.getByText("Not recovered")).toBeTruthy();
    expect(screen.getByText("Did not pass: The outbox's work was reconciled")).toBeTruthy();
    expect(screen.getByText(/7 days of data lost · took 3 hours 5 minutes/)).toBeTruthy();
    await noViolations(document.body);

    fireEvent.click(screen.getByRole("button", { name: "Record a drill" }));
    const form = await screen.findByRole("dialog", { name: "Record a drill" });
    const record = within(form).getByRole("button", { name: "Record the drill" }) as HTMLButtonElement;
    expect(record.disabled).toBe(true);
    fireEvent.change(within(form).getByLabelText("Held on"), { target: { value: "2026-11-15" } });
    fireEvent.change(within(form).getByLabelText("Restored from"), { target: { value: "The weekly export of 2026-11-08" } });
    fireEvent.change(within(form).getByLabelText("Data lost, in minutes"), { target: { value: "10080" } });
    fireEvent.change(within(form).getByLabelText("Recovery took, in minutes"), { target: { value: "185" } });
    fireEvent.click(within(form).getByLabelText("The ledger's verification passed"));
    fireEvent.change(within(form).getByLabelText("What it found and what changed"), { target: { value: "Importing the export took 40 minutes." } });
    await noViolations(document.body);
    expect(record.disabled).toBe(false);
    fireEvent.click(record);
    await waitFor(() => expect(calls()).toContain("POST /v1/compliance/restore-drills"));
  });

  it("shows the register read-only to a reader who is not the DPO", async () => {
    answer({ "GET /v1/compliance/restore-drills": { status: 200, body: { items: [] } } });
    renderPage(RestoreDrillsTab, { session: auditor });
    expect(await screen.findByText("No full drill recorded yet.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Record a drill" })).toBeNull();
  });

  it("says minutes as people do", () => {
    expect([minutes(1), minutes(45), minutes(60), minutes(185), minutes(10080), minutes(1500)]).toEqual(["1 minute", "45 minutes", "1 hour", "3 hours 5 minutes", "7 days", "1 day 1 hour"]);
  });
});
