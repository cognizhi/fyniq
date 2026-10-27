import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Contacts } from "./Contacts";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const auditor = { ...ownerSession, roles: ["auditor"] } as Session;
const bookkeeper = { ...ownerSession, roles: ["bookkeeper"] } as Session;
const vendor = { id: "v1", legal_name: "Northwind Analytics Pte Ltd", customer: false, vendor: true, active: true, currency: "SGD", possible_duplicates: [], version: 1 };
const customer = { id: "c1", legal_name: "Only A Customer", customer: true, vendor: false, active: true, possible_duplicates: [], version: 1 };
const account = (last4: string) => ({ currency: "SGD", kind: "singapore" as const, bic: "DBSSSGSG", bank_country: "SG", account_name: "Northwind Analytics Pte Ltd", number_kind: "account" as const, account_number_last4: last4 });
const inForce = { id: "ch0", contact_id: "v1", status: "approved", in_force: true, account: account("6789"), checks: [], version: 1, approved_at: "2026-10-20T02:00:00Z" };
const waiting = { id: "ch1", contact_id: "v1", status: "waiting", in_force: false, account: account("5544"), replaces: account("6789"), approval_id: "a1", checks: [], version: 1 };
const letter = {
  id: "e1", change_id: "ch2", evidence_type: "bank_letter", file_name: "letter.pdf", media_type: "application/pdf", size_bytes: 2048, sha256: "a".repeat(64),
  original_sha256: "a".repeat(64), source: "upload", data_class: "bank", uploaded_by: "user:u1", uploaded_at: "2026-10-27T02:00:00Z", state: "attached", version: 1,
};

const open = async (session: Session) => {
  renderPage(Contacts, { path: "/contacts", session });
  fireEvent.click(await screen.findByRole("button", { name: "Bank details: Northwind Analytics Pte Ltd" }));
  return screen.findByRole("dialog", { name: "Northwind Analytics Pte Ltd's bank details" });
};

