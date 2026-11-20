import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { SecurityAssessmentsTab } from "./SecurityAssessments";

const owner = { ...ownerSession, roles: ["owner"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const high = { finding_id: "f-1", ref: "PT-1", title: "Reflected script", severity: "high", found_on: "2026-10-05", due_on: "2026-11-04", state: "open", overdue: true, steps: [] };
const assessment = { assessment_id: "a-1", kind: "penetration_test", tester: "Example Security", independent: true, scope: "The web app", started_on: "2026-09-21", ended_on: "2026-10-05",
  summary: "One high", report_sha256: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", report_location: "The shared drive", recorded_by: "user:u-1",
  recorded_at: "2026-10-06T02:00:00Z", findings: [high] };
const register = { items: [assessment], last_independent_on: "2026-10-05", next_due_on: "2027-10-05", overdue_findings: 1 };

describe("SecurityAssessmentsTab", () => {
  // req: SEC-25
  it("lists the assessments with their findings, records one and a step, and shows an auditor no way to change anything; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/compliance/security-assessments": { status: 200, body: register },
      "POST /v1/compliance/security-assessments": { status: 201, body: assessment },
      "POST /v1/compliance/security-findings/f-1/steps": { status: 201, body: { step: "fixed", on: "2026-10-08", recorded_by: "user:u-1", recorded_at: "2026-10-08T02:00:00Z" } },
    });
    const { unmount } = renderPage(SecurityAssessmentsTab, { path: "/compliance/controls", session: owner });
    expect(await screen.findByText(/The last independent test ended on 5 Oct 2026; the next is due by 5 Oct 2027/)).toBeTruthy();
    expect(screen.getByText("1 overdue finding")).toBeTruthy();
    expect(screen.getByText("Overdue")).toBeTruthy();
    await noViolations();

    fireEvent.click(screen.getByRole("button", { name: "Record an assessment" }));
    let dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Who tested"), { target: { value: "Example Security" } });
    fireEvent.change(within(dialog).getByLabelText("What it covered"), { target: { value: "The web app" } });
    fireEvent.change(within(dialog).getByLabelText("First day"), { target: { value: "2026-09-21" } });
    fireEvent.change(within(dialog).getByLabelText("Last day"), { target: { value: "2026-10-05" } });
    fireEvent.change(within(dialog).getByLabelText("What it concluded"), { target: { value: "One high" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Add a finding" }));
    fireEvent.change(within(dialog).getByLabelText("Finding 1 reference"), { target: { value: "PT-1" } });
    fireEvent.change(within(dialog).getByLabelText("Finding 1 title"), { target: { value: "Reflected script" } });
    fireEvent.change(within(dialog).getByLabelText("Finding 1 severity"), { target: { value: "high" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Record it" }));
    await waitFor(() => expect(seen.some((x) => x.method === "POST" && x.path === "/v1/compliance/security-assessments")).toBe(true));
    expect(seen.find((x) => x.method === "POST" && x.path === "/v1/compliance/security-assessments")?.body).toEqual({
      kind: "penetration_test", tester: "Example Security", independent: true, scope: "The web app", started_on: "2026-09-21", ended_on: "2026-10-05", summary: "One high",
      findings: [{ ref: "PT-1", title: "Reflected script", severity: "high" }] });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    fireEvent.click(screen.getByRole("button", { name: "Record a step of PT-1" }));
    dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Day it happened"), { target: { value: "2026-10-08" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Record it" }));
    await waitFor(() => expect(seen.some((x) => x.path === "/v1/compliance/security-findings/f-1/steps")).toBe(true));
    expect(seen.find((x) => x.path === "/v1/compliance/security-findings/f-1/steps")?.body).toEqual({ step: "fixed", on: "2026-10-08" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    unmount();

    answer({ "GET /v1/compliance/security-assessments": { status: 200, body: register } });
    renderPage(SecurityAssessmentsTab, { path: "/compliance/controls", session: auditor });
    expect(await screen.findByText("PT-1: Reflected script")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Record an assessment" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Record a step of PT-1" })).toBeNull();
    await noViolations();
  });
});
