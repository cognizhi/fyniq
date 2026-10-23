// The feature registry (PRD-001 §7.8, §7.9; FR-UX-29, FR-UX-31; openspec/changes/p1-app-shell
// design.md): every route, navigation item, field group, column set and action of the web app, in
// one place, with
// - its experience mode: shown in Basic, or only in Expert (presentation only, FR-UX-24);
// - its help key: the tooltip text lives under that key in src/i18n/en-SG.json, never in code;
// - its guide page in docs/user-guide/ (the page's front-matter `key`, optionally "#<heading>"),
//   which the knowledge base in kb/ compiles from the same file;
// - for a route, its path and whether it needs a session;
// - for a navigation item, its route, icon, group, the roles that see it (PRD-001 §7.1) and the
//   organisation's feature switch it needs (FR-ORG-06). The shell cuts the navigation to the
//   person's roles and switches (FR-UX-40); the API still refuses what a role may not do.
// The router, the sidebar, the bottom bar and the command palette read it; later the mode filter,
// the help panel and the tour. registry.test.ts fails when an entry lacks a mode, a help key, its
// tooltip or its guide page (QA-UI-05), and when the router has a route the registry does not list.
import { BookCheck, BookOpen, Building, Contact, House, Inbox, ReceiptText, Scale, Settings, ShieldCheck, Stamp, UserRound, Users, type LucideIcon } from "lucide-react";
import type { components } from "./api/schema";

export type Mode = "basic" | "expert";
export type FeatureSwitch = keyof components["schemas"]["Features"];

interface Feature {
  id: string;
  mode: Mode;
  help: string;
  guide: string;
}

export interface RouteFeature extends Feature {
  kind: "route";
  path: string;
  // signedIn: the page needs a session; the API still decides what it shows (the server enforces
  // every right, FR-UX-40).
  signedIn: boolean;
}

export interface ActionFeature extends Feature {
  kind: "action";
}

export interface NavFeature extends Feature {
  kind: "nav";
  route: string; // a registry route's path
  icon: LucideIcon;
  // personal and main are the sidebar's sections, footer its foot (Settings); you and organisation
  // are the sections of the Settings page's own navigation.
  group: "personal" | "main" | "footer" | "you" | "organisation";
  roles: "all" | readonly string[]; // role names as GET /v1/session returns them
  feature?: FeatureSwitch;
  bottom?: boolean; // one of the compact bottom bar's items (at most four, then More)
}

export type Entry = RouteFeature | ActionFeature | NavFeature;

