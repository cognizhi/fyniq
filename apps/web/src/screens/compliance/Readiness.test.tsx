import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { ReadinessTab } from "./Readiness";

const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const body = {
  notice: "Readiness, not a certificate: nothing here says Fyniq is certified by any body.",
  frameworks: [
    { key: "csa-ct", title: "CSA Cyber Trust", stage: "Stage 1, with the first employees", controls: 3, pass: 1, fail: 0, overdue: 1, not_assessed: 1, not_applicable: 0, planned: 1,
      gaps: [{ id: "CTL-CHG-12", control: "An independent penetration test within the last 12 months", status: "overdue", detail: "The next independent penetration test is due by 5 Oct 2026." },
        { id: "CTL-RES-04", control: "Security Command Center findings fixed within SLA", status: "not_assessed", detail: "The verification has not run for this organisation yet." }] },
    { key: "imda-ai", title: "IMDA AI governance", stage: "From the AI assistant", controls: 0, pass: 0, fail: 0, overdue: 0, not_assessed: 0, not_applicable: 0, planned: 0, gaps: [] },
  ],
  annex_a: { total: 93, covered: ["5.15", "8.15"], uncovered: ["5.1", "5.2"] },
};

describe("ReadinessTab", () => {
  // req: CMP-08
  it("shows each framework's controls that pass and the gaps, and Annex A's numbers with no control, as readiness and not a certificate; axe finds nothing", async () => {
    answer({ "GET /v1/compliance/readiness": { status: 200, body } });
    renderPage(ReadinessTab, { path: "/compliance/controls", session: auditor });
    expect(await screen.findByText(/Readiness, not a certificate/)).toBeTruthy();
    const card = await screen.findByRole("region", { name: "CSA Cyber Trust" });
    expect(within(card).getByText("1 of 3 pass")).toBeTruthy();
    const gaps = within(card).getByRole("list", { name: "Gaps in CSA Cyber Trust" });
    expect(within(gaps).getByText("CTL-CHG-12: An independent penetration test within the last 12 months")).toBeTruthy();
    expect(within(gaps).getByText("Not assessed")).toBeTruthy();
    expect(screen.queryByRole("region", { name: "IMDA AI governance" })).toBeNull();
    expect(screen.getByText("2 of 93 numbers have a control")).toBeTruthy();
    expect(screen.getByLabelText("Annex A numbers with no control").textContent).toBe("5.1, 5.2");
    await noViolations();
  });
});