// req: FR-CT-03
// req: SEC-11
// req: master-data/contacts Account numbers sealed and masked (rev 0.152)
describe("a vendor's bank details", () => {
  it("shows the account in force by its last four characters, the change waiting and the freeze; axe finds nothing", async () => {
    answer({
      "GET /v1/contacts": { status: 200, body: { items: [vendor, customer] } },
      "GET /v1/contacts/v1/bank-details": { status: 200, body: { contact_id: "v1", payments_frozen: true, in_force: [inForce], changes: [waiting, inForce] } },
    });
    const dialog = await open(accountant);
    expect(await within(dialog).findByText("Payments to Northwind Analytics Pte Ltd are frozen")).toBeTruthy();
    const kept = within(dialog).getByRole("region", { name: "In force" });
    expect(within(kept).getByText(/DBSSSGSG · ••••6789 · Northwind/)).toBeTruthy();
    const changes = within(dialog).getByRole("region", { name: "Changes" });
    expect(within(changes).getByText("Waiting for approval")).toBeTruthy();
    expect(within(changes).getByText("In force")).toBeTruthy();
    expect(within(changes).getByRole("link", { name: "Open the approval" }).getAttribute("href")).toBe("/approvals/a1");
    expect(screen.queryByRole("button", { name: "Bank details: Only A Customer" })).toBeNull();
    await noViolations(document.body);
  });

  it("shows a full number only on request, and hides it again; the Auditor cannot change bank details", async () => {
    answer({
      "GET /v1/contacts": { status: 200, body: { items: [vendor] } },
      "GET /v1/contacts/v1/bank-details": { status: 200, body: { contact_id: "v1", payments_frozen: false, in_force: [inForce], changes: [inForce] } },
      "POST /v1/bank-changes/ch0/reveal": { status: 200, body: { change_id: "ch0", account_number: "0123456789" } },
    });
    const dialog = await open(accountant);
    expect(within(dialog).queryByText("0123456789")).toBeNull();
    fireEvent.click(await within(dialog).findByRole("button", { name: "Show number: SGD account number" }));
    expect(await within(dialog).findByText("0123456789")).toBeTruthy();
    fireEvent.click(within(dialog).getByRole("button", { name: "Hide" }));
    expect(within(dialog).queryByText("0123456789")).toBeNull();
  });

  it("is read without change or reveal by a masked view, and not offered to a role without one", async () => {
    answer({
      "GET /v1/contacts": { status: 200, body: { items: [vendor] } },
      "GET /v1/contacts/v1/bank-details": { status: 200, body: { contact_id: "v1", payments_frozen: false, in_force: [inForce], changes: [inForce] } },
    });
    const dialog = await open(auditor);
    expect((await within(dialog).findAllByText(/••••6789/)).length).toBe(2);
    expect(within(dialog).queryByRole("button", { name: "Change bank details" })).toBeNull();
    expect(within(dialog).queryByRole("button", { name: /Show number/ })).toBeNull();
  });

  it("is not offered to a Bookkeeper", async () => {
    answer({ "GET /v1/contacts": { status: 200, body: { items: [vendor] } } });
    renderPage(Contacts, { path: "/contacts", session: bookkeeper });
    await screen.findByText("Northwind Analytics Pte Ltd");
    expect(screen.queryByRole("button", { name: /Bank details/ })).toBeNull();
  });

  // req: FR-DOC-06
  // req: SOD-1
  it("prepares a change, attaches the letter, and asks for approval, which freezes payments", async () => {
    let files: (typeof letter)[] = [];
    let version = { ...waiting, id: "ch2", status: "draft", replaces: account("6789"), approval_id: undefined, account: account("5544"),
      checks: [{ severity: "error", code: "PAYEE-11", field: "evidence", message: "no bank letter or vendor's confirmation is attached", fix: "Attach the vendor's letter" }] };
    const seen = answer({
      "GET /v1/contacts": { status: 200, body: { items: [vendor] } },
      "GET /v1/contacts/v1/bank-details": { status: 200, body: { contact_id: "v1", payments_frozen: false, in_force: [inForce], changes: [inForce] } },
      "POST /v1/bank-changes": () => ({ status: 201, body: version, headers: { ETag: '"1-draft"' } }),
      "GET /v1/bank-changes/ch2": () => ({ status: 200, body: version, headers: { ETag: '"1-draft"' } }),
      "GET /v1/bank-changes/ch2/evidence": () => ({ status: 200, body: { items: files } }),
      "POST /v1/bank-changes/ch2/evidence": () => {
        files = [letter];
        version = { ...version, checks: [] };
        return { status: 201, body: letter };
      },
      "POST /v1/bank-changes/ch2/approval": { status: 202, body: { id: "a2", status: "pending" } },
    });
    const dialog = await open(accountant);
    fireEvent.click(await within(dialog).findByRole("button", { name: "Change bank details" }));
    const editor = await screen.findByRole("dialog", { name: "Change bank details" });
    fireEvent.change(within(editor).getByLabelText("SWIFT/BIC code"), { target: { value: "dbsssgsg" } });
    fireEvent.change(within(editor).getByLabelText("Account number"), { target: { value: "998-877-665544" } });
    fireEvent.click(within(editor).getByRole("button", { name: "Save as a draft" }));
    const draft = await screen.findByRole("dialog", { name: "Change of bank details" });
    expect(await within(draft).findByText("no bank letter or vendor's confirmation is attached")).toBeTruthy();
    expect((within(draft).getByRole("button", { name: "Ask for approval" }) as HTMLButtonElement).disabled).toBe(true);
    const created = seen.find((s) => s.method === "POST" && s.path === "/v1/bank-changes")!;
    expect(created.body).toMatchObject({ contact_id: "v1", currency: "SGD", kind: "singapore", bic: "DBSSSGSG", bank_country: "SG", account_number: "998-877-665544" });
    expect(created.headers.get("Idempotency-Key")).toBeTruthy();

    fireEvent.change(within(draft).getByLabelText("File"), { target: { files: [new File(["%PDF"], "letter.pdf", { type: "application/pdf" })] } });
    expect((within(draft).getByLabelText("What it is") as HTMLInputElement).value).toBe("bank_letter");
    fireEvent.click(within(draft).getByRole("button", { name: "Attach" }));
    await waitFor(() => expect((within(draft).getByRole("button", { name: "Ask for approval" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(within(draft).getByRole("button", { name: "Ask for approval" }));
    expect(await screen.findByText(/Payments to Northwind Analytics Pte Ltd are frozen until another person approves/)).toBeTruthy();
    const asked = seen.find((s) => s.method === "POST" && s.path === "/v1/bank-changes/ch2/approval")!;
    expect(asked.headers.get("If-Match")).toBe('"1-draft"');
    expect(asked.headers.get("Idempotency-Key")).toBeTruthy();
  });
});