export const routes = [
  { kind: "route", id: "home", path: "/", mode: "basic", signedIn: true, help: "home.overview", guide: "home.overview" },
  { kind: "route", id: "sign-in", path: "/sign-in", mode: "basic", signedIn: false, help: "auth.sign-in", guide: "auth.sign-in" },
  { kind: "route", id: "install", path: "/install", mode: "basic", signedIn: false, help: "installation.setup", guide: "installation.setup" },
  { kind: "route", id: "inbox", path: "/inbox", mode: "basic", signedIn: true, help: "inbox.overview", guide: "approvals.decide#approve-or-reject" },
  { kind: "route", id: "approval", path: "/approvals/$approvalId", mode: "basic", signedIn: true, help: "approvals.request", guide: "approvals.decide#approve-or-reject" },
  { kind: "route", id: "review-log", path: "/review-log", mode: "basic", signedIn: true, help: "approvals.review-log", guide: "approvals.decide#review-high-risk-actions" },
  { kind: "route", id: "contacts", path: "/contacts", mode: "basic", signedIn: true, help: "contacts.overview", guide: "contacts.manage" },
  { kind: "route", id: "items", path: "/items", mode: "basic", signedIn: true, help: "items.overview", guide: "contacts.manage#items-and-prices" },
  { kind: "route", id: "bills", path: "/purchases/bills", mode: "basic", signedIn: true, help: "purchases.bills", guide: "purchases.bills" },
  { kind: "route", id: "bill-new", path: "/purchases/bills/new", mode: "basic", signedIn: true, help: "bill.new", guide: "purchases.bills#enter-a-bill" },
  { kind: "route", id: "credit-new", path: "/purchases/credits/new", mode: "basic", signedIn: true, help: "credit.new", guide: "purchases.bills#credit-notes" },
  { kind: "route", id: "bill", path: "/purchases/bills/$billId", mode: "basic", signedIn: true, help: "purchases.bill", guide: "purchases.bills#check-and-post-a-bill" },
  { kind: "route", id: "purchase-orders", path: "/purchases/orders", mode: "basic", signedIn: true, help: "purchases.orders", guide: "purchases.orders" },
  { kind: "route", id: "purchase-order-new", path: "/purchases/orders/new", mode: "basic", signedIn: true, help: "order.new", guide: "purchases.orders#raise-an-order" },
  { kind: "route", id: "purchase-order", path: "/purchases/orders/$orderId", mode: "basic", signedIn: true, help: "purchases.order", guide: "purchases.orders#approve-an-order" },
  { kind: "route", id: "recurring-bills", path: "/purchases/bills/recurring", mode: "basic", signedIn: true, help: "purchases.recurring-bills", guide: "purchases.bills#recurring-bills" },
  { kind: "route", id: "recurring-bill-new", path: "/purchases/bills/recurring/new", mode: "basic", signedIn: true, help: "recurring-bill.new", guide: "purchases.bills#recurring-bills" },
  { kind: "route", id: "recurring-bill", path: "/purchases/bills/recurring/$recurringBillId", mode: "basic", signedIn: true, help: "purchases.recurring-bill", guide: "purchases.bills#recurring-bills" },
  { kind: "route", id: "accounting", path: "/accounting", mode: "expert", signedIn: true, help: "nav.accounting", guide: "accounting.journals" },
  { kind: "route", id: "journals", path: "/accounting/journals", mode: "expert", signedIn: true, help: "accounting.journals", guide: "accounting.journals" },
  { kind: "route", id: "journal-new", path: "/accounting/journals/new", mode: "expert", signedIn: true, help: "journal.new", guide: "accounting.journals#prepare-a-journal" },
  { kind: "route", id: "journal", path: "/accounting/journals/$journalId", mode: "expert", signedIn: true, help: "accounting.journal", guide: "accounting.journals#check-and-post-a-journal" },
  { kind: "route", id: "recurring-journals", path: "/accounting/journals/recurring", mode: "expert", signedIn: true, help: "accounting.recurring", guide: "accounting.journals#recurring-journals" },
  { kind: "route", id: "recurring-journal-new", path: "/accounting/journals/recurring/new", mode: "expert", signedIn: true, help: "recurring.new", guide: "accounting.journals#recurring-journals" },
  { kind: "route", id: "recurring-journal", path: "/accounting/journals/recurring/$recurringId", mode: "expert", signedIn: true, help: "accounting.recurring-journal", guide: "accounting.journals#recurring-journals" },
  { kind: "route", id: "periods", path: "/accounting/close", mode: "expert", signedIn: true, help: "accounting.periods", guide: "accounting.periods" },
  { kind: "route", id: "opening-balances", path: "/accounting/opening-balances", mode: "expert", signedIn: true, help: "accounting.opening-balances", guide: "accounting.opening-balances" },
  { kind: "route", id: "period-close", path: "/accounting/close/$periodId", mode: "expert", signedIn: true, help: "accounting.period-close", guide: "accounting.periods#close-a-month" },
  { kind: "route", id: "general-ledger", path: "/reports/general-ledger", mode: "expert", signedIn: true, help: "reports.general-ledger", guide: "reports.general-ledger" },
  { kind: "route", id: "chart-of-accounts", path: "/accounting/chart-of-accounts", mode: "expert", signedIn: true, help: "accounting.chart", guide: "accounting.chart" },
  { kind: "route", id: "currencies", path: "/accounting/fx", mode: "expert", signedIn: true, help: "accounting.currencies", guide: "accounting.currencies" },
  { kind: "route", id: "settings", path: "/settings", mode: "basic", signedIn: true, help: "settings.overview", guide: "home.overview#settings" },
  { kind: "route", id: "settings-profile", path: "/settings/profile", mode: "basic", signedIn: true, help: "settings.profile", guide: "preferences.security" },
  { kind: "route", id: "settings-organisation", path: "/settings/organisation", mode: "expert", signedIn: true, help: "settings.organisation", guide: "settings.organisation" },
  { kind: "route", id: "settings-accounting", path: "/settings/accounting", mode: "expert", signedIn: true, help: "settings.accounting", guide: "settings.accounting" },
  { kind: "route", id: "settings-statutory-packs", path: "/settings/statutory-packs", mode: "expert", signedIn: true, help: "settings.statutory-packs", guide: "settings.statutory-packs" },
  { kind: "route", id: "settings-members", path: "/settings/members", mode: "expert", signedIn: true, help: "settings.members", guide: "settings.members" },
  { kind: "route", id: "settings-team-mode", path: "/settings/team-mode", mode: "expert", signedIn: true, help: "settings.team-mode", guide: "settings.team-mode" },
  { kind: "route", id: "settings-approval-policies", path: "/settings/approval-policies", mode: "expert", signedIn: true, help: "settings.approval-policies", guide: "approvals.policies" },
] as const satisfies readonly RouteFeature[];

