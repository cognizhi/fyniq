import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { AcraReconciliation } from "./AcraReconciliation";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const capital = { id: "acc-3010", code: "3010", name: "Ordinary share capital", type: "equity", active: true, group: false, money_account: false, control: false,
  payroll_cost: false, revalue: false, parent_id: null, version: 1, role: "share-capital" };
const holder = (id: string, name: string, shares: string) => ({ contact_id: id, name, director: false, shareholder: true, investor: false, recorded: true,
  holdings: [{ account_code: "3010", class_name: "Ordinary shares", shares }] });
const holding = (id: string, name: string, shares: string) => ({ holder_id: id, holder_name: name, class_account_id: "acc-3010", class_code: "3010", shares });
const recorded = { id: "ax-1", status: "recorded" as const, version: 1, profile_on: "2026-10-01", evidence: 1, checks: [], recorded_by: { kind: "user", id: "u1", name: "Grace Ho" },
  recorded_at: "2026-10-02T09:30:00Z", holdings: [holding("c-kow", "Ah Kow", "700000"), holding("c-lion", "Lion Rock Ventures Pte. Ltd.", "299000")] };
const paid = { key: "founders.share-capital-registered", ref: "C.2 #25", status: "ok", detail: "Share capital on 3010 and 3020 agrees with the register of share issues" };

// req: FR-EQ-26
// req: FR-EQ-27
describe("the ACRA reconciliation", () => {
  it("shows each difference from the business profile and paid-up capital against the ledger; axe finds nothing", async () => {
    answer({
      "GET /v1/cap-table/acra-reconciliation": { status: 200, body: { extract: recorded, differences: [{ holder_id: "c-lion", holder_name: "Lion Rock Ventures Pte. Ltd.",
        class_account_id: "acc-3010", class_code: "3010", register: "300000", acra: "299000" }] } },
      "GET /v1/acra-extracts": { status: 200, body: { items: [recorded, { ...recorded, id: "ax-0", status: "superseded", profile_on: "2026-01-05" }] } },
      "GET /v1/checks/founders.share-capital-registered": { status: 200, body: paid },
      "GET /v1/accounts": { status: 200, body: { items: [capital] } },
    });
    renderPage(AcraReconciliation, { path: "/equity/reconciliation", route: "/equity/reconciliation" });
    const table = await screen.findByRole("table", { name: "Differences from ACRA" });
    expect(within(table).getAllByRole("row").map((r) => r.textContent)).toEqual(["HolderClassRegisterACRA", "Lion Rock Ventures Pte. Ltd.3010300,000299,000"]);
    expect(screen.getByText("1 difference")).toBeTruthy();
    expect(screen.getByText(/Business profile of 1 Oct 2026, recorded by Grace Ho/)).toBeTruthy();
    expect(await screen.findByText("Share capital on 3010 and 3020 agrees with the register of share issues")).toBeTruthy();
    expect(screen.getByText("Business profile of 5 Jan 2026")).toBeTruthy();
    // Only whoever may post on the cap table types one in.
    expect(screen.queryByRole("button", { name: "Type in a business profile" })).toBeNull();
    await noViolations(document.body);
  });

  it("types in a business profile from rows for the register's holdings, its shares left to type, and records it with its PDF", async () => {
    const draft = { ...recorded, id: "ax-2", status: "draft" as const, profile_on: "2026-10-02", evidence: 1, recorded_by: undefined, recorded_at: undefined };
    const seen = answer({
      "GET /v1/cap-table/acra-reconciliation": { status: 200, body: { differences: [] } },
      "GET /v1/acra-extracts": { status: 200, body: { items: [] } },
      "GET /v1/checks/founders.share-capital-registered": { status: 200, body: paid },
      "GET /v1/accounts": { status: 200, body: { items: [capital] } },
      "GET /v1/holders": { status: 200, body: { items: [holder("c-kow", "Ah Kow", "700000"), holder("c-lion", "Lion Rock Ventures Pte. Ltd.", "300000")] } },
      "POST /v1/acra-extracts": { status: 201, body: draft },
    });
    renderPage(AcraReconciliation, { path: "/equity/reconciliation", route: "/equity/reconciliation", session: accountant });
    expect(await screen.findByText(/No business profile is recorded yet/)).toBeTruthy();
    await screen.findByRole("button", { name: "Type in a business profile" });
    await waitFor(() => expect(seen.some((s) => s.path === "/v1/holders")).toBe(true));
    fireEvent.click(screen.getByRole("button", { name: "Type in a business profile" }));
    const dialog = await screen.findByRole("dialog");
    expect((within(dialog).getByLabelText("Holding 1: holder") as HTMLSelectElement).value).toBe("c-kow");
    expect((within(dialog).getByLabelText("Holding 1: shares") as HTMLInputElement).value).toBe("");
    fireEvent.change(within(dialog).getByLabelText("Business profile of"), { target: { value: "2026-10-02" } });
    fireEvent.change(within(dialog).getByLabelText("Holding 1: shares"), { target: { value: "700,000" } });
    fireEvent.change(within(dialog).getByLabelText("Holding 2: shares"), { target: { value: "300000" } });
    await noViolations(document.body);
    fireEvent.click(within(dialog).getByRole("button", { name: "Save the extract" }));
    expect(await screen.findByText("The extract is typed in. Attach the business profile's PDF, then record it.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/acra-extracts")?.body).toEqual({
      profile_on: "2026-10-02",
      holdings: [{ holder_id: "c-kow", class_account_id: "acc-3010", shares: "700000" }, { holder_id: "c-lion", class_account_id: "acc-3010", shares: "300000" }],
    });
  });

  it("records a draft with its PDF at the draft's version", async () => {
    const draft = { ...recorded, id: "ax-2", status: "draft" as const, version: 2, profile_on: "2026-10-02", evidence: 1, recorded_by: undefined, recorded_at: undefined };
    const seen = answer({
      "GET /v1/cap-table/acra-reconciliation": { status: 200, body: { differences: [] } },
      "GET /v1/acra-extracts": { status: 200, body: { items: [draft] } },
      "GET /v1/acra-extracts/ax-2/evidence": { status: 200, body: { items: [] } },
      "GET /v1/checks/founders.share-capital-registered": { status: 200, body: paid },
      "GET /v1/accounts": { status: 200, body: { items: [capital] } },
      "GET /v1/holders": { status: 200, body: { items: [] } },
      "POST /v1/acra-extracts/ax-2/record": { status: 200, body: { ...draft, status: "recorded" } },
    });
    renderPage(AcraReconciliation, { path: "/equity/reconciliation", route: "/equity/reconciliation", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "Record" }));
    expect(await screen.findByText("Recorded the business profile of 2 Oct 2026. The register is compared with it at its day.")).toBeTruthy();
    expect(seen.find((s) => s.method === "POST" && s.path === "/v1/acra-extracts/ax-2/record")?.headers.get("If-Match")).toBe('"2-draft"');
  });
});
