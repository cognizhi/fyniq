import { BookOpen, House, Inbox, Landmark, Receipt, ReceiptText, Wallet } from "lucide-react";
import { describe, expect, it } from "vitest";
import type { Session } from "../api/client";
import { nav, routes, settingsNav, type NavFeature } from "../registry";
import { areaOf, bottomBar, landingOf, mayOpen, navFor } from "./navigation";

// req: FR-UX-40
// req: experience/modes Basic and Expert navigation
// Items as later modules will register them; the registry's own items are checked at the end.
const item = (id: string, over: Partial<NavFeature>): NavFeature => ({
  kind: "nav",
  id,
  route: `/${id}`,
  icon: House,
  group: "main",
  roles: "all",
  mode: "basic",
  help: "home.overview",
  guide: "home.overview",
  ...over,
});

const items = [
  item("my-claims", { group: "personal", icon: ReceiptText, roles: ["employee", "owner"] }),
  item("home", { route: "/", bottom: true }),
  item("inbox", { icon: Inbox, roles: ["owner", "bookkeeper", "accountant"], bottom: true }),
  item("sales", { icon: Receipt, roles: ["owner", "bookkeeper", "ar_specialist"], bottom: true }),
  item("banking", { icon: Landmark, roles: ["owner", "bookkeeper"], bottom: true }),
  item("payroll", { icon: Wallet, roles: ["owner", "payroll_administrator"], feature: "payroll" }),
  item("accounting", { icon: BookOpen, roles: ["owner", "accountant"], mode: "expert" }),
];

const off = { multi_currency: false, payroll: false, projects: false, portals: false, ai_assistant: false };
const as = (roles: string[], features = off): Pick<Session, "roles" | "features"> => ({ roles, features });
const ids = (xs: NavFeature[]) => xs.map((x) => x.id);

describe("navigation cut to the person", () => {
  it("shows each role only its areas", () => {
    expect(ids(navFor(items, as(["employee"]), "expert"))).toEqual(["my-claims"]); // the personal workspace (FR-UX-39)
    expect(ids(navFor(items, as(["ar_specialist"]), "expert"))).toEqual(["home", "sales"]);
    expect(ids(navFor(items, as(["bookkeeper", "ar_specialist"]), "expert"))).toEqual(["home", "inbox", "sales", "banking"]);
  });

  it("hides an area the organisation switched off", () => {
    expect(ids(navFor(items, as(["owner"]), "expert"))).not.toContain("payroll");
    expect(ids(navFor(items, as(["owner"], { ...off, payroll: true }), "expert"))).toContain("payroll");
    expect(ids(navFor(items, { roles: ["owner"], features: undefined }, "expert"))).not.toContain("payroll");
  });

  it("hides Expert-only areas in Basic mode, and only those", () => {
    expect(ids(navFor(items, as(["owner"]), "basic"))).toEqual(["my-claims", "home", "inbox", "sales", "banking"]);
    expect(ids(navFor(items, as(["owner"]), "expert"))).toContain("accounting");
  });

  // req: FR-ADM-08
  it("shows the installation's pages to a Super admin only", () => {
    expect(ids(navFor(settingsNav, { ...as(["owner"]), super_admin: true }, "expert"))).toContain("settings.sign-in-providers");
    expect(ids(navFor(settingsNav, as(["owner", "administrator"]), "expert"))).not.toContain("settings.sign-in-providers");
  });

  it("puts at most four of the person's items in the compact bottom bar, before More", () => {
    const owner = navFor(items, as(["owner"], { ...off, payroll: true }), "expert");
    expect(ids(bottomBar(owner))).toEqual(["home", "inbox", "sales", "banking"]);
    expect(bottomBar(navFor(items, as(["ar_specialist"]), "basic"))).toEqual([items[1], items[3]]);
  });

  it("registers every navigation item on a known route, at most 13 in the sidebar", () => {
    const paths = new Set<string>(routes.map((r) => r.path));
    for (const n of nav) expect(paths.has(n.route), n.id).toBe(true);
    expect(nav.filter((n) => n.group === "main").length).toBeLessThanOrEqual(13);
  });
});

