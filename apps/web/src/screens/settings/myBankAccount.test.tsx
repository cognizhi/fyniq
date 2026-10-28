import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { Profile } from "./Profile";

const employee = { ...ownerSession, roles: ["employee"] } as Session;
const account = (last4: string) => ({ currency: "SGD", kind: "singapore" as const, bic: "DBSSSGSG", bank_country: "SG", account_name: "Tan Ah Kow", number_kind: "account" as const, account_number_last4: last4 });
const inForce = { id: "ch0", member_user_id: ownerSession.user_id, status: "approved", in_force: true, account: account("6789"), checks: [], version: 1, approved_at: "2026-10-02T02:00:00Z" };
const waiting = { id: "ch1", member_user_id: ownerSession.user_id, status: "waiting", in_force: false, account: account("0921"), replaces: account("6789"), approval_id: "a1", checks: [], version: 1 };
const shot = {
  id: "e1", change_id: "ch2", evidence_type: "bank_app_screenshot", file_name: "account.png", media_type: "image/png", size_bytes: 2048, sha256: "a".repeat(64),
  original_sha256: "a".repeat(64), source: "upload", data_class: "bank", uploaded_by: "user:u1", uploaded_at: "2026-10-28T02:00:00Z", state: "attached", version: 1,
};

function page(details: unknown, extra = {}) {
  return answer({
    "GET /v1/session/passkeys": { status: 200, body: { items: [] } },
    "GET /v1/session/devices": { status: 200, body: { items: [] } },
    "GET /v1/me/bank-details": { status: 200, body: details },
    ...extra,
  });
}

const card = () => screen.findByRole("region", { name: "Bank account for reimbursements" });

