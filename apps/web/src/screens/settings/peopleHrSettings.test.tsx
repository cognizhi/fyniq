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

  it("shows how the link stands, a gap Nemura delivered and Fyniq never received, and asks Nemura to send it again", async () => {
    const seen = answer({
      "GET /v1/people-link": { status: 200, body: link },
      "GET /v1/people-link/health": { status: 200, body: { state: "active", reachable: true, checked_at: "2026-11-21T09:00:00Z", nemura_highest_sequence: 10, received_highest_sequence: 8, waiting: 1, failing: 1, refused: 0, missing: [{ from: 4, to: 4 }, { from: 9, to: 10 }], requested: [{ from: 20, to: 22 }] } },
      "GET /v1/payroll-arrangements": { status: 200, body: { month: "2026-11", in_force: [], missing: [], met: true } },
      "GET /v1/payroll-imports": { status: 200, body: { items: [] } },
      "POST /v1/people-link/repair": { status: 200, body: { requested: [{ from: 4, to: 4 }, { from: 9, to: 10 }], health: {} } },
    });
    renderPage(PeopleHrSettings, { path: "/settings/people", session: owner });
    expect(await screen.findByText(/Nemura has queued 10 events for Fyniq; Fyniq's inbox holds up to 8\./)).toBeTruthy();
    expect(screen.getByText(/1 waiting to be delivered, 1 failing, 0 refused\./)).toBeTruthy();
    expect(screen.getByText(/never received: 4, 9 to 10\./)).toBeTruthy();
    expect(screen.getByText(/Asked Nemura to send these again in the last day: 20 to 22\./)).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Ask Nemura to send them again" }));
    expect(await screen.findByText("Asked Nemura to send 2 runs of events again.")).toBeTruthy();
    expect(seen.some((s) => s.method === "POST" && s.path.endsWith("/v1/people-link/repair"))).toBe(true);
  });

  it("says when Nemura's log could not be read and invents no number", async () => {
    answer({
      "GET /v1/people-link": { status: 200, body: link },
      "GET /v1/people-link/health": { status: 200, body: { state: "active", reachable: false, note: "Nemura did not answer: try again in a minute.", checked_at: "2026-11-21T09:00:00Z", nemura_highest_sequence: 0, received_highest_sequence: 5, waiting: 0, failing: 0, refused: 0, missing: [], requested: [] } },
      "GET /v1/payroll-arrangements": { status: 200, body: { month: "2026-11", in_force: [], missing: [], met: true } },
      "GET /v1/payroll-imports": { status: 200, body: { items: [] } },
    });
    renderPage(PeopleHrSettings, { path: "/settings/people", session: owner });
    expect(await screen.findByText("Nemura's log could not be read.")).toBeTruthy();
    expect(screen.getByText("Nemura did not answer: try again in a minute.")).toBeTruthy();
    expect(screen.queryByText(/has queued/)).toBeNull();
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

  it("asks to link Nemura with what its page shows, and says nothing is linked by asking", async () => {
    const seen = answer({
      "GET /v1/people-link": { status: 200, body: { ...link, state: "none", sources: [], inbox: [] } },
      "GET /v1/payroll-arrangements": { status: 200, body: { month: "2026-11", in_force: [], missing: [], met: true } },
      "GET /v1/payroll-imports": { status: 200, body: { items: [] } },
      "POST /v1/people-link": { status: 202, body: { id: "r1" } },
    });
    renderPage(PeopleHrSettings, { path: "/settings/people", session: owner });
    const form = await screen.findByRole("form", { name: "Link Nemura" });
    await noViolations(document.body);
    const set = (label: string, value: string) => fireEvent.change(within(form).getByLabelText(label), { target: { value } });
    set("Nemura organisation ID", "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44");
    set("Nemura's web address", "https://people.example.test");
    set("The ID Fyniq's client will have at Nemura", "fyniq-link-1");
    set("The ID Fyniq's read-only client will have at Nemura", "fyniq-ask-1");
    set("Nemura's link key", "link-key-given-by-nemura");
    set("Nemura's client key (JWK)", "{\"kty\":\"EC\",\"crv\":\"P-256\",\"x\":\"a\",\"y\":\"b\"}");
    set("The client expires on", "2027-06-01");
    fireEvent.click(within(form).getByRole("button", { name: "Ask to link Nemura" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/people-link")).toBe(true));
    const post = seen.find((s) => s.method === "POST")!;
    expect(post.body).toMatchObject({ nemura_organisation_id: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44", nemura_client_id: "fyniq-link-1", features: ["records", "payroll"], client_public_key: { kty: "EC" } });
    expect(await screen.findByText(/Asked to link Nemura\. The Owner and a second approver approve it/)).toBeTruthy();
  });

  it("asks to unlink with a reason, and while unlinking shows the work in flight and finishes when nothing is left", async () => {
    const seen = answer({
      "GET /v1/people-link": { status: 200, body: link },
      "GET /v1/people-link/health": { status: 200, body: { state: "active", reachable: true, checked_at: "2026-11-21T09:00:00Z", nemura_highest_sequence: 1, received_highest_sequence: 1, waiting: 0, failing: 0, refused: 0, missing: [], requested: [] } },
      "GET /v1/payroll-arrangements": { status: 200, body: { month: "2026-11", in_force: [], missing: [], met: true } },
      "GET /v1/payroll-imports": { status: 200, body: { items: [] } },
      "POST /v1/people-link/unlink": { status: 202, body: { id: "r2" } },
    });
    const first = renderPage(PeopleHrSettings, { path: "/settings/people", session: owner });
    const form = await screen.findByRole("form", { name: "Unlink Nemura" });
    const ask = within(form).getByRole("button", { name: "Ask to unlink Nemura" }) as HTMLButtonElement;
    expect(ask.disabled).toBe(true);
    fireEvent.change(within(form).getByLabelText("Why"), { target: { value: "Moving payroll to a provider" } });
    fireEvent.click(ask);
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/people-link/unlink")).toBe(true));
    first.unmount();
    answer({
      "GET /v1/people-link": { status: 200, body: { ...link, state: "unlinking", in_flight: { salary_batches: 1, unknown_outcomes: 1, events_waiting: 0 } } },
      "GET /v1/payroll-arrangements": { status: 200, body: { month: "2026-11", in_force: [], missing: [], met: true } },
      "GET /v1/payroll-imports": { status: 200, body: { items: [] } },
      "POST /v1/people-link/unlink/finish": { status: 409, body: { code: "PPL-31", detail: "Work is still in flight: 1 salary batch(es) with a payment not settled.", fix: "Record what became of each payment, then finish unlinking" } },
    });
    renderPage(PeopleHrSettings, { path: "/settings/people", session: owner });
    expect(await screen.findByText(/1 salary batch\(es\) not settled, 1 payment\(s\) with an unknown outcome/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Finish unlinking" }));
    expect(await screen.findByText(/Work is still in flight/)).toBeTruthy();
  });
});
