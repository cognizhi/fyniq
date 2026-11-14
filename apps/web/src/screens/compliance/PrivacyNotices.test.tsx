import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { PrivacyNotices } from "./PrivacyNotices";

const dpo = { ...ownerSession, roles: ["dpo"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const sha = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08";
const version = { version: 1, title: "Notice", sha256: sha, from: "fyniq-notice-draft@1", published_by: "user:u-1", published_at: "2026-11-02T02:00:00Z", acknowledged: 2 };
const notices = [
  { key: "notice-members", kind: "notice", title: "Privacy notice for our people", members: 3, current: version, summary: "This notice tells you...",
    draft: { source: "fyniq", revision: 0, sha256: sha, unpublished: false }, mine: { version: 1, due_on: "2026-12-02", overdue: false } },
  { key: "notice-contacts", kind: "notice", title: "Privacy notice for our customers' and suppliers' contacts", members: 3, current: { ...version, acknowledged: 0 },
    summary: "We keep the names...", draft: { source: "fyniq", revision: 0, sha256: sha, unpublished: false } },
];
const newsletter = { id: "p-1", name: "Newsletter", description: "A monthly email.", audience: "contacts", needs_consent: true, created_by: "user:u-1", created_at: "2026-11-09T02:00:00Z", given: 1 };

// req: CMP-PD-03
describe("Compliance › Privacy notices and consent", () => {
  it("lists the notices, the contacts' given in what Fyniq sends, and says no purpose needs consent by default; axe finds nothing", async () => {
    answer({
      "GET /v1/compliance/policies": { status: 200, body: { items: notices } },
      "GET /v1/compliance/consent-purposes": { status: 200, body: { items: [] } },
      "GET /v1/compliance/consents": { status: 200, body: { items: [] } },
    });
    renderPage(PrivacyNotices, { path: "/compliance/privacy/notices", session: auditor });
    const list = await screen.findByRole("list", { name: "Privacy notices" });
    expect(within(list).getByText(/2 of 3 acknowledged/)).toBeTruthy();
    expect(within(list).getByText(/given to contacts in what Fyniq sends/)).toBeTruthy();
    expect(await screen.findByText(/No purpose is defined/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Add a purpose" })).toBeNull();
    await noViolations();
  });

  it("lets the Compliance officer add a purpose and record a contact's consent; axe finds nothing", async () => {
    const seen = answer({
      "GET /v1/compliance/policies": { status: 200, body: { items: notices } },
      "GET /v1/compliance/consent-purposes": { status: 200, body: { items: [newsletter] } },
      "GET /v1/compliance/consents": { status: 200, body: { items: [{ id: "c-1", purpose_id: "p-1", person: "contact:k-1", event: "given", at: "2026-11-08T02:00:00Z", recorded_by: "user:u-1" }] } },
      "GET /v1/contacts": { status: 200, body: { items: [{ id: "k-1", legal_name: "Ada Tan" }, { id: "k-2", legal_name: "Ben Lee" }] } },
      "GET /v1/members": { status: 200, body: { items: [{ user_id: "u-1", display_name: "Arjun Rao" }] } },
      "POST /v1/compliance/consent-purposes": { status: 201, body: { ...newsletter, id: "p-2", name: "Events" } },
      "POST /v1/compliance/consents": { status: 201, body: { id: "c-2", purpose_id: "p-1", person: "contact:k-2", event: "given", at: "2026-11-09T02:00:00Z", recorded_by: "user:u-1" } },
    });
    renderPage(PrivacyNotices, { path: "/compliance/privacy/notices", session: dpo });
    const purposes = await screen.findByRole("list", { name: "Purposes and consent" });
    expect(within(purposes).getByText("1 consent standing")).toBeTruthy();
    // A record names its person, not their reference.
    const records = screen.getByRole("list", { name: "Consents given and withdrawn" });
    expect(await within(records).findByText(/^Ada Tan · /)).toBeTruthy();
    expect(within(records).queryByText(/contact:k-1/)).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Add a purpose" }));
    let dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Purpose"), { target: { value: "Events" } });
    fireEvent.change(within(dialog).getByLabelText("What it means for the person"), { target: { value: "Invitations to our events." } });
    await noViolations();
    fireEvent.click(within(dialog).getByRole("button", { name: "Add the purpose" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/compliance/consent-purposes")).toBe(true));
    expect(seen.find((s) => s.path === "/v1/compliance/consent-purposes" && s.method === "POST")?.body).toEqual({
      name: "Events", description: "Invitations to our events.", audience: "contacts", needs_consent: true,
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    fireEvent.click(screen.getByRole("button", { name: "Record consent for Newsletter" }));
    dialog = await screen.findByRole("dialog");
    await within(dialog).findByRole("option", { name: "Ben Lee" });
    fireEvent.change(within(dialog).getByLabelText("Contact"), { target: { value: "contact:k-2" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Record it" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST" && s.path === "/v1/compliance/consents")).toBe(true));
    const rec = seen.find((s) => s.path === "/v1/compliance/consents" && s.method === "POST")!;
    expect(rec.body).toEqual({ purpose_id: "p-1", person: "contact:k-2", event: "given" });
    expect(rec.headers.get("Idempotency-Key")).toBeTruthy();
  });
});