export const actions = [
  { kind: "action", id: "command-palette", mode: "basic", help: "shell.command-palette", guide: "home.overview#find-anything-with-search" },
  { kind: "action", id: "appearance.theme", mode: "basic", help: "appearance.theme", guide: "preferences.appearance#theme" },
  { kind: "action", id: "appearance.contrast", mode: "basic", help: "appearance.contrast", guide: "preferences.appearance#high-contrast" },
  // Basic mode always uses Comfortable; Expert may choose (experience/modes "Basic and Expert density").
  { kind: "action", id: "appearance.density", mode: "expert", help: "appearance.density", guide: "preferences.appearance#density" },
  { kind: "action", id: "appearance.motion", mode: "basic", help: "appearance.motion", guide: "preferences.appearance#motion" },
  { kind: "action", id: "sign-out", mode: "basic", help: "auth.sign-out", guide: "home.overview#sign-out" },
  { kind: "action", id: "approval.approve", mode: "basic", help: "approval.approve", guide: "approvals.decide#approve-or-reject" },
  { kind: "action", id: "approval.reject", mode: "basic", help: "approval.reject", guide: "approvals.decide#approve-or-reject" },
  { kind: "action", id: "approval.withdraw", mode: "basic", help: "approval.withdraw", guide: "approvals.decide#withdraw-your-request" },
  { kind: "action", id: "approval.copy-hash", mode: "basic", help: "approval.copy-hash", guide: "approvals.decide#approve-or-reject" },
  { kind: "action", id: "review.mark", mode: "basic", help: "review.mark", guide: "approvals.decide#review-high-risk-actions" },
  { kind: "action", id: "member.invite", mode: "expert", help: "member.invite", guide: "settings.members#invite-someone" },
  { kind: "action", id: "member.change-roles", mode: "expert", help: "member.change-roles", guide: "settings.members#change-someones-roles" },
  { kind: "action", id: "member.add-role", mode: "expert", help: "member.add-role", guide: "settings.members#change-someones-roles" },
  { kind: "action", id: "member.end-role", mode: "expert", help: "member.end-role", guide: "settings.members#change-someones-roles" },
  { kind: "action", id: "member.withdraw-invitation", mode: "expert", help: "member.withdraw-invitation", guide: "settings.members#invite-someone" },
  { kind: "action", id: "team-mode.switch", mode: "expert", help: "team-mode.switch", guide: "settings.team-mode#switch-the-team-mode" },
  { kind: "action", id: "approval-policy.change", mode: "expert", help: "approval-policy.change", guide: "approvals.policies#change-a-policy" },
  { kind: "action", id: "setting.change", mode: "expert", help: "setting.change", guide: "settings.organisation#change-a-setting" },
  { kind: "action", id: "setting.reset", mode: "expert", help: "setting.reset", guide: "settings.organisation#change-a-setting" },
  { kind: "action", id: "accounting.record-framework", mode: "expert", help: "accounting.record-framework", guide: "settings.accounting#record-the-framework" },
  { kind: "action", id: "accounting.confirm-framework", mode: "expert", help: "accounting.confirm-framework", guide: "settings.accounting#confirm-and-approve" },
  { kind: "action", id: "accounting.record-policy", mode: "expert", help: "accounting.record-policy", guide: "settings.accounting#write-a-policy" },
  { kind: "action", id: "accounting.approve-policy", mode: "expert", help: "accounting.approve-policy", guide: "settings.accounting#confirm-and-approve" },
  { kind: "action", id: "pack.activate", mode: "expert", help: "pack.activate", guide: "settings.statutory-packs#activate-a-version" },
  { kind: "action", id: "review-banner", mode: "basic", help: "review.awaiting-verification", guide: "review.awaiting-verification" },
  { kind: "action", id: "contact.add", mode: "basic", help: "contact.add", guide: "contacts.manage#add-a-contact" },
  { kind: "action", id: "item.add", mode: "basic", help: "item.add", guide: "contacts.manage#items-and-prices" },
  { kind: "action", id: "chart.add", mode: "expert", help: "chart.add", guide: "accounting.chart#add-an-account" },
  { kind: "action", id: "chart.load-standard", mode: "expert", help: "chart.load-standard", guide: "accounting.chart#start-from-the-standard-chart" },
  { kind: "action", id: "bill.save", mode: "basic", help: "bill.save", guide: "purchases.bills#enter-a-bill" },
  { kind: "action", id: "bill.post", mode: "basic", help: "bill.post", guide: "purchases.bills#check-and-post-a-bill" },
  { kind: "action", id: "bill.ask-approval", mode: "basic", help: "bill.ask-approval", guide: "purchases.bills#bills-above-the-self-approval-threshold" },
  { kind: "action", id: "bill.void", mode: "basic", help: "bill.void", guide: "purchases.bills#void-or-edit-a-posted-bill" },
  { kind: "action", id: "bill.ask-void", mode: "basic", help: "bill.ask-void", guide: "purchases.bills#void-or-edit-a-posted-bill" },
  { kind: "action", id: "bill.edit", mode: "basic", help: "bill.edit", guide: "purchases.bills#void-or-edit-a-posted-bill" },
  { kind: "action", id: "bill.post-edit", mode: "basic", help: "bill.post-edit", guide: "purchases.bills#void-or-edit-a-posted-bill" },
  { kind: "action", id: "bill.ask-edit", mode: "basic", help: "bill.ask-edit", guide: "purchases.bills#void-or-edit-a-posted-bill" },
  { kind: "action", id: "credit.apply", mode: "basic", help: "credit.apply", guide: "purchases.bills#credit-notes" },
  { kind: "action", id: "credit.refund", mode: "basic", help: "credit.refund", guide: "purchases.bills#credit-notes" },
  { kind: "action", id: "bill.attach-evidence", mode: "basic", help: "bill.attach-evidence", guide: "purchases.bills#attach-the-suppliers-invoice" },
  { kind: "action", id: "bill.record-payment", mode: "basic", help: "bill.record-payment", guide: "purchases.bills#record-a-payment" },
  // A line's spread is an Expert field group (§7.8: service periods and spreads); a bill that has one shows it, and its
  // schedule, in both modes.
  { kind: "action", id: "order.save", mode: "basic", help: "order.save", guide: "purchases.orders#raise-an-order" },
  { kind: "action", id: "order.approve", mode: "basic", help: "order.approve", guide: "purchases.orders#approve-an-order" },
  { kind: "action", id: "order.ask-approval", mode: "basic", help: "order.ask-approval", guide: "purchases.orders#approve-an-order" },
  { kind: "action", id: "order.bill", mode: "basic", help: "order.bill", guide: "purchases.orders#bill-an-order" },
  { kind: "action", id: "order.close", mode: "basic", help: "order.close", guide: "purchases.orders#close-an-order" },
  { kind: "action", id: "recurring-bill.save", mode: "basic", help: "recurring-bill.save", guide: "purchases.bills#recurring-bills" },
  { kind: "action", id: "recurring-bill.check", mode: "basic", help: "recurring-bill.check", guide: "purchases.bills#recurring-bills" },
  { kind: "action", id: "recurring-bill.pause", mode: "basic", help: "recurring-bill.pause", guide: "purchases.bills#recurring-bills" },
  { kind: "action", id: "recurring-bill.resume", mode: "basic", help: "recurring-bill.resume", guide: "purchases.bills#recurring-bills" },
  { kind: "action", id: "bill.spread", mode: "expert", help: "bill.spread", guide: "purchases.bills#spread-a-cost-over-months" },
  { kind: "action", id: "bill.releases", mode: "basic", help: "bill.releases", guide: "purchases.bills#spread-a-cost-over-months" },
  { kind: "action", id: "bill-payment.void", mode: "basic", help: "bill-payment.void", guide: "purchases.bills#void-a-payment" },
  { kind: "action", id: "bill-payment.ask-void", mode: "basic", help: "bill-payment.ask-void", guide: "purchases.bills#void-a-payment" },
  { kind: "action", id: "journal.save", mode: "expert", help: "journal.save", guide: "accounting.journals#prepare-a-journal" },
  { kind: "action", id: "journal.check", mode: "expert", help: "journal.check", guide: "accounting.journals#check-and-post-a-journal" },
  { kind: "action", id: "journal.post", mode: "expert", help: "journal.post", guide: "accounting.journals#check-and-post-a-journal" },
  { kind: "action", id: "journal.reverse", mode: "expert", help: "journal.reverse", guide: "accounting.journals#reverse-a-posted-journal" },
  { kind: "action", id: "journal.ask-approval", mode: "expert", help: "journal.ask-approval", guide: "accounting.journals#journals-above-the-self-approval-threshold" },
  { kind: "action", id: "journal.ask-reverse", mode: "expert", help: "journal.ask-reverse", guide: "accounting.journals#reverse-a-posted-journal" },
  { kind: "action", id: "journal.reverse-on", mode: "expert", help: "journal.reverse-on", guide: "accounting.journals#journals-that-reverse-by-themselves" },
  { kind: "action", id: "journal.edit", mode: "expert", help: "journal.edit", guide: "accounting.journals#edit-a-posted-journal" },
  { kind: "action", id: "journal.attach-evidence", mode: "expert", help: "journal.attach-evidence", guide: "accounting.journals#attach-evidence" },
  { kind: "action", id: "journal.post-edit", mode: "expert", help: "journal.post-edit", guide: "accounting.journals#edit-a-posted-journal" },
  { kind: "action", id: "journal.ask-edit", mode: "expert", help: "journal.ask-edit", guide: "accounting.journals#edit-a-posted-journal" },
  { kind: "action", id: "recurring.save", mode: "expert", help: "recurring.save", guide: "accounting.journals#recurring-journals" },
  { kind: "action", id: "recurring.check", mode: "expert", help: "recurring.check", guide: "accounting.journals#recurring-journals" },
  { kind: "action", id: "recurring.reverses", mode: "expert", help: "recurring.reverses", guide: "accounting.journals#recurring-journals" },
  { kind: "action", id: "recurring.pause", mode: "expert", help: "recurring.pause", guide: "accounting.journals#recurring-journals" },
  { kind: "action", id: "recurring.resume", mode: "expert", help: "recurring.resume", guide: "accounting.journals#recurring-journals" },
  { kind: "action", id: "journal.waiting", mode: "basic", help: "journal.waiting", guide: "accounting.journals#automatic-reversals-that-wait" },
  { kind: "action", id: "period.open-year", mode: "expert", help: "period.open-year", guide: "accounting.periods#open-a-financial-year" },
  { kind: "action", id: "opening.save", mode: "expert", help: "opening.save", guide: "accounting.opening-balances#enter-the-opening-balances" },
  { kind: "action", id: "opening.check", mode: "expert", help: "opening.check", guide: "accounting.opening-balances#check-and-post-them" },
  { kind: "action", id: "opening.post", mode: "expert", help: "opening.post", guide: "accounting.opening-balances#check-and-post-them" },
  { kind: "action", id: "opening.ask-change", mode: "expert", help: "opening.ask-change", guide: "accounting.opening-balances#change-posted-opening-balances" },
  { kind: "action", id: "close.run-checks", mode: "expert", help: "close.run-checks", guide: "accounting.periods#close-a-month" },
  { kind: "action", id: "close.attest", mode: "expert", help: "close.attest", guide: "accounting.periods#close-a-month" },
  { kind: "action", id: "close.reopen", mode: "expert", help: "close.reopen", guide: "accounting.periods#reopen-a-month" },
  { kind: "action", id: "close.explain-variance", mode: "expert", help: "close.explain-variance", guide: "accounting.periods#explain-the-variances" },
  { kind: "action", id: "close.mark-task", mode: "expert", help: "close.mark-task", guide: "accounting.periods#work-through-the-checklist" },
  { kind: "action", id: "close.skip-task", mode: "expert", help: "close.skip-task", guide: "accounting.periods#work-through-the-checklist" },
  { kind: "action", id: "close.edit-template", mode: "expert", help: "close.edit-template", guide: "accounting.periods#change-the-checklist-template" },
  { kind: "action", id: "close.year-end", mode: "expert", help: "close.year-end", guide: "accounting.periods#year-end" },
  { kind: "action", id: "fx.find-rate", mode: "expert", help: "fx.find-rate", guide: "accounting.currencies#how-a-posting-finds-its-rate" },
  { kind: "action", id: "fx.add-rate", mode: "expert", help: "fx.add-rate", guide: "accounting.currencies#add-a-rate" },
  { kind: "action", id: "fx.import-rates", mode: "expert", help: "fx.import-rates", guide: "accounting.currencies#import-rates-from-a-file" },
  { kind: "action", id: "fx.withdraw-rate", mode: "expert", help: "fx.withdraw-rate", guide: "accounting.currencies#withdraw-a-rate" },
  { kind: "action", id: "fx.revalue", mode: "expert", help: "fx.revalue", guide: "accounting.currencies#revalue-a-month" },
  { kind: "action", id: "ledger.export", mode: "expert", help: "ledger.export", guide: "reports.general-ledger#export-it" },
  { kind: "action", id: "mode", mode: "basic", help: "shell.mode", guide: "preferences.security#basic-and-expert-mode" },
  { kind: "action", id: "step-up", mode: "basic", help: "session.step-up", guide: "auth.sign-in#confirm-its-you-with-a-passkey" },
  { kind: "action", id: "sign-in-again", mode: "basic", help: "session.sign-in-again", guide: "auth.sign-in#confirm-its-you-with-a-passkey" },
  { kind: "action", id: "passkey.add", mode: "basic", help: "passkey.add", guide: "preferences.security#add-a-passkey" },
  { kind: "action", id: "passkey.revoke", mode: "basic", help: "passkey.revoke", guide: "preferences.security#revoke-a-passkey" },
  { kind: "action", id: "device.sign-out", mode: "basic", help: "device.sign-out", guide: "preferences.security#your-devices" },
  { kind: "action", id: "device.sign-out-others", mode: "basic", help: "device.sign-out-others", guide: "preferences.security#your-devices" },
] as const satisfies readonly ActionFeature[];

