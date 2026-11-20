import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { PeopleHrSettings } from "./PeopleHrSettings";

const owner = { ...ownerSession, roles: ["owner"] } as Session;
const link = {
  state: "active", epoch: 1, version: 2, linked_at: "2026-11-01T09:00:00Z", nemura_organisation_id: "n1", nemura_client_id: "c", nemura_ask_client_id: "a", client_id: "x", oauth_client_id: "y",
  fyniq_link_key: "k", fyniq_client_public_key: {}, fyniq_ask_client_public_key: {},
  inbox: [{ outcome: "applied", count: 12, last_at: "2026-11-20T09:00:00Z" }, { outcome: "refused", count: 1, last_at: "2026-11-19T09:00:00Z" }],
  sources: [{ feature: "payroll", source: "nemura", effective_from: "2026-11-01", set_at: "2026-11-01T09:00:00Z" }],
};

// req: FR-PPL-08
// part of FR-PPL-01 (linking and unlinking forms), FR-PAY-15 (the Checks entry)
describe("people and HR settings", () => {
  it("shows where each feature lives, the link and what Fyniq received, and what the go-live gate still needs; axe finds nothing", async () => {
    answer({
      "GET /v1/people-link": { status: 200, body: link },
      "GET /v1/payroll-arrangements": { status: 200, body: { month: "2026-11", in_force: [], missing: ["payslips", "kets", "cpf_sdl_shg", "ais", "ir21"], met: false } },
      "GET /v1/payroll-imports": { status: 200, body: { items: [{ id: "i1", source_system: "talenox", period: "2026-10", version: 1, journal_id: "j1", lines: 4, created_at: "2026-11-02T09:00:00Z" }] } },
    });
    renderPage(PeopleHrSettings, { path: "/settings/people", session: owner });
    const table = await screen.findByRole("table", { name: "Sources" });
    expect(within(table).getByText("Nemura")).toBeTruthy();
    expect(within(table).getAllByText("None: posted by manual journal").length).toBe(3);
    expect(screen.getByText(/12 applied/)).toBeTruthy();
    expect(screen.getByText(/1 refused/)).toBeTruthy();
    expect(await screen.findByText(/A pay run cannot be approved for payment: itemised payslips, key employment terms/)).toBeTruthy();
    expect(screen.getByText(/talenox 2026-10, version 1: a draft journal of 4 lines/)).toBeTruthy();
    await noViolations(document.body);
  });

  it("records an arrangement with its evidence", async () => {
    const seen = answer({
      "GET /v1/people-link": { status: 200, body: { ...link, state: "none", sources: [], inbox: [] } },
      "GET /v1/payroll-arrangements": { status: 200, body: { month: "2026-11", in_force: [], missing: ["payslips"], met: false } },
      "GET /v1/payroll-imports": { status: 200, body: { items: [] } },
      "POST /v1/payroll-arrangements": { status: 201, body: { id: "a1", item: "payslips", method: "provider", provider: "Talenox", effective_from: "2026-11", recorded_by: "user:x", recorded_at: "2026-11-02T09:00:00Z" } },
    });
    renderPage(PeopleHrSettings, { path: "/settings/people", session: owner });
    const form = await screen.findByRole("form", { name: "Record an arrangement" });
    const save = within(form).getByRole("button", { name: "Record the arrangement" }) as HTMLButtonElement;
    expect(save.disabled).toBe(true); // evidence is needed for a provider
    fireEvent.change(within(form).getByLabelText("The provider"), { target: { value: "Talenox" } });
    fireEvent.change(within(form).getByLabelText("Evidence file ID"), { target: { value: "e1" } });
    fireEvent.click(save);
    await waitFor(() => expect(seen.some((s) => s.method === "POST")).toBe(true));
    const post = seen.find((s) => s.method === "POST")!;
    expect(post.body).toMatchObject({ item: "payslips", method: "provider", provider: "Talenox", evidence_id: "e1" });
  });
});
