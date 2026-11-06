import { expect, test, type Page, type Route } from "@playwright/test";

// A founder's account, from the accountant's page to the founder's own (p1-founders-money task 1.6), against the built
// web app in a real browser with every /api call answered by a fixture (the Go suites prove the figures): Accounting ›
// Director accounts lists Mei Lin, owed S$1,100.00, and Arjun, in debit; Mei Lin's statement shows S$1,500.00 lent and
// S$400.00 paid out to her, and downloads as CSV; then Mei Lin, signed in as herself, reads Money I put in.
// req: FR-EQ-01
// req: FR-EQ-11
// req: FR-EQ-14

const session = {
  provider: "local",
  user_id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01",
  organisation_id: "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02",
  roles: ["owner", "controller"],
  super_admin: false,
  step_up_method: "sign_in",
  features: { multi_currency: true, payroll: false, projects: false, portals: false, ai_assistant: false },
};
const organisation = { id: session.organisation_id, name: "Cognizhi Pte Ltd", base_currency: "SGD", base_minor_units: 2, team_mode: "founder", version: 1 };
const sgd = (amount: string) => ({ amount, currency: "SGD" });
const loan = { account_id: "acc-2500", code: "2500", name: "Loan from director / shareholder", role: "director-loan" };
const mei = {
  contact_id: "c-mei",
  legal_name: "Mei Lin Tan",
  director: true,
  shareholder: true,
  member_user_id: session.user_id,
  active: true,
  balances: [{ ...loan, balance: sgd("-1100.00") }],
  net: sgd("-1100.00"),
  in_debit: false,
};
const arjun = { ...mei, contact_id: "c-arjun", legal_name: "Arjun Rao", member_user_id: undefined, balances: [{ ...loan, balance: sgd("200.00") }], net: sgd("200.00"), in_debit: true };
const statement = {
  contact_id: "c-mei",
  legal_name: "Mei Lin Tan",
  from: "2026-01-01",
  to: "2026-10-31",
  currency: "SGD",
  accounts: [{ account_id: "acc-2500", code: "2500", name: "Loan from director / shareholder", opening: sgd("0.00"), by_origin: [{ origin: "lent", amount: sgd("-1500.00") }, { origin: "paid_out", amount: sgd("400.00") }], closing: sgd("-1100.00") }],
  movements: [
    { entry_id: "e-1", date: "2026-10-01", account_id: "acc-2500", origin: "lent", amount: sgd("-1500.00"), source_type: "bank_transaction", source_id: "b-1", reversal: false },
    { entry_id: "e-2", date: "2026-10-05", account_id: "acc-2500", origin: "paid_out", amount: sgd("400.00"), source_type: "manual_journal", source_id: "j-1", number: "JNL-0002", reversal: false },
  ],
};

async function serve(page: Page) {
  const seen: string[] = [];
  await page.route("**/api/v1/**", async (route: Route) => {
    const req = route.request();
    const url = new URL(req.url());
    const key = `${req.method()} ${url.pathname.replace(/^\/api/, "")}`;
    seen.push(key);
    const json = (status: number, body: unknown) => route.fulfill({ status, contentType: status >= 400 ? "application/problem+json" : "application/json", body: JSON.stringify(body) });
    switch (key) {
      case "GET /v1/setup":
        return json(200, { done: true });
      case "GET /v1/session":
        return json(200, session);
      case "GET /v1/organisation":
        return json(200, organisation);
      case "GET /v1/organisation/brand": // the header shows the logo (FR-ORG-09): none uploaded
        return json(200, { version: 0, accent_options: [] });
      case "GET /v1/director-accounts":
        return json(200, { as_at: "2026-10-31", currency: "SGD", items: [mei, arjun], unkeyed: sgd("0.00") });
      case "GET /v1/director-accounts/c-mei/statement":
        return json(200, statement);
      case "GET /v1/director-accounts/c-mei/statement/export":
        return json(200, { export_id: "x-1", file_name: "statement-mei-lin-tan-2026-01-01-to-2026-10-31.csv", media_type: "text/csv; charset=utf-8", sha256: "00", size_bytes: 3, rows: 2, ledger_revision: 7, content_base64: btoa("a,b") });
      case "GET /v1/me/director-account":
        return json(200, { linked: true, account: mei });
      case "GET /v1/approvals":
      case "GET /v1/notifications":
      case "GET /v1/banners":
        return json(200, { items: [] });
      default:
        return route.fulfill({ status: 403, contentType: "application/problem+json", body: JSON.stringify({ code: "API-AUTHZ-01", status: 403 }) });
    }
  });
  return seen;
}

test("the director accounts show each founder's balance and statement, and a founder reads their own", async ({ page }) => {
  const seen = await serve(page);
  await page.goto("/accounting/directors");
  const meiCard = page.locator("section", { has: page.getByRole("heading", { name: "Mei Lin Tan" }) });
  await expect(meiCard.getByText("The company owes Mei Lin Tan")).toBeVisible();
  await expect(page.locator("section", { has: page.getByRole("heading", { name: "Arjun Rao" }) }).getByText("Account in debit")).toBeVisible();

  await meiCard.getByRole("button", { name: "Statement" }).click();
  const table = page.getByRole("table");
  await expect(table.getByText("Lent to the company")).toBeVisible();
  await expect(table.getByText("1,500.00")).toBeVisible();
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "CSV" }).click();
  expect((await download).suggestedFilename()).toBe("statement-mei-lin-tan-2026-01-01-to-2026-10-31.csv");
  expect(seen).toContain("GET /v1/director-accounts/c-mei/statement/export");

  await page.goto("/me/money-i-put-in");
  await expect(page.getByRole("heading", { name: "The company owes you" })).toBeVisible();
  await expect(page.getByText("1,100.00 SGD")).toBeVisible();
});
