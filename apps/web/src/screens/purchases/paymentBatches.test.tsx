import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Session } from "../../api/client";
import { answer, ownerSession } from "../../test/api";
import { noViolations } from "../../test/axe";
import { renderPage } from "../../test/render";
import { PaymentBatchPage } from "./PaymentBatchPage";
import { PaymentBatches } from "./PaymentBatches";

const accountant = { ...ownerSession, roles: ["accountant"] } as Session;
const releaser = { ...ownerSession, roles: ["payment_releaser"] } as Session;
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const account = { currency: "SGD", kind: "singapore" as const, bic: "DBSSSGSG", bank_country: "SG", account_name: "Northwind", number_kind: "account" as const, account_number_last4: "6789" };
const item = (number: string, open: string, checks: unknown[] = []) => ({ kind: "bill" as const, document_id: `d-${number}`, number, date: "2026-10-02", open: sgd(open), checks });
const northwind = { payee: { kind: "vendor" as const, id: "v1", name: "Northwind Analytics Pte Ltd" }, account, payments_frozen: false, amount: sgd("327.00"), items: [item("BILL-0041", "109.00"), item("BILL-0042", "218.00")], paid: false, rejected: false };
const base = {
  id: "b1", status: "draft" as const, money_account_id: "a1", money_account_name: "DBS SGD current", payment_date: "2026-10-31", method: "fast" as const,
  payments: [northwind], total: sgd("327.00"), checks: [], reapproval_needed: false, version: 1,
};

const page = (batch: unknown, session: Session, extra = {}) => {
  const seen = answer({ "GET /v1/payment-batches/b1": { status: 200, body: batch, headers: { ETag: '"1-posted"' } }, ...extra });
  renderPage(PaymentBatchPage, { path: "/purchases/payment-batches/b1", route: "/purchases/payment-batches/$batchId", session });
  return seen;
};

