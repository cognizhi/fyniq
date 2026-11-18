import { api, expect, expert, organisation, test } from "./harness";

interface Walkthrough {
  role: string;
  name: string;
  landing: string; // the path the person lands on from /
  areas: string[]; // the sidebar's items, in Expert mode, as §7.1's table gives them
  pulse?: boolean; // Home shows the company pulse (cash, runway, books health): "the full Home" of §7.1, and never to a role it leaves out
  work?: RegExp[]; // the rows of Home's "Your work" (§7.1's "Home shows" beyond the pulse), on the page the role lands on; [] for none
  engagement?: boolean; // the Auditor's engagement on the page it lands on
  outside?: { path: string; read?: string }; // a page outside the person's areas, and the API read behind it
}

const all = ["Home", "Inbox", "Contacts", "Sales", "Purchases", "Expense claims", "Banking", "Reports", "Accounting", "Equity", "Planning", "Compliance", "Settings"];

// PRD-001 §7.1's table of what each role sees (the reference for QA-UI-08): each role's landing page, its areas, whether
// Home shows the company pulse, and a page outside its access with the read the API then refuses. Accountants,
// Controllers, Owners and Tax agents have every finance area and the full Home; the Approver, AR, AP and payroll roles
// never see the company's cash. Home's "Your work" holds the rest of each role's Home: the Bookkeeper's bank lines and
// drafts, the AR and AP specialists' overdue documents and what they work through, the payment releaser's batches,
// rejected payments and bank balances; the full-Home roles have none of its rows. The Administrator, the Company
// secretary and the DPO land on Settings or Compliance, which show their rows, and the Auditor on Reports, which shows
// its engagement. The Bookkeeper's and the payment releaser's pulse is not checked. The Auditor's workspace hides areas
// whose records Appendix D still lets it read, so only the page is checked for it.
const walkthroughs: Walkthrough[] = [
  { role: "employee", name: "Eve Employee", landing: "/me/claims", areas: ["My claims", "My shares and options", "Settings"], outside: { path: "/banking", read: "/v1/bank-lines" } },
  { role: "approver", name: "Abe Approver", landing: "/", areas: ["Home", "Inbox", "Reports", "Planning", "Settings"], pulse: false, outside: { path: "/banking", read: "/v1/bank-lines" } },
  { role: "bookkeeper", name: "Bea Bookkeeper", landing: "/", areas: ["Home", "Inbox", "Contacts", "Sales", "Purchases", "Expense claims", "Banking", "Reports", "Accounting", "Settings"], work: [/bank lines? to categorise/, /draft invoices? to finish/, /draft bills? to finish/], outside: { path: "/equity", read: "/v1/cap-table" } },
  { role: "ar_specialist", name: "Ari Receivables", landing: "/", areas: ["Home", "Inbox", "Contacts", "Sales", "Banking", "Reports", "Settings"], pulse: false, work: [/draft invoices? to finish/, /invoices? overdue/, /receipts? to apply/, /quotes? waiting for an answer/], outside: { path: "/accounting/journals", read: "/v1/journals" } },
  { role: "ap_specialist", name: "Apu Payables", landing: "/", areas: ["Home", "Inbox", "Contacts", "Purchases", "Expense claims", "Banking", "Reports", "Settings"], pulse: false, work: [/draft bills? to finish/, /bills? overdue/, /bills? to pay/, /payment batch(es)? being prepared/, /approved claims? to pay/, /bank-detail changes? waiting/], outside: { path: "/sales/invoices", read: "/v1/invoices" } },
  { role: "payment_releaser", name: "Rae Releaser", landing: "/", areas: ["Home", "Inbox", "Purchases", "Banking", "Reports", "Settings"], work: [/batch(es)? waiting for release/, /payments? the bank rejected/], outside: { path: "/sales/invoices", read: "/v1/invoices" } },
  { role: "payroll_administrator", name: "Pat Payroll", landing: "/", areas: ["Home", "Inbox", "Reports", "Settings"], pulse: false, work: [], outside: { path: "/banking", read: "/v1/bank-lines" } },
  { role: "accountant", name: "Cal Accountant", landing: "/", areas: all, pulse: true, work: [] },
  { role: "controller", name: "Cora Controller", landing: "/", areas: all, pulse: true, work: [] },
  { role: "owner", name: "Ann Owner", landing: "/", areas: ["Money I put in", ...all], pulse: true, work: [] },
  { role: "tax_agent", name: "Tam Tax", landing: "/", areas: all, pulse: true, work: [] },
  { role: "administrator", name: "Ada Admin", landing: "/settings", areas: ["Home", "Inbox", "Reports", "Compliance", "Settings"], work: [/(person|people) asking to join/, /invitations? waiting to be accepted/, /role changes? waiting for a second approver/, /integrations? failing/], outside: { path: "/sales/invoices", read: "/v1/invoices" } },
  { role: "auditor", name: "Aud Auditor", landing: "/reports", areas: ["Home", "Inbox", "Reports", "Accounting", "Compliance", "Settings"], engagement: true, outside: { path: "/banking" } },
  { role: "board_viewer", name: "Bo Board", landing: "/reports", areas: ["Home", "Inbox", "Reports", "Equity", "Settings"], outside: { path: "/banking", read: "/v1/bank-lines" } },
  { role: "company_secretary", name: "Cy Secretary", landing: "/compliance", areas: ["Home", "Inbox", "Equity", "Compliance", "Settings"], work: [/corporate items? to see to/, /register updates? waiting for approval/], outside: { path: "/banking", read: "/v1/bank-lines" } },
  { role: "dpo", name: "Dee Protection", landing: "/compliance", areas: ["Home", "Inbox", "Compliance", "Settings"], work: [/data requests? to answer/, /breach(es)? still open/], outside: { path: "/banking", read: "/v1/bank-lines" } },
];

