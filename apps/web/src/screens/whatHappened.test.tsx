import { fireEvent, screen, within } from "@testing-library/react";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import type { Session } from "../api/client";
import { routes } from "../registry";
import { answer, ownerSession } from "../test/api";
import { noViolations } from "../test/axe";
import { renderPage } from "../test/render";
import { Contacts } from "./contacts/Contacts";
import { happenings, happeningsFor, WhatHappened } from "./WhatHappened";

// req: FR-UX-11
// req: experience/navigation FR-UX-11 Teaching empty states and contextual help: What happened, where to record it
describe("what happened, where to record it", () => {
  it("leads from what happened to the page that records it, and to the guide's page; axe finds nothing", async () => {
    const { container } = renderPage(WhatHappened);
    fireEvent.change(await screen.findByLabelText("Something happened"), { target: { value: "supplier-bill" } });
    const said = screen.getByRole("status");
    expect(said.textContent).toContain("Record it in Purchases › New bill. Enter the bill with its lines and GST");
    expect(within(said).getByRole("link", { name: "Purchases › New bill" }).getAttribute("href")).toBe("/purchases/bills/new");
    expect(within(said).getByRole("link", { name: "How it works, in the guide" }).getAttribute("href")).toMatch(/^\/help\/purchases\./);
    await noViolations(container);
  });

  it("names a page that exists for each, and lists only what the person's access records", () => {
    const paths = new Set(routes.map((r) => r.path));
    expect(happenings.filter((h) => !paths.has(h.to as never)).map((h) => h.id)).toEqual([]);
    const sales = happeningsFor({ roles: ["ar_specialist"] } as Session).map((h) => h.id);
    expect(sales).toContain("customer-paid");
    expect(sales).not.toContain("supplier-bill");
    expect(happeningsFor({ roles: ["employee"] } as Session).map((h) => h.id)).toEqual(["paid-personally"]);
    expect(happeningsFor(ownerSession).length).toBe(happenings.length);
  });
});

// An empty state, as a page's source has it: from `className="fy-empty` to the end of its element.
function emptyStates(root: string): { file: string; line: number; block: string }[] {
  const files = (d: string): string[] =>
    readdirSync(d).flatMap((n) => {
      const p = join(d, n);
      if (statSync(p).isDirectory()) return files(p);
      return /\.tsx$/.test(n) && !/\.(test|stories)\.tsx$/.test(n) ? [p] : [];
    });
  return files(root).flatMap((p) => {
    const s = readFileSync(p, "utf8");
    return [...s.matchAll(/className="fy-empty[ "]/g)].map((m) => {
      const start = s.lastIndexOf("<", m.index);
      const tag = /^<(\w+)/.exec(s.slice(start))?.[1] ?? "div";
      const re = new RegExp(`<(/?)${tag}\\b[^>]*?(/?)>`, "g");
      re.lastIndex = start;
      let depth = 0;
      let end = s.length;
      for (let t = re.exec(s); t; t = re.exec(s)) {
        if (!t[1] && !t[2]) depth++;
        else if (t[1]) depth--;
        if (depth === 0) {
          end = re.lastIndex;
          break;
        }
      }
      return { file: relative(root, p), line: s.slice(0, start).split("\n").length, block: s.slice(start, end) };
    });
  });
}

// The empty states with no first action of their own, and why: what fills them comes from elsewhere, or the control
// that fills them is right above.
const noAction: Record<string, string> = {
  "screens/equity/ExitWaterfall.tsx": "a class with a liquidation preference is recorded under Equity, as the page says",
  "screens/accounting/Schedules.tsx": "spreads come from a bill's, an invoice's or a bank line's lines",
  "screens/accounting/GeneralLedger.tsx": "the account picker is right above",
  "screens/settings/Templates.tsx": "the starter styles to begin from are right above",
  "screens/purchases/BillsBoard.tsx": "a column of the board with no bill in it",
  "screens/banking/StatementImport.tsx": "the file chooser is right below",
  "screens/banking/BankReconciliation.tsx": "Prepare is the page's own action, above",
  "screens/banking/AutoReconciled.tsx": "what Fyniq reconciled by itself",
  "screens/founders/MoneyIPutIn.tsx": "an Owner links the director's account",
  "screens/approvals/ReviewLog.tsx": "high-risk actions arrive as people act",
  "screens/claims/ClaimsReview.tsx": "claims arrive when members submit them",
  "screens/claims/CardLines.tsx": "finance imports the card's statement",
  "screens/claims/MyClaims.tsx": "finance advances money for a trip",
  "shell/NotPartOfAccess.tsx": "not a list: what a page outside the person's access says",
};

// req: experience/navigation FR-UX-11 Teaching empty states and contextual help: Empty list
describe("empty lists", () => {
  it("each explain what goes there and offer the first action, unless what fills them comes from elsewhere", () => {
    const all = emptyStates(resolve(__dirname, ".."));
    expect(all.length).toBeGreaterThan(30);
    const unexplained = all.filter((e) => !/<(p|span)\b[^>]*>[^<{]*[A-Za-z]/.test(e.block) && !/<p\b/.test(e.block)).map((e) => `${e.file}:${e.line}`);
    expect(unexplained).toEqual([]);
    const actionless = all.filter((e) => !/<(Link|Button|button|a)\b/.test(e.block) && !noAction[e.file]).map((e) => `${e.file}:${e.line}`);
    expect(actionless).toEqual([]);
  });

  it("offers adding the first contact to someone who may", async () => {
    answer({ "GET /v1/contacts": { status: 200, body: { items: [] } } });
    renderPage(Contacts, { path: "/contacts", session: { ...ownerSession, roles: ["bookkeeper"] } as Session });
    expect(await screen.findByRole("heading", { name: "No contacts yet" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Add your first contact" }));
    expect(await screen.findByRole("dialog", { name: "New contact" })).toBeTruthy();
  });
});
