import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import type { components } from "../../api/schema";
import { answer, ownerSession, seen, type Handler } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { CorporateProfile } from "./CorporateProfile";

type Profile = components["schemas"]["CorporateProfile"];
type Check = components["schemas"]["CheckOut"];

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const board = { ...ownerSession, roles: ["board_viewer"] } as Session;

const check = (key: string, title: string, status: Check["status"], detail: string) =>
  ({ key, ref: "C.2", area: "Corporate", title, status, detail, explain: "", count: 0, items: [], items_hidden: false }) as unknown as Check;

const tan = {
  id: "o-1",
  officer_id: "o-1",
  status: "recorded",
  version: 1,
  masked: false,
  evidence: 1,
  checks: [],
  officer: { role: "director", name: "Tan Mei Lin", kind: "individual", id_type: "nric", id_last4: "567A", appointed_on: "2026-03-02", appointment_lodged_on: "2026-03-09", nominee: false },
} as const;
const kow = { ...tan, id: "o-2", officer_id: "o-2", officer: { ...tan.officer, role: "secretary", name: "Ah Kow", id_last4: undefined, id_type: undefined } } as const;
const draft = {
  id: "o-3",
  officer_id: "o-3",
  status: "draft",
  version: 1,
  masked: false,
  evidence: 0,
  checks: [{ code: "EQUITY-06", severity: "error", message: "It has no evidence attached", fix: "Attach the officer's consent to act" }],
  officer: { role: "director", name: "Lim Hui", kind: "individual", appointed_on: "2026-11-17", nominee: false },
} as const;
const profile = {
  on: "2026-12-05",
  detail: { id: "d-1", status: "recorded", version: 1, evidence: 1, checks: [], detail: { from: "2026-03-02", name: "Cognizhi Pte. Ltd.", registered_office: "1 North Bridge Road", ssic: "62011" } },
  officers: [tan, kow],
  secretary: true,
  details: [],
  officer_history: [draft, tan, kow],
  nominees: [],
  nominee_history: [],
  statements_approvals: [],
} as unknown as Profile;

function serve(extra: Record<string, Handler> = {}) {
  return answer({
    "GET /v1/corporate-profile": { status: 200, body: profile },
    "GET /v1/corporate-profile/reconciliation": {
      status: 200,
      body: { status: "differs", profile_on: "2026-12-01", differences: [{ kind: "office_differs", profile: "1 North Bridge Road", acra: "2 Science Park Drive" }] },
    },
    "GET /v1/compliance/calendar": {
      status: 200,
      body: {
        from: "2026-12-01",
        to: "2027-11-30",
        today: "2026-12-05",
        deadlines: [
          { key: "register_officer_appointment:o-3:2026-11-17", obligation: "register_officer_appointment", title: "Lodge Lim Hui's appointment as director with ACRA", due_on: "2026-12-01", status: "overdue" },
          { key: "gst_f5:2026-12-31", obligation: "gst_f5", title: "GST F5 for October to December", due_on: "2027-01-31", status: "open" },
        ],
      },
    },
    "GET /v1/checks/corporate.secretary": { status: 200, body: check("corporate.secretary", "The company has its secretary", "ok", "Ah Kow is the company secretary") },
    "GET /v1/checks/corporate.statements-approved": {
      status: 200,
      body: check("corporate.statements-approved", "The directors approve the financial statements in time", "not_assessed", "Not assessed: it needs the books start"),
    },
    "GET /v1/checks/corporate.acra-profile": { status: 200, body: check("corporate.acra-profile", "The corporate profile agrees with ACRA's business profile", "warning", "1 difference") },
    "GET /v1/checks/corporate.lodgements": { status: 200, body: check("corporate.lodgements", "Every corporate change is lodged with ACRA on time", "due", "1 change past its deadline") },
    ...extra,
  });
}

// req: FR-CS-01
// req: FR-CS-06
// req: C.2 #34
// req: QA-UI-04
describe("the corporate profile and registers", () => {
  it("shows the checks, the company, its officers with the named secretary, ACRA's differences and the lodgements, and axe finds nothing", async () => {
    serve();
    const { container } = renderPage(CorporateProfile, { path: "/compliance/corporate-profile", session: accountant });
    expect(await screen.findByText("Cognizhi Pte. Ltd.")).toBeTruthy();
    const officers = screen.getByRole("table", { name: "Officers in office" });
    expect(within(officers).getByText("Tan Mei Lin")).toBeTruthy();
    expect(within(officers).getByText("NRIC ending 567A")).toBeTruthy();
    expect(within(officers).getByText("Named secretary")).toBeTruthy();
    expect(await screen.findByText(/The registered office: 1 North Bridge Road here, 2 Science Park Drive at ACRA/)).toBeTruthy();
    const lodgements = screen.getByRole("list", { name: "Lodgements due" });
    expect(within(lodgements).getByText(/Lodge Lim Hui's appointment as director with ACRA/)).toBeTruthy();
    expect(within(lodgements).queryByText(/GST F5/)).toBeNull();
    const checks = await screen.findByRole("list", { name: "The profile's checks" });
    expect(within(checks).getByText("Not assessed: it needs the books start")).toBeTruthy();
    expect(screen.getByText(/It has no evidence attached/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Ask for approval" }).hasAttribute("disabled")).toBe(true);
    await noViolations(container);
  });

  it("drafts an officer with the last 4 characters of their identifier", async () => {
    serve({ "POST /v1/corporate-profile/officers": { status: 201, body: draft } });
    renderPage(CorporateProfile, { path: "/compliance/corporate-profile", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Record an officer" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "Lim Hui" } });
    fireEvent.change(within(dialog).getByLabelText("Identifier (optional)"), { target: { value: "nric" } });
    fireEvent.change(within(dialog).getByLabelText("Its last 4 characters"), { target: { value: "123B" } });
    fireEvent.change(within(dialog).getByLabelText("Appointed on"), { target: { value: "2026-11-17" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Draft the officer" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/corporate-profile/officers")).toBe(true));
    const sent = seen.find((s) => s.method === "POST" && s.path === "/v1/corporate-profile/officers");
    expect(sent?.body).toEqual({ role: "director", name: "Lim Hui", kind: "individual", appointed_on: "2026-11-17", nominee: false, id_type: "nric", id_last4: "123B" });
  });

  it("lets a Board viewer see it, without drafting", async () => {
    serve();
    renderPage(CorporateProfile, { path: "/compliance/corporate-profile", session: board });
    expect(await screen.findByText("Cognizhi Pte. Ltd.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Record an officer" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Ask for approval" })).toBeNull();
  });
});