// req: FR-AP-22
// req: SEC-11
// req: purchases/payables Entered by the person
describe("your bank account for reimbursements", () => {
  it("says there is none yet, and axe finds nothing", async () => {
    page({ member_user_id: ownerSession.user_id, payments_frozen: false, changes: [] });
    renderPage(Profile, { path: "/settings/profile", session: employee });
    const c = await card();
    expect(await within(c).findByText(/No bank account yet/)).toBeTruthy();
    expect((within(c).getByRole("button", { name: "Change my bank account" }) as HTMLButtonElement).disabled).toBe(false);
    await noViolations(document.body);
  });

  // req: purchases/payables Approved by another person
  // req: purchases/payables The full number on request
  it("shows the account in force by its last four digits and the change waiting, with its number only on request; axe finds nothing", async () => {
    page({ member_user_id: ownerSession.user_id, payments_frozen: true, in_force: inForce, changes: [waiting, inForce] }, {
      "POST /v1/me/bank-changes/ch0/reveal": { status: 200, body: { change_id: "ch0", account_number: "0123456789" } },
    });
    renderPage(Profile, { path: "/settings/profile", session: employee });
    const c = await card();
    const list = await within(c).findByRole("list", { name: "Your bank account" });
    expect(within(list).getByText("DBSSSGSG · ••••6789")).toBeTruthy();
    expect(within(list).getByText(/In force since/)).toBeTruthy();
    expect(within(list).getByText("DBSSSGSG · ••••0921")).toBeTruthy();
    expect(within(c).getByText("Change waits for approval")).toBeTruthy();
    expect(within(c).getByRole("button", { name: "Withdraw" })).toBeTruthy();
    expect((within(c).getByRole("button", { name: "Change my bank account" }) as HTMLButtonElement).disabled).toBe(true);
    expect(within(c).queryByText("0123456789")).toBeNull();
    await noViolations(document.body);
    fireEvent.click(within(c).getByRole("button", { name: "Show number: Your account number" }));
    expect(await within(c).findByText("0123456789")).toBeTruthy();
  });

  it("withdraws the change waiting, at the change's version", async () => {
    const seen = page({ member_user_id: ownerSession.user_id, payments_frozen: true, in_force: inForce, changes: [waiting, inForce] }, {
      "POST /v1/me/bank-changes/ch1/withdraw": { status: 200, body: { ...waiting, status: "draft", approval_id: undefined } },
    });
    renderPage(Profile, { path: "/settings/profile", session: employee });
    const c = await card();
    fireEvent.click(await within(c).findByRole("button", { name: "Withdraw" }));
    expect(await within(c).findByText(/Withdrawn\. Your bank account stays as it was/)).toBeTruthy();
    const withdrawn = seen.find((s) => s.method === "POST" && s.path === "/v1/me/bank-changes/ch1/withdraw")!;
    expect(withdrawn.headers.get("If-Match")).toBe('"1-draft"');
    expect(withdrawn.headers.get("Idempotency-Key")).toBeTruthy();
  });

  it("is not offered to someone who does not claim", async () => {
    page({ member_user_id: ownerSession.user_id, payments_frozen: false, changes: [] });
    renderPage(Profile, { path: "/settings/profile" });
    await screen.findByRole("region", { name: "Sign-in and security" });
    expect(screen.queryByRole("region", { name: "Bank account for reimbursements" })).toBeNull();
  });

  // req: FR-DOC-06
  // req: FM-1
  it("prepares a change, attaches the screenshot, and asks for approval", async () => {
    let files: (typeof shot)[] = [];
    let version = { ...waiting, id: "ch2", status: "draft", approval_id: undefined,
      checks: [{ severity: "error", code: "PAYEE-11", field: "evidence", message: "no screenshot of your banking app or statement header is attached", fix: "Attach a screenshot" }] };
    const seen = page({ member_user_id: ownerSession.user_id, payments_frozen: false, in_force: inForce, changes: [inForce] }, {
      "POST /v1/me/bank-changes": () => ({ status: 201, body: version, headers: { ETag: '"1-draft"' } }),
      "GET /v1/me/bank-changes/ch2": () => ({ status: 200, body: version, headers: { ETag: '"1-draft"' } }),
      "GET /v1/me/bank-changes/ch2/evidence": () => ({ status: 200, body: { items: files } }),
      "POST /v1/me/bank-changes/ch2/evidence": () => {
        files = [shot];
        version = { ...version, checks: [] };
        return { status: 201, body: shot };
      },
      "POST /v1/me/bank-changes/ch2/approval": { status: 202, body: { id: "a2", status: "pending" } },
    });
    renderPage(Profile, { path: "/settings/profile", session: employee });
    const c = await card();
    fireEvent.click(await within(c).findByRole("button", { name: "Change my bank account" }));
    const editor = await screen.findByRole("dialog", { name: "Change my bank account" });
    expect((within(editor).getByLabelText("Name on the account") as HTMLInputElement).value).toBe("Tan Ah Kow");
    fireEvent.change(within(editor).getByLabelText("Bank's SWIFT/BIC code"), { target: { value: "ocbcsgsg" } });
    fireEvent.change(within(editor).getByLabelText("Account number"), { target: { value: "601-234567-001" } });
    fireEvent.click(within(editor).getByRole("button", { name: "Save as a draft" }));
    const draft = await screen.findByRole("dialog", { name: "Change of your bank account" });
    expect(await within(draft).findByText("no screenshot of your banking app or statement header is attached")).toBeTruthy();
    expect((within(draft).getByRole("button", { name: "Ask for approval" }) as HTMLButtonElement).disabled).toBe(true);
    expect((within(draft).getByLabelText("Account number") as HTMLInputElement).value).toBe("");
    const created = seen.find((s) => s.method === "POST" && s.path === "/v1/me/bank-changes")!;
    expect(created.body).toEqual({ bic: "OCBCSGSG", account_name: "Tan Ah Kow", account_number: "601-234567-001" });
    expect(created.headers.get("Idempotency-Key")).toBeTruthy();

    fireEvent.change(within(draft).getByLabelText("File"), { target: { files: [new File(["png"], "account.png", { type: "image/png" })] } });
    expect((within(draft).getByLabelText("What it is") as HTMLInputElement).value).toBe("bank_app_screenshot");
    expect(within(draft).queryByLabelText("Personal data in it")).toBeNull();
    fireEvent.click(within(draft).getByRole("button", { name: "Attach" }));
    await waitFor(() => expect((within(draft).getByRole("button", { name: "Ask for approval" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(within(draft).getByRole("button", { name: "Ask for approval" }));
    expect(await screen.findByText(/Asked for approval of your bank account/)).toBeTruthy();
    const asked = seen.find((s) => s.method === "POST" && s.path === "/v1/me/bank-changes/ch2/approval")!;
    expect(asked.headers.get("If-Match")).toBe('"1-draft"');
    expect(asked.headers.get("Idempotency-Key")).toBeTruthy();
  });
});