// QA-UI-08's walkthroughs: each role of §7.1 alone in an organisation with the sample company, signed in through the
// local sign-in page. Scenarios: QA-UI-08 "Budget holder" (the Approver: no Banking, Payroll or Accounting, the
// not-available page for a bank link and the API's refusal of it) and "Two roles" (the union, in navigation.test.ts);
// FR-UX-39's personal workspace; FR-UX-40's areas and Home cut to the person's rights.
// req: QA-UI-08
// req: FR-UX-39
// req: FR-UX-40
for (const w of walkthroughs) {
  test(`${w.role}: lands where §7.1 says, sees its areas, and finds the rest not part of its access`, async ({ browser }) => {
    const j = await organisation(browser, `Walkthrough ${w.name} Pte Ltd`, [{ key: w.role.replaceAll("_", "-"), name: w.name, roles: [w.role] }]);
    const page = j.people[w.role.replaceAll("_", "-")]!.page;
    await page.goto("/");
    await expect(page).toHaveURL((u) => u.pathname === w.landing);
    if (w.role !== "employee") await expert(page);

    const sidebar = page.locator('nav[aria-label="Main"] .fy-nav__label');
    await expect(sidebar.first()).toBeVisible();
    expect(await sidebar.allTextContents()).toEqual(w.areas);

    if (w.pulse !== undefined) {
      await page.goto("/");
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
      await page.waitForLoadState("networkidle");
      await expect(page.getByRole("region", { name: /^Company pulse/ })).toHaveCount(w.pulse ? 1 : 0);
    }
    if (w.work) {
      await page.goto("/");
      await page.waitForLoadState("networkidle");
      const card = page.getByRole("region", { name: "Your work" });
      if (w.work.length === 0) await expect(card).toHaveCount(0);
      for (const row of w.work) await expect(card.getByRole("link", { name: row })).toBeVisible();
      if (w.role === "payment_releaser") {
        // The bank balances are the cash position's accounts: shown when the API has any, as it says.
        const cash = await api<{ accounts: unknown[] }>(page, "/v1/cash-position");
        await expect(card.getByRole("heading", { name: /^Bank balances on/ })).toHaveCount(cash.accounts.length > 0 ? 1 : 0);
      }
    }
    if (w.engagement) {
      await page.goto("/");
      const card = page.getByRole("region", { name: "Your engagement" });
      await expect(card.getByText(/^Read-only/)).toBeVisible();
      await expect(card.getByRole("link", { name: /Audit trail/ })).toBeVisible();
    }
    if (w.role !== "employee") {
      await page.goto("/inbox");
      await expect(page.getByText("This page isn't part of your access")).toHaveCount(0);
    }
    if (w.outside) {
      await page.goto(w.outside.path);
      await expect(page.getByRole("heading", { name: "This page isn't part of your access" })).toBeVisible();
      if (w.outside.read) {
        const r = await page.request.get(`/api${w.outside.read}`);
        expect(r.status(), `GET ${w.outside.read} as ${w.role}`).toBe(403);
      }
    }
  });
}
