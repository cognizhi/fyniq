import { expect, test, type Page, type Route } from "@playwright/test";

// The payment batch journey against the built web app, with every /api call answered by a fixture that keeps the
// batch's state (the Go suites prove the API, the approval engine and the release): a new batch from what is due,
// asked for, approved by the other founder, released, and its payment list downloaded.
// req: FR-BNK-12
// req: FR-AP-13
// req: SOD-2

const me = "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01";
const session = {
  provider: "local",
  user_id: me,
  organisation_id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02",
  roles: ["owner", "accountant", "payment_releaser"],
  super_admin: false,
  step_up_method: "sign_in",
  features: { multi_currency: true, payroll: false, projects: false, portals: false, ai_assistant: false },
};
const organisation = { id: session.organisation_id, name: "Cognizhi Pte Ltd", base_currency: "SGD", base_minor_units: 2, team_mode: "founder", version: 1 };
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const account = { currency: "SGD", kind: "singapore", bic: "DBSSSGSG", bank_country: "SG", account_name: "Northwind Analytics Pte. Ltd.", number_kind: "account", account_number_last4: "6789" };
const items = [
  { kind: "bill", document_id: "d-41", number: "BILL-0041", date: "2026-10-02", open: sgd("109.00"), checks: [] },
  { kind: "bill", document_id: "d-42", number: "BILL-0042", date: "2026-10-05", open: sgd("218.00"), checks: [] },
];
const payment = { payee: { kind: "vendor", id: "v1", name: "Northwind Analytics Pte Ltd" }, account, payments_frozen: false, amount: sgd("327.00"), items, paid: false, rejected: false };
const list = "PB-0003-payment-list.csv";

// batches answers the batch's calls from its state, which the journey moves on as each step would.
async function batches(page: Page) {
  const state = { status: "none" as "none" | "draft" | "waiting" | "approved" | "released", version: 1 };
  const seen: string[] = [];
  const missing: string[] = [];
  const batch = () => ({
    id: "b1",
    status: state.status,
    ...(state.status === "approved" || state.status === "released" ? { number: "PB-0003" } : {}),
    ...(state.status === "waiting" ? { approval_id: "a-1" } : {}),
    money_account_id: "a1",
    money_account_name: "DBS current",
    payment_date: "2026-10-30",
    method: "fast",
    payments: [state.status === "released" ? { ...payment, account: undefined, payment_id: "p1", remittance: "waiting" } : payment],
    total: sgd("327.00"),
    checks: [],
    reapproval_needed: false,
    version: state.version,
    ...(state.status === "released"
      ? { release: { released_by: { kind: "user", id: me, name: "Mei Ling Tan" }, released_at: "2026-10-30T01:30:00Z", approval_id: "a-1", writer: "payment-list@1", file_name: list, sha256: "ab".repeat(32), payments: [{ payment_id: "p1" }] } }
      : {}),
  });
  const tag = () => `"${state.version}-${state.status}"`;
  await page.route("**/api/v1/**", async (route: Route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(/^\/api/, "");
    const key = `${req.method()} ${path}`;
    seen.push(key);
    const json = (status: number, body: unknown, headers: Record<string, string> = {}) =>
      route.fulfill({ status, headers, contentType: status >= 400 ? "application/problem+json" : "application/json", body: JSON.stringify(body) });
    switch (key) {
      case "GET /v1/setup":
        return json(200, { done: true });
      case "GET /v1/session":
        return json(200, session);
      case "GET /v1/organisation":
        return json(200, organisation);
      case "GET /v1/approvals":
        return json(200, { items: [] });
      case "GET /v1/review-log":
        return json(200, { items: [], unreviewed: 0, overdue: 0 });
      case "GET /v1/notifications":
        return json(200, { items: [], unread: 0 });
      case "GET /v1/banners":
        return json(200, { items: [] });
      case "GET /v1/payment-batches":
        return json(200, { items: state.status === "none" ? [] : [batch()] });
      case "GET /v1/money-accounts":
        return json(200, { items: [{ id: "m1", account_id: "a1", kind: "bank", name: "DBS current", identifier: "…8771", currency: "SGD" }] });
      case "GET /v1/payment-batches/suggestion":
        return json(200, { payment_date: new URL(req.url()).searchParams.get("payment_date"), suggested: items.map((x) => ({ kind: x.kind, document_id: x.document_id })) });
      case "POST /v1/payment-batches":
        state.status = "draft";
        return json(201, batch(), { ETag: tag() });
      case "GET /v1/payment-batches/b1":
        return json(200, batch(), { ETag: tag() });
      case "POST /v1/payment-batches/b1/approval":
        if (req.headers()["if-match"] !== tag()) return json(412, { code: "API-PRECONDITION-01", status: 412 });
        state.status = "waiting";
        state.version++;
        return json(202, { id: "a-1", status: "pending" });
      case "POST /v1/payment-batches/b1/release":
        if (req.headers()["if-match"] !== tag()) return json(412, { code: "API-PRECONDITION-01", status: 412 });
        state.status = "released";
        state.version++;
        return json(200, batch(), { ETag: tag() });
      case "GET /v1/payment-batches/b1/file":
        return json(200, { file_name: list, media_type: "text/csv", sha256: "ab".repeat(32), size_bytes: 3, content_base64: "YWJj" });
      default:
        missing.push(key);
        return route.fulfill({ status: 599, body: `no fixture for ${key}` });
    }
  });
  // approve stands for the other founder approving the request in their Inbox.
  const approve = () => {
    state.status = "approved";
    state.version++;
  };
  return { seen, missing, approve };
}

test("a batch is prepared from what is due, asked for, approved by the other founder, released and its list downloaded", async ({ page }) => {
  const { seen, missing, approve } = await batches(page);
  await page.goto("/purchases/payment-batches");
  await expect(page.getByRole("heading", { level: 2, name: "No payment batches yet" })).toBeVisible();
  await page.getByRole("button", { name: "New batch" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByLabel("Pay from")).toHaveValue("a1");
  await dialog.getByRole("button", { name: "Prepare the batch" }).click();

  await expect(page).toHaveURL(/\/purchases\/payment-batches\/b1$/);
  await expect(page.getByRole("heading", { level: 1, name: /30 Oct 2026 batch/ })).toBeVisible();
  await expect(page.getByRole("cell", { name: "Northwind Analytics Pte Ltd Supplier" })).toBeVisible();
  await page.getByRole("button", { name: "Ask for approval" }).click();
  await expect(page.getByText(/Asked for approval/)).toBeVisible();
  await expect(page.getByRole("link", { name: "Open the approval" })).toBeVisible();

  approve();
  await page.reload();
  await expect(page.getByRole("heading", { level: 1, name: /PB-0003/ })).toBeVisible();
  await page.getByRole("button", { name: "Release" }).click();
  await expect(page.getByText(/Released: its payments are posted/)).toBeVisible();
  await expect(page.getByText("Waiting for the bank")).toBeVisible();

  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download payment list" }).click();
  expect((await download).suggestedFilename()).toBe(list);
  await expect(page.getByText(/Downloaded PB-0003-payment-list.csv/)).toBeVisible();

  expect(seen.filter((k) => k.startsWith("POST"))).toEqual([
    "POST /v1/payment-batches",
    "POST /v1/payment-batches/b1/approval",
    "POST /v1/payment-batches/b1/release",
  ]);
  expect(missing).toEqual([]);
});