// The sidebar's items in the designed order (the NAV list in docs/design/screens/build.py). Each
// module adds its item with its first screen; the sidebar holds at most 13 (§7.1).
export const nav = [
  { kind: "nav", id: "nav.home", route: "/", icon: House, group: "main", roles: "all", bottom: true, mode: "basic", help: "home.overview", guide: "home.overview" },
  { kind: "nav", id: "nav.inbox", route: "/inbox", icon: Inbox, group: "main", roles: "all", bottom: true, mode: "basic", help: "inbox.overview", guide: "approvals.decide#approve-or-reject" },
  { kind: "nav", id: "nav.contacts", route: "/contacts", icon: Contact, group: "main", roles: ["approver", "bookkeeper", "ar_specialist", "ap_specialist", "payment_releaser", "accountant", "controller", "owner", "auditor", "tax_agent", "dpo"], mode: "basic", help: "contacts.overview", guide: "contacts.manage" },
  // Accounting is Expert (experience/modes "Basic and Expert navigation"); its first page is the chart.
  { kind: "nav", id: "nav.purchases", route: "/purchases/bills", icon: ReceiptText, group: "main", roles: ["approver", "bookkeeper", "ap_specialist", "accountant", "controller", "owner", "auditor", "tax_agent"], mode: "basic", help: "nav.purchases", guide: "purchases.bills" },
  { kind: "nav", id: "nav.accounting", route: "/accounting", icon: BookOpen, group: "main", roles: ["approver", "bookkeeper", "ar_specialist", "ap_specialist", "payment_releaser", "payroll_administrator", "accountant", "controller", "owner", "auditor", "tax_agent"], mode: "expert", help: "nav.accounting", guide: "accounting.journals" },
  { kind: "nav", id: "nav.settings", route: "/settings", icon: Settings, group: "footer", roles: "all", mode: "basic", help: "settings.overview", guide: "home.overview#settings" },
] as const satisfies readonly NavFeature[];

