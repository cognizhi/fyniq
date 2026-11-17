import { expect, expert, organisation, test } from "./harness";

interface Walkthrough {
  role: string;
  name: string;
  landing: string; // the path the person lands on from /
  areas: string[]; // the sidebar's items, in Expert mode, as §7.1's table gives them
  pulse?: boolean; // Home shows the company pulse (cash, runway, books health): "the full Home" of §7.1, and never to a role it leaves out
  outside?: { path: string; read?: string }; // a page outside the person's areas, and the API read behind it
}

const all = ["Home", "Inbox", "Contacts", "Sales", "Purchases", "Expense claims", "Banking", "Reports", "Accounting", "Equity", "Planning", "Compliance", "Settings"];

// PRD-001 §7.1's table of what each role sees (the reference for QA-UI-08): each role's landing page, its areas, whether
// Home shows the company pulse, and a page outside its access with the read the API then refuses. Accountants,
// Controllers, Owners and Tax agents have every finance area and the full Home; the Approver, AR, AP and payroll roles
// never see the company's cash. The Bookkeeper's and the Payment releaser's Home blocks (cash today, bank balances)
// are not built yet, so their pulse is not checked. The Auditor's workspace hides areas whose records Appendix D still
// lets it read, so only the page is checked for it.
const walkthroughs: Walkthrough[] = [
  { role: "employee", name: "Eve Employee", landing: "/me/claims", areas: ["My claims", "My shares and options", "Settings"], outside: { path: "/banking", read: "/v1/bank-lines" } },
  { role: "approver", name: "Abe Approver", landing: "/", areas: ["Home", "Inbox", "Reports", "Planning", "Settings"], pulse: false, outside: { path: "/banking", read: "/v1/bank-lines" } },
  { role: "bookkeeper", name: "Bea Bookkeeper", landing: "/", areas: ["Home", "Inbox", "Contacts", "Sales", "Purchases", "Expense claims", "Banking", "Reports", "Accounting", "Settings"], outside: { path: "/equity", read: "/v1/cap-table" } },
  { role: "ar_specialist", name: "Ari Receivables", landing: "/", areas: ["Home", "Inbox", "Contacts", "Sales", "Banking", "Reports", "Settings"], pulse: false, outside: { path: "/accounting/journals", read: "/v1/journals" } },
  { role: "ap_specialist", name: "Apu Payables", landing: "/", areas: ["Home", "Inbox", "Contacts", "Purchases", "Expense claims", "Banking", "Reports", "Settings"], pulse: false, outside: { path: "/sales/invoices", read: "/v1/invoices" } },
  { role: "payment_releaser", name: "Rae Releaser", landing: "/", areas: ["Home", "Inbox", "Purchases", "Banking", "Reports", "Settings"], outside: { path: "/sales/invoices", read: "/v1/invoices" } },
  { role: "payroll_administrator", name: "Pat Payroll", landing: "/", areas: ["Home", "Inbox", "Reports", "Settings"], pulse: false, outside: { path: "/banking", read: "/v1/bank-lines" } },
  { role: "accountant", name: "Cal Accountant", landing: "/", areas: all, pulse: true },
  { role: "controller", name: "Cora Controller", landing: "/", areas: all, pulse: true },
  { role: "owner", name: "Ann Owner", landing: "/", areas: ["Money I put in", ...all], pulse: true },
  { role: "tax_agent", name: "Tam Tax", landing: "/", areas: all, pulse: true },
  { role: "administrator", name: "Ada Admin", landing: "/settings", areas: ["Home", "Inbox", "Reports", "Compliance", "Settings"], outside: { path: "/sales/invoices", read: "/v1/invoices" } },
  { role: "auditor", name: "Aud Auditor", landing: "/reports", areas: ["Home", "Inbox", "Reports", "Accounting", "Compliance", "Settings"], outside: { path: "/banking" } },
  { role: "board_viewer", name: "Bo Board", landing: "/reports", areas: ["Home", "Inbox", "Reports", "Equity", "Settings"], outside: { path: "/banking", read: "/v1/bank-lines" } },
  { role: "company_secretary", name: "Cy Secretary", landing: "/compliance", areas: ["Home", "Inbox", "Equity", "Compliance", "Settings"], outside: { path: "/banking", read: "/v1/bank-lines" } },
  { role: "dpo", name: "Dee Protection", landing: "/compliance", areas: ["Home", "Inbox", "Compliance", "Settings"], outside: { path: "/banking", read: "/v1/bank-lines" } },
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
