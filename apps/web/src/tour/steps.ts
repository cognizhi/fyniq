import { personalOnly } from "../shell/navigation";

// The first-run tour's steps (FR-UX-34; MASTER.md §12 "Walkthrough"; docs/design/screens Tour): at most seven, a
// variant per role, each on an element of the real interface marked data-tour. A step whose element is not shown (the
// mode, the breakpoint, a feature switch) is skipped. The tour only points: it never clicks, types or changes data.

export interface Step {
  id: string;
  target: string; // the data-tour value of the element it points at
  title: string;
  text: string;
}

const all: Record<string, Step> = {
  mode: { id: "mode", target: "mode", title: "Basic or Expert: your choice", text: "Basic shows the essentials. Expert shows every feature your roles allow. Switch any time: nothing you entered is lost." },
  home: { id: "home", target: "home", title: "Today at a glance", text: "Cash, runway and books health, and the work waiting for you. The ⓘ beside a figure says how it is worked out." },
  inbox: { id: "inbox", target: "nav.inbox", title: "Your Inbox", text: "Approvals and anything that needs you. Nothing changes until a person approves it." },
  banking: { id: "banking", target: "nav.banking", title: "Record money in and out", text: "Bank lines arrive in Banking. Accept a suggestion or choose a category: nothing posts until you do." },
  accounting: { id: "accounting", target: "nav.accounting", title: "The books", text: "Journals, the chart of accounts and each month's close are in Accounting." },
  reports: { id: "reports", target: "nav.reports", title: "Reports", text: "The statements and the KPIs, each figure traceable to where it comes from." },
  claims: { id: "claims", target: "nav.my-claims", title: "Your expense claims", text: "Claim back what you paid for the company, with its receipt, and follow it until it is paid." },
  search: { id: "search", target: "search", title: "Find anything", text: "Press Ctrl K, or ⌘K on a Mac, to search the records, go to a page or run a command." },
  assistant: { id: "assistant", target: "assistant", title: "Ask the assistant", text: "Ask about your books. Its answers cite their sources, and it only proposes changes for a person to approve." },
  help: { id: "help", target: "help", title: "Help is always here", text: "The user guide, this tour and the keyboard shortcuts are under Help." },
};

export type Variant = "founder" | "accountant" | "employee" | "approver" | "board";

// variants are each role group's steps, never more than seven.
export const variants: Record<Variant, readonly string[]> = {
  founder: ["mode", "home", "inbox", "banking", "search", "assistant", "help"],
  accountant: ["mode", "home", "inbox", "accounting", "search", "assistant", "help"],
  employee: ["mode", "claims", "search", "assistant", "help"],
  approver: ["mode", "home", "inbox", "reports", "search", "assistant", "help"],
  board: ["mode", "home", "reports", "search", "help"],
};

const keepers = ["accountant", "controller", "bookkeeper", "tax_agent", "ar_specialist", "ap_specialist", "payroll_administrator"];

// variantOf is a person's variant by their roles: the Owner's the founder's; then the books' keepers; then approvers;
// then those who read the reports; a person with their own items only the employee's.
export function variantOf(roles: readonly string[] | null | undefined): Variant {
  const r = roles ?? [];
  if (personalOnly(r)) return "employee";
  if (r.includes("owner")) return "founder";
  if (r.some((x) => keepers.includes(x))) return "accountant";
  if (r.includes("approver") || r.includes("payment_releaser")) return "approver";
  if (r.some((x) => ["board_viewer", "auditor"].includes(x))) return "board";
  return "founder";
}

export function stepsFor(roles: readonly string[] | null | undefined): Step[] {
  return variants[variantOf(roles)].map((id) => all[id]!).filter(Boolean);
}