// The Settings page's own navigation (docs/design/screens OrganisationSettings, UsersRoles): your
// settings, then the organisation's. Cut like the sidebar, by role and mode; the API still decides.
export const settingsNav = [
  { kind: "nav", id: "settings.profile", route: "/settings/profile", icon: UserRound, group: "you", roles: "all", mode: "basic", help: "settings.profile", guide: "preferences.security" },
  // The organisation's pages show to the roles that may view them (Appendix D, V or more); the API decides the rest.
  { kind: "nav", id: "settings.organisation", route: "/settings/organisation", icon: Building, group: "organisation", roles: "all", mode: "expert", help: "settings.organisation", guide: "settings.organisation" },
  { kind: "nav", id: "settings.accounting", route: "/settings/accounting", icon: BookCheck, group: "organisation", roles: ["owner", "administrator", "controller", "accountant", "bookkeeper", "ar_specialist", "ap_specialist", "payroll_administrator", "payment_releaser", "approver", "auditor", "tax_agent", "board_viewer", "company_secretary", "dpo"], mode: "expert", help: "settings.accounting", guide: "settings.accounting" },
  { kind: "nav", id: "settings.members", route: "/settings/members", icon: Users, group: "organisation", roles: ["owner", "administrator", "controller", "auditor", "dpo"], mode: "expert", help: "settings.members", guide: "settings.members" },
  { kind: "nav", id: "settings.team-mode", route: "/settings/team-mode", icon: ShieldCheck, group: "organisation", roles: ["owner", "administrator", "controller", "auditor", "dpo"], mode: "expert", help: "settings.team-mode", guide: "settings.team-mode" },
  { kind: "nav", id: "settings.approval-policies", route: "/settings/approval-policies", icon: Stamp, group: "organisation", roles: ["owner", "administrator", "controller", "accountant", "auditor", "dpo"], mode: "expert", help: "settings.approval-policies", guide: "approvals.policies" },
  { kind: "nav", id: "settings.statutory-packs", route: "/settings/statutory-packs", icon: Scale, group: "organisation", roles: ["owner", "controller", "accountant", "tax_agent", "payroll_administrator", "auditor"], mode: "expert", help: "settings.statutory-packs", guide: "settings.statutory-packs" },
] as const satisfies readonly NavFeature[];

export type RouteId = (typeof routes)[number]["id"];
export type ActionId = (typeof actions)[number]["id"];

export const registry: readonly Entry[] = [...routes, ...actions, ...nav, ...settingsNav];

export function entry(id: RouteId | ActionId): Entry {
  const e = registry.find((x) => x.id === id);
  if (!e) throw new Error(`no registry entry ${id}`);
  return e;
}