// What each role sees, as PRD-001 §7.1's table says it (FR-UX-40; the reference for QA-UI-08): the areas of the
// registry's sidebar for each role alone, in Expert mode, with the switched features off. Every finance role also has
// Home, which takes it to its landing page, the Inbox, where approvals arrive, and Settings, where its profile is.
// req: FR-UX-40
// req: FR-UX-39
// req: QA-UI-08
describe("§7.1's table of what each role sees", () => {
  const all = ["nav.contacts", "nav.sales", "nav.purchases", "nav.claims-review", "nav.banking", "nav.reports", "nav.accounting", "nav.equity", "nav.planning", "nav.compliance"];
  const finance = (...areas: string[]) => ["nav.home", "nav.inbox", ...areas, "nav.settings"];
  const table: Record<string, string[]> = {
    employee: ["nav.my-claims", "nav.my-shares", "nav.settings"],
    approver: finance("nav.reports", "nav.planning"),
    bookkeeper: finance("nav.contacts", "nav.sales", "nav.purchases", "nav.claims-review", "nav.banking", "nav.reports", "nav.accounting"),
    ar_specialist: finance("nav.contacts", "nav.sales", "nav.banking", "nav.reports"),
    ap_specialist: finance("nav.contacts", "nav.purchases", "nav.claims-review", "nav.banking", "nav.reports"),
    payment_releaser: finance("nav.purchases", "nav.banking", "nav.reports"),
    payroll_administrator: finance("nav.reports"),
    accountant: finance(...all),
    controller: finance(...all),
    owner: ["nav.money-i-put-in", ...finance(...all)],
    tax_agent: finance(...all),
    administrator: finance("nav.reports", "nav.compliance"),
    auditor: finance("nav.reports", "nav.accounting", "nav.compliance"),
    board_viewer: finance("nav.reports", "nav.equity"),
    company_secretary: finance("nav.equity", "nav.compliance"),
    dpo: finance("nav.compliance"),
  };
  const order = (xs: string[]) => nav.map((n) => n.id).filter((id) => xs.includes(id));

  for (const [role, areas] of Object.entries(table)) {
    it(`gives ${role} its areas and no other`, () => {
      expect(ids(navFor(nav, as([role]), "expert"))).toEqual(order(areas));
    });
  }

  it("gives a person with two roles the union of their areas", () => {
    expect(ids(navFor(nav, as(["ar_specialist", "ap_specialist"]), "expert"))).toEqual(order([...table.ar_specialist!, ...table.ap_specialist!]));
  });

  it("finds the area a page is in, the longest route first", () => {
    expect(areaOf(nav, "/purchases/claims/0192")?.id).toBe("nav.claims-review");
    expect(areaOf(nav, "/purchases/orders/new")?.id).toBe("nav.purchases");
    expect(areaOf(nav, "/sales/quotes")?.id).toBe("nav.sales");
    expect(areaOf(nav, "/planning/budgets")?.id).toBe("nav.planning");
    expect(areaOf(nav, "/settings/profile")).toBeUndefined();
    expect(areaOf(nav, "/inbox")).toBeUndefined();
  });

  it("opens a page only inside the person's areas, and a person with personal rights only their own pages", () => {
    expect(mayOpen(nav, as(["ar_specialist"]), "/sales/invoices/new")).toBe(true);
    expect(mayOpen(nav, as(["ar_specialist"]), "/accounting/journals")).toBe(false);
    expect(mayOpen(nav, as(["ar_specialist", "accountant"]), "/accounting/journals")).toBe(true);
    expect(mayOpen(nav, as(["approver"]), "/purchases/bills")).toBe(false);
    expect(mayOpen(nav, as(["approver"]), "/inbox")).toBe(true);
    expect(mayOpen(nav, as(["employee"]), "/me/claims/new")).toBe(true);
    expect(mayOpen(nav, as(["employee"]), "/settings/profile")).toBe(true);
    expect(mayOpen(nav, as(["employee"]), "/inbox")).toBe(false);
    expect(mayOpen(nav, as(["employee"]), "/banking/accounts/0192")).toBe(false);
  });

  it("lands each role where §7.1 says", () => {
    expect(landingOf(["employee"])).toBe("/me/claims");
    expect(landingOf(["bookkeeper"])).toBeUndefined();
    expect(landingOf(["administrator"])).toBe("/settings");
    expect(landingOf(["company_secretary"])).toBe("/compliance");
    expect(landingOf(["dpo"])).toBe("/compliance");
    expect(landingOf(["board_viewer"])).toBe("/reports");
    expect(landingOf(["auditor"])).toBe("/reports");
    expect(landingOf(["administrator", "accountant"])).toBeUndefined(); // Home wins
  });
});