// req: FR-BNK-12
// req: banking/bank-transactions Bills and expense claims (rev 0.154)
describe("payment batches", () => {
  it("lists the batches with their status, and warns of one that needs approving again; axe finds nothing", async () => {
    answer({
      "GET /v1/payment-batches": {
        status: 200,
        body: { items: [{ ...base, status: "approved", number: "PB-0003", reapproval_needed: true }, { ...base, id: "b2", status: "paid", number: "PB-0002" }] },
      },
    });
    renderPage(PaymentBatches, { path: "/purchases/payment-batches", session: accountant });
    const table = await screen.findByRole("table", { name: "Payment batches" });
    expect(within(table).getByText("Needs approving again")).toBeTruthy();
    expect(within(table).getByText("Paid")).toBeTruthy();
    expect(screen.getByText(/needs approving again$/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "New batch" })).toBeTruthy();
    await noViolations(document.body);
  });

  it("prepares a new batch from what is due by its day, and says why a day with nothing due prepares nothing", async () => {
    const dbs = { id: "m1", account_id: "a1", kind: "bank", name: "DBS current", identifier: "…8771", currency: "SGD" };
    let suggested: unknown[] = [];
    const seen = answer({
      "GET /v1/payment-batches": { status: 200, body: { items: [] } },
      "GET /v1/money-accounts": { status: 200, body: { items: [dbs, { ...dbs, id: "m2", account_id: "a2", name: "Wise USD", currency: "USD" }] } },
      "GET /v1/payment-batches/suggestion": () => ({ status: 200, body: { payment_date: "2026-10-30", suggested } }),
      "POST /v1/payment-batches": { status: 201, body: base },
    });
    renderPage(PaymentBatches, { path: "/purchases/payment-batches", session: accountant });
    fireEvent.click(await screen.findByRole("button", { name: "New batch" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).queryByRole("option", { name: /Wise USD/ })).toBeNull();
    fireEvent.change(within(dialog).getByLabelText("Payment date"), { target: { value: "2026-10-30" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Prepare the batch" }));
    expect(await within(dialog).findByText(/Nothing is due to be paid by 30 Oct 2026/)).toBeTruthy();
    expect(seen.some((s) => s.method === "POST")).toBe(false);
    await noViolations(document.body);

    suggested = [{ kind: "bill", document_id: "d-BILL-0041" }, { kind: "claim", document_id: "c1" }];
    fireEvent.click(within(dialog).getByRole("button", { name: "Prepare the batch" }));
    await waitFor(() => expect(seen.some((s) => s.method === "POST")).toBe(true));
    const post = seen.find((s) => s.method === "POST")!;
    expect(post.body).toEqual({ money_account_id: "a1", payment_date: "2026-10-30", method: "fast", items: suggested });
    expect(post.headers.get("Idempotency-Key")).toBeTruthy();
  });

  it("shows a draft's held item and its warning, keeps it from being asked for, and removes the item; axe finds nothing", async () => {
    const held = { ...northwind, items: [item("BILL-0041", "109.00"), item("BILL-0042", "218.00", [{ severity: "error", code: "PB-13", field: "items[1]", message: "BILL-0042 waits for withholding", fix: "Record the decision" }])] };
    const seen = page(
      { ...base, payments: [held], checks: [{ severity: "error", code: "PB-13", field: "items[1]", message: "BILL-0042 waits for withholding", fix: "Record the decision" }, { severity: "warning", code: "PB-23", field: "payments", message: "Northwind Analytics Pte Ltd has no remittance email, so no remittance advice is sent when it is paid", fix: "Add it" }] },
      accountant,
      { "PUT /v1/payment-batches/b1": { status: 200, body: base } },
    );
    expect(await screen.findByRole("heading", { level: 1, name: /31 Oct 2026 batch/ })).toBeTruthy();
    expect(screen.getByText("Blocked")).toBeTruthy();
    expect(screen.getAllByText("BILL-0042 waits for withholding").length).toBeGreaterThan(0);
    expect(screen.getByText(/has no remittance email/)).toBeTruthy();
    expect((screen.getByRole("button", { name: "Ask for approval" }) as HTMLButtonElement).disabled).toBe(true);
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Remove BILL-0042 from the batch" }));
    await waitFor(() => expect(seen.some((s) => s.method === "PUT")).toBe(true));
    const put = seen.find((s) => s.method === "PUT")!;
    expect(put.body).toMatchObject({ items: [{ kind: "bill", document_id: "d-BILL-0041" }] });
    expect(put.headers.get("If-Match")).toBe('"1-posted"');
  });

  // req: G4-055
  it("blocks the release of a batch whose payee changed after approval, and asks for approval again", async () => {
    const seen = page({ ...base, status: "approved", number: "PB-0003", reapproval_needed: true }, accountant, {
      "POST /v1/payment-batches/b1/reapproval": { status: 202, body: { id: "a2", status: "pending" } },
    });
    expect(await screen.findByText("Release is blocked: the batch changed after it was approved")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Release" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Ask to approve again" }));
    expect(await screen.findByText(/Asked for its approval again/)).toBeTruthy();
    const asked = seen.find((s) => s.path === "/v1/payment-batches/b1/reapproval")!;
    expect(asked.headers.get("If-Match")).toBe('"1-posted"');
    expect(asked.headers.get("Idempotency-Key")).toBeTruthy();
    await noViolations(document.body);
  });

  // req: SOD-2
  it("releases an approved batch as the Payment releaser, and the preparer is not offered the release", async () => {
    const seen = page({ ...base, status: "approved", number: "PB-0003" }, releaser, {
      "POST /v1/payment-batches/b1/release": { status: 200, body: { ...base, status: "released", number: "PB-0003" } },
    });
    fireEvent.click(await screen.findByRole("button", { name: "Release" }));
    expect(await screen.findByText(/Released: its payments are posted/)).toBeTruthy();
    expect(seen.find((s) => s.path === "/v1/payment-batches/b1/release")!.headers.get("If-Match")).toBe('"1-posted"');
  });

  it("shows a released batch's payments as paid or waiting, the remittance sent or not, and downloads its payment list; axe finds nothing", async () => {
    const paid = { ...northwind, payment_id: "p1", paid: true, remittance: "sent" as const };
    const gaveUp = { ...northwind, payee: { kind: "vendor" as const, id: "v2", name: "Quiet Supplier Pte Ltd" }, payment_id: "p3", paid: false, remittance: "failed" as const };
    const waiting = { ...northwind, payee: { kind: "person" as const, id: "u1", name: "Tan Ah Kow" }, payment_id: "p2", account: undefined, amount: sgd("27.30") };
    const release = { released_by: { kind: "user", id: "u2", name: "Mei Lin Tan" }, released_at: "2026-10-31T01:30:00Z", approval_id: "a1", writer: "payment-list@1",
      file_name: "PB-0003-payment-list.csv", sha256: "ab".repeat(32), payments: [{ payment_id: "p1" }, { payment_id: "p2" }] };
    const seen = page({ ...base, status: "released", number: "PB-0003", payments: [paid, waiting, gaveUp], release }, releaser, {
      "GET /v1/payment-batches/b1/file": { status: 200, body: { file_name: "PB-0003-payment-list.csv", media_type: "text/csv", sha256: "ab".repeat(32), size_bytes: 3, content_base64: "YWJj" } },
    });
    expect(await screen.findByText("Remittance sent")).toBeTruthy();
    expect(screen.getByText("Paid")).toBeTruthy();
    expect(screen.getAllByText("Waiting for the bank")).toHaveLength(2);
    expect(screen.getByText("Remittance not sent")).toBeTruthy();
    expect(screen.getByText(/Released by Mei Lin Tan/)).toBeTruthy();
    await noViolations(document.body);
    fireEvent.click(screen.getByRole("button", { name: "Download payment list" }));
    expect(await screen.findByText(/Downloaded PB-0003-payment-list.csv/)).toBeTruthy();
    expect(seen.some((s) => s.path === "/v1/payment-batches/b1/file")).toBe(true);
  });
});
