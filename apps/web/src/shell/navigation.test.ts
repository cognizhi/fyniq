import { BookOpen, House, Inbox, Landmark, Receipt, ReceiptText, Wallet } from "lucide-react";
import { describe, expect, it } from "vitest";
import type { Session } from "../api/client";
import { nav, routes, type NavFeature } from "../registry";
import { bottomBar, navFor } from "./navigation";

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
    expect(ids(navFor(items, as(["employee"]), "expert"))).toEqual(["my-claims", "home"]);
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

  it("puts at most four of the person's items in the compact bottom bar, then More", () => {
    const owner = navFor(items, as(["owner"], { ...off, payroll: true }), "expert");
    const bar = bottomBar(owner);
    expect(ids(bar.items)).toEqual(["home", "inbox", "sales", "banking"]);
    expect(bar.more).toBe(true);
    expect(bottomBar(navFor(items, as(["ar_specialist"]), "basic"))).toEqual({ items: [items[1], items[3]], more: false });
  });

  it("registers every navigation item on a known route, at most 13 in the sidebar", () => {
    const paths = new Set<string>(routes.map((r) => r.path));
    for (const n of nav) expect(paths.has(n.route), n.id).toBe(true);
    expect(nav.filter((n) => n.group === "main").length).toBeLessThanOrEqual(13);
  });
});
