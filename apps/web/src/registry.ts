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
import { Bell, BookCheck, BookOpen, Building, Contact, House, Inbox, Landmark, LayoutTemplate, Megaphone, Plug, Receipt, ReceiptText, Scale, Settings, ShieldCheck, Stamp, UserRound, Users, Wallet, type LucideIcon } from "lucide-react";
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
  // The customer's view of a quote or invoice through the secure link its email carried (FR-AR-06, FR-AR-16): no sign-in.
  { kind: "route", id: "document-view", path: "/d", mode: "basic", signedIn: false, help: "public.document", guide: "sales.invoices#what-the-customer-sees" },
  // My claims (FR-AP-14, FR-AP-18, FR-UX-39; p1-expense-claims task 8.1): a member's own claims, card lines and cash
  // advances; /me is where a person whose rights are personal only lands.
  { kind: "route", id: "personal", path: "/me", mode: "basic", signedIn: true, help: "claims.personal", guide: "claims.my-claims" },
  { kind: "route", id: "my-claims", path: "/me/claims", mode: "basic", signedIn: true, help: "claims.my-claims", guide: "claims.my-claims" },
  { kind: "route", id: "claim-new", path: "/me/claims/new", mode: "basic", signedIn: true, help: "claim.new", guide: "claims.my-claims#claim-a-receipt" },
  { kind: "route", id: "claim", path: "/me/claims/$claimId", mode: "basic", signedIn: true, help: "claims.claim", guide: "claims.my-claims#submit-a-claim" },
  { kind: "route", id: "mileage-claim", path: "/me/claims/new/mileage", mode: "basic", signedIn: true, help: "claim.new-mileage", guide: "claims.my-claims#claim-a-trip-in-your-own-vehicle" },
  { kind: "route", id: "claim-trips", path: "/me/claims/$claimId/mileage", mode: "basic", signedIn: true, help: "claims.trips", guide: "claims.my-claims#claim-a-trip-in-your-own-vehicle" },
  { kind: "route", id: "inbox", path: "/inbox", mode: "basic", signedIn: true, help: "inbox.overview", guide: "approvals.decide#approve-or-reject" },
  { kind: "route", id: "approval", path: "/approvals/$approvalId", mode: "basic", signedIn: true, help: "approvals.request", guide: "approvals.decide#approve-or-reject" },
  { kind: "route", id: "review-log", path: "/review-log", mode: "basic", signedIn: true, help: "approvals.review-log", guide: "approvals.decide#review-high-risk-actions" },
  { kind: "route", id: "contacts", path: "/contacts", mode: "basic", signedIn: true, help: "contacts.overview", guide: "contacts.manage" },
  { kind: "route", id: "items", path: "/items", mode: "basic", signedIn: true, help: "items.overview", guide: "contacts.manage#items-and-prices" },
  { kind: "route", id: "invoices", path: "/sales/invoices", mode: "basic", signedIn: true, help: "sales.invoices", guide: "sales.invoices" },
  { kind: "route", id: "invoice-new", path: "/sales/invoices/new", mode: "basic", signedIn: true, help: "invoice.new", guide: "sales.invoices#enter-an-invoice" },
  { kind: "route", id: "credit-note-new", path: "/sales/credit-notes/new", mode: "basic", signedIn: true, help: "credit-note.new", guide: "sales.invoices#credit-notes" },
  { kind: "route", id: "invoice", path: "/sales/invoices/$invoiceId", mode: "basic", signedIn: true, help: "sales.invoice", guide: "sales.invoices#check-and-issue-an-invoice" },
  { kind: "route", id: "quotes", path: "/sales/quotes", mode: "basic", signedIn: true, help: "sales.quotes", guide: "sales.quotes" },
  { kind: "route", id: "quote-new", path: "/sales/quotes/new", mode: "basic", signedIn: true, help: "quote.new", guide: "sales.quotes#prepare-a-quote" },
  { kind: "route", id: "sales-order-new", path: "/sales/orders/new", mode: "basic", signedIn: true, help: "sales-order.new", guide: "sales.quotes#sales-orders" },
  { kind: "route", id: "quote", path: "/sales/quotes/$quoteId", mode: "basic", signedIn: true, help: "sales.quote", guide: "sales.quotes#send-a-quote" },
  { kind: "route", id: "recurring-invoices", path: "/sales/recurring", mode: "basic", signedIn: true, help: "sales.recurring-invoices", guide: "sales.invoices#recurring-invoices" },
  { kind: "route", id: "recurring-invoice-new", path: "/sales/recurring/new", mode: "basic", signedIn: true, help: "recurring-invoice.new", guide: "sales.invoices#recurring-invoices" },
  { kind: "route", id: "recurring-invoice", path: "/sales/recurring/$recurringInvoiceId", mode: "basic", signedIn: true, help: "sales.recurring-invoice", guide: "sales.invoices#recurring-invoices" },
  { kind: "route", id: "statements", path: "/sales/statements", mode: "basic", signedIn: true, help: "sales.statements-reminders", guide: "sales.statements" },
  { kind: "route", id: "bills", path: "/purchases/bills", mode: "basic", signedIn: true, help: "purchases.bills", guide: "purchases.bills" },
  { kind: "route", id: "bill-new", path: "/purchases/bills/new", mode: "basic", signedIn: true, help: "bill.new", guide: "purchases.bills#enter-a-bill" },
  { kind: "route", id: "credit-new", path: "/purchases/credits/new", mode: "basic", signedIn: true, help: "credit.new", guide: "purchases.bills#credit-notes" },
  { kind: "route", id: "bill", path: "/purchases/bills/$billId", mode: "basic", signedIn: true, help: "purchases.bill", guide: "purchases.bills#check-and-post-a-bill" },
  // The approver's queue and a claim opened from it (FR-AP-17, FR-APR-01; p1-expense-claims task 8.2): both modes.
  { kind: "route", id: "claims-review", path: "/purchases/claims", mode: "basic", signedIn: true, help: "claims.review", guide: "claims.approve-team#approve-or-return-a-claim" },
  { kind: "route", id: "claim-review", path: "/purchases/claims/$claimId", mode: "basic", signedIn: true, help: "claims.review-claim", guide: "claims.approve-team#approve-or-return-a-claim" },
  { kind: "route", id: "purchase-orders", path: "/purchases/orders", mode: "basic", signedIn: true, help: "purchases.orders", guide: "purchases.orders" },
  { kind: "route", id: "purchase-order-new", path: "/purchases/orders/new", mode: "basic", signedIn: true, help: "order.new", guide: "purchases.orders#raise-an-order" },
  { kind: "route", id: "purchase-order", path: "/purchases/orders/$orderId", mode: "basic", signedIn: true, help: "purchases.order", guide: "purchases.orders#approve-an-order" },
  { kind: "route", id: "recurring-bills", path: "/purchases/bills/recurring", mode: "basic", signedIn: true, help: "purchases.recurring-bills", guide: "purchases.bills#recurring-bills" },
  { kind: "route", id: "recurring-bill-new", path: "/purchases/bills/recurring/new", mode: "basic", signedIn: true, help: "recurring-bill.new", guide: "purchases.bills#recurring-bills" },
  { kind: "route", id: "recurring-bill", path: "/purchases/bills/recurring/$recurringBillId", mode: "basic", signedIn: true, help: "purchases.recurring-bill", guide: "purchases.bills#recurring-bills" },
  // Banking (FR-BNK-01, FR-BNK-13, FR-BNK-15…18; p1-bank-import-reconcile task 10.1): Basic; safety (gaps, freshness) shows in both modes.
  { kind: "route", id: "banking", path: "/banking", mode: "basic", signedIn: true, help: "banking.accounts", guide: "banking.accounts" },
  { kind: "route", id: "bank-account-new", path: "/banking/accounts/new", mode: "basic", signedIn: true, help: "bank-account.new", guide: "banking.accounts#add-a-money-account" },
  { kind: "route", id: "bank-account", path: "/banking/accounts/$accountId", mode: "basic", signedIn: true, help: "banking.account", guide: "banking.accounts#an-accounts-statements-and-lines" },
  { kind: "route", id: "statement-import", path: "/banking/import", mode: "basic", signedIn: true, help: "banking.import", guide: "banking.accounts#import-statements" },
  { kind: "route", id: "categorise", path: "/banking/categorise", mode: "basic", signedIn: true, help: "banking.categorise", guide: "banking.accounts#categorise-the-waiting-lines" },
  { kind: "route", id: "bank-rules", path: "/banking/rules", mode: "expert", signedIn: true, help: "banking.rules", guide: "banking.accounts#bank-rules" },
  { kind: "route", id: "auto-reconciled", path: "/banking/auto-reconciled", mode: "basic", signedIn: true, help: "banking.auto-reconciled", guide: "banking.accounts#what-fyniq-reconciled-by-itself" },
  { kind: "route", id: "bank-reconciliation", path: "/banking/reconciliations/$accountId/$periodId", mode: "expert", signedIn: true, help: "banking.reconciliation", guide: "banking.accounts#reconcile-an-account-for-a-month" },
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
  { kind: "route", id: "fixed-assets", path: "/accounting/assets", mode: "expert", signedIn: true, help: "accounting.fixed-assets", guide: "accounting.fixed-assets" },
  { kind: "route", id: "settings", path: "/settings", mode: "basic", signedIn: true, help: "settings.overview", guide: "home.overview#settings" },
  { kind: "route", id: "settings-profile", path: "/settings/profile", mode: "basic", signedIn: true, help: "settings.profile", guide: "preferences.security" },
  { kind: "route", id: "settings-organisation", path: "/settings/organisation", mode: "expert", signedIn: true, help: "settings.organisation", guide: "settings.organisation" },
  { kind: "route", id: "settings-accounting", path: "/settings/accounting", mode: "expert", signedIn: true, help: "settings.accounting", guide: "settings.accounting" },
  { kind: "route", id: "settings-statutory-packs", path: "/settings/statutory-packs", mode: "expert", signedIn: true, help: "settings.statutory-packs", guide: "settings.statutory-packs" },
  { kind: "route", id: "settings-members", path: "/settings/members", mode: "expert", signedIn: true, help: "settings.members", guide: "settings.members" },
  { kind: "route", id: "settings-team-mode", path: "/settings/team-mode", mode: "expert", signedIn: true, help: "settings.team-mode", guide: "settings.team-mode" },
  { kind: "route", id: "settings-approval-policies", path: "/settings/approval-policies", mode: "expert", signedIn: true, help: "settings.approval-policies", guide: "approvals.policies" },
  { kind: "route", id: "settings-expense-policy", path: "/settings/expense-policy", mode: "expert", signedIn: true, help: "settings.expense-policy", guide: "claims.approve-team#set-the-expense-policy" },
  { kind: "route", id: "settings-templates", path: "/settings/templates", mode: "basic", signedIn: true, help: "settings.templates", guide: "settings.templates" },
  { kind: "route", id: "settings-notifications", path: "/settings/notifications", mode: "basic", signedIn: true, help: "settings.notifications", guide: "settings.notifications" },
  { kind: "route", id: "settings-announcements", path: "/settings/announcements", mode: "expert", signedIn: true, help: "settings.announcements", guide: "settings.announcements" },
  { kind: "route", id: "settings-integrations", path: "/settings/integrations", mode: "expert", signedIn: true, help: "settings.integrations", guide: "settings.integrations" },
  { kind: "route", id: "settings-integration", path: "/settings/integrations/$connector", mode: "expert", signedIn: true, help: "settings.integration", guide: "settings.integrations#a-connectors-page" },
  { kind: "route", id: "settings-template", path: "/settings/templates/$templateId", mode: "basic", signedIn: true, help: "settings.template", guide: "settings.templates#edit-a-template" },
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
  { kind: "action", id: "notification.centre", mode: "basic", help: "notification.centre", guide: "settings.notifications#your-notifications" },
  { kind: "action", id: "notification.read", mode: "basic", help: "notification.read", guide: "settings.notifications#your-notifications" },
  { kind: "action", id: "notification.unread", mode: "basic", help: "notification.unread", guide: "settings.notifications#your-notifications" },
  { kind: "action", id: "notification.archive", mode: "basic", help: "notification.archive", guide: "settings.notifications#your-notifications" },
  { kind: "action", id: "notification.read-all", mode: "basic", help: "notification.read-all", guide: "settings.notifications#your-notifications" },
  { kind: "action", id: "notification.preference", mode: "basic", help: "notification.preference", guide: "settings.notifications#choose-how-they-reach-you" },
  { kind: "action", id: "notification.quiet-hours", mode: "basic", help: "notification.quiet-hours", guide: "settings.notifications#quiet-hours" },
  { kind: "action", id: "notification.test", mode: "basic", help: "notification.test", guide: "settings.notifications#send-yourself-a-test" },
  { kind: "action", id: "banner.dismiss", mode: "basic", help: "banner.dismiss", guide: "settings.notifications#banners" },
  { kind: "action", id: "announcement.publish", mode: "expert", help: "announcement.publish", guide: "settings.announcements#publish-an-announcement" },
  { kind: "action", id: "announcement.withdraw", mode: "expert", help: "announcement.withdraw", guide: "settings.announcements#end-an-announcement" },
  { kind: "action", id: "connector.install", mode: "expert", help: "connector.install", guide: "settings.integrations#install-a-connector" },
  { kind: "action", id: "connector.health", mode: "expert", help: "connector.health", guide: "settings.integrations#health" },
  { kind: "action", id: "connector.data-classes", mode: "expert", help: "connector.data-classes", guide: "settings.integrations#what-a-connector-sends-out" },
  { kind: "action", id: "connector.configure", mode: "expert", help: "connector.configure", guide: "settings.integrations#configure-it-and-set-its-secrets" },
  { kind: "action", id: "connector.secret", mode: "expert", help: "connector.secret", guide: "settings.integrations#configure-it-and-set-its-secrets" },
  { kind: "action", id: "connector.test", mode: "expert", help: "connector.test", guide: "settings.integrations#test-it" },
  { kind: "action", id: "connector.enable", mode: "expert", help: "connector.enable", guide: "settings.integrations#turn-it-on-or-off" },
  { kind: "action", id: "connector.pause", mode: "expert", help: "connector.pause", guide: "settings.integrations#turn-it-on-or-off" },
  { kind: "action", id: "connector.resume", mode: "expert", help: "connector.resume", guide: "settings.integrations#turn-it-on-or-off" },
  { kind: "action", id: "connector.disable", mode: "expert", help: "connector.disable", guide: "settings.integrations#turn-it-on-or-off" },
  { kind: "action", id: "connector.uninstall", mode: "expert", help: "connector.uninstall", guide: "settings.integrations#uninstall-it" },
  { kind: "action", id: "connector.consent", mode: "expert", help: "connector.consent", guide: "settings.integrations#upgrade-and-consent" },
  { kind: "action", id: "connector.order", mode: "expert", help: "connector.order", guide: "settings.integrations#email-senders-domains-and-suppressions" },
  { kind: "action", id: "connector.approve", mode: "expert", help: "connector.approve", guide: "settings.integrations#approve-or-reject-a-change" },
  { kind: "action", id: "connector.reject", mode: "expert", help: "connector.reject", guide: "settings.integrations#approve-or-reject-a-change" },
  { kind: "action", id: "email.sender", mode: "expert", help: "email.sender", guide: "settings.integrations#email-senders-domains-and-suppressions" },
  { kind: "action", id: "email.release", mode: "expert", help: "email.release", guide: "settings.integrations#email-senders-domains-and-suppressions" },
  { kind: "action", id: "pack.activate", mode: "expert", help: "pack.activate", guide: "settings.statutory-packs#activate-a-version" },
  { kind: "action", id: "template.new", mode: "basic", help: "template.new", guide: "settings.templates#start-a-template" },
  { kind: "action", id: "template.save", mode: "basic", help: "template.save", guide: "settings.templates#edit-a-template" },
  { kind: "action", id: "template.publish", mode: "basic", help: "template.publish", guide: "settings.templates#publish-a-version" },
  { kind: "action", id: "template.ask-approval", mode: "basic", help: "template.ask-approval", guide: "settings.templates#publish-a-version" },
  { kind: "action", id: "template.rollback", mode: "basic", help: "template.rollback", guide: "settings.templates#roll-back" },
  { kind: "action", id: "template.default", mode: "basic", help: "template.default", guide: "settings.templates#choose-the-default" },
  { kind: "action", id: "template.bundle", mode: "expert", help: "template.bundle", guide: "settings.templates#move-templates-with-a-bundle" },
  { kind: "action", id: "review-banner", mode: "basic", help: "review.awaiting-verification", guide: "review.awaiting-verification" },
  { kind: "action", id: "contact.add", mode: "basic", help: "contact.add", guide: "contacts.manage#add-a-contact" },
  { kind: "action", id: "contact.templates", mode: "basic", help: "contact.templates", guide: "contacts.manage#their-own-templates" },
  { kind: "action", id: "contact.bank-details", mode: "basic", help: "contact.bank-details", guide: "contacts.manage#bank-details" },
  { kind: "action", id: "bank-details.change", mode: "basic", help: "bank-details.change", guide: "contacts.manage#change-a-suppliers-bank-details" },
  { kind: "action", id: "bank-details.ask", mode: "basic", help: "bank-details.ask", guide: "contacts.manage#change-a-suppliers-bank-details" },
  { kind: "action", id: "bank-details.reveal", mode: "basic", help: "bank-details.reveal", guide: "contacts.manage#see-the-full-number" },
  { kind: "action", id: "bank-change.attach-evidence", mode: "basic", help: "bank-change.attach-evidence", guide: "contacts.manage#change-a-suppliers-bank-details" },
  { kind: "action", id: "item.add", mode: "basic", help: "item.add", guide: "contacts.manage#items-and-prices" },
  { kind: "action", id: "chart.add", mode: "expert", help: "chart.add", guide: "accounting.chart#add-an-account" },
  { kind: "action", id: "chart.load-standard", mode: "expert", help: "chart.load-standard", guide: "accounting.chart#start-from-the-standard-chart" },
  { kind: "action", id: "bill.save", mode: "basic", help: "bill.save", guide: "purchases.bills#enter-a-bill" },
  { kind: "action", id: "claims.owed", mode: "basic", help: "claims.owed", guide: "claims.my-claims#follow-your-claims" },
  { kind: "action", id: "expense-policy.add", mode: "expert", help: "expense-policy.add", guide: "claims.approve-team#set-the-expense-policy" },
  { kind: "action", id: "fixed-assets.register", mode: "expert", help: "fixed-assets.register", guide: "accounting.fixed-assets#register-an-asset" },
  { kind: "action", id: "fixed-assets.register-opening", mode: "expert", help: "fixed-assets.register-opening", guide: "accounting.fixed-assets#assets-bought-before-the-books-start" },
  { kind: "action", id: "fixed-assets.unregistered", mode: "expert", help: "fixed-assets.unregistered", guide: "accounting.fixed-assets#register-an-asset" },
  { kind: "action", id: "fixed-assets.change", mode: "expert", help: "fixed-assets.change", guide: "accounting.fixed-assets#change-an-asset" },
  { kind: "action", id: "fixed-assets.dispose", mode: "expert", help: "fixed-assets.dispose", guide: "accounting.fixed-assets#sell-or-write-off-an-asset" },
  { kind: "action", id: "fixed-assets.void", mode: "expert", help: "fixed-assets.void", guide: "accounting.fixed-assets#void-a-disposal" },
  { kind: "action", id: "fixed-assets.void-asset", mode: "expert", help: "fixed-assets.void-asset", guide: "accounting.fixed-assets#void-an-asset-registered-by-mistake" },
  { kind: "action", id: "fixed-assets.voided", mode: "expert", help: "fixed-assets.voided", guide: "accounting.fixed-assets#void-an-asset-registered-by-mistake" },
  { kind: "action", id: "expense-policy.load-starter", mode: "expert", help: "expense-policy.load-starter", guide: "claims.approve-team#set-the-expense-policy" },
  { kind: "action", id: "claims.export", mode: "basic", help: "claims.export", guide: "claims.my-claims#follow-your-claims" },
  { kind: "action", id: "claims.mileage-log", mode: "basic", help: "claims.mileage-log", guide: "claims.my-claims#claim-a-trip-in-your-own-vehicle" },
  { kind: "action", id: "claim.save", mode: "basic", help: "claim.save", guide: "claims.my-claims#claim-a-receipt" },
  { kind: "action", id: "claim.submit", mode: "basic", help: "claim.submit", guide: "claims.my-claims#submit-a-claim" },
  { kind: "action", id: "claim.withdraw", mode: "basic", help: "claim.withdraw", guide: "claims.my-claims#submit-a-claim" },
  { kind: "action", id: "claim.attach-evidence", mode: "basic", help: "claim.attach-evidence", guide: "claims.my-claims#attach-the-receipt" },
  { kind: "action", id: "card-line.complete", mode: "basic", help: "card-line.complete", guide: "claims.my-claims#complete-your-company-card-lines" },
  { kind: "action", id: "card-line.attach-evidence", mode: "basic", help: "card-line.attach-evidence", guide: "claims.my-claims#complete-your-company-card-lines" },
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
  // Input tax (FR-AP-20): the facts and each line's decision are safety information, so Basic shows them too (§7.8).
  { kind: "action", id: "bill.input-tax", mode: "basic", help: "bill.input-tax", guide: "purchases.bills#input-tax" },
  { kind: "action", id: "bill.business-share", mode: "basic", help: "bill.business-share", guide: "purchases.bills#input-tax" },
  { kind: "action", id: "bill.tax-agent", mode: "basic", help: "bill.tax-agent", guide: "purchases.bills#input-tax" },
  // Withholding tax (FR-AP-19): the nature, the decision and the hold are safety information, so Basic shows them too.
  // Invoices and credit notes (FR-AR-03, FR-AR-04, FR-AR-06): Basic, but for the discount and the spread of a line.
  { kind: "action", id: "invoice.save", mode: "basic", help: "invoice.save", guide: "sales.invoices#enter-an-invoice" },
  { kind: "action", id: "invoice.issue", mode: "basic", help: "invoice.issue", guide: "sales.invoices#check-and-issue-an-invoice" },
  { kind: "action", id: "invoice.ask-approval", mode: "basic", help: "invoice.ask-approval", guide: "sales.invoices#invoices-above-the-self-approval-threshold" },
  { kind: "action", id: "invoice.email", mode: "basic", help: "invoice.email", guide: "sales.invoices#email-an-invoice" },
  { kind: "action", id: "invoice.void", mode: "basic", help: "invoice.void", guide: "sales.invoices#void-or-edit-an-issued-invoice" },
  { kind: "action", id: "invoice.ask-void", mode: "basic", help: "invoice.ask-void", guide: "sales.invoices#void-or-edit-an-issued-invoice" },
  { kind: "action", id: "invoice.edit", mode: "basic", help: "invoice.edit", guide: "sales.invoices#void-or-edit-an-issued-invoice" },
  { kind: "action", id: "invoice.post-edit", mode: "basic", help: "invoice.post-edit", guide: "sales.invoices#void-or-edit-an-issued-invoice" },
  { kind: "action", id: "invoice.ask-edit", mode: "basic", help: "invoice.ask-edit", guide: "sales.invoices#void-or-edit-an-issued-invoice" },
  { kind: "action", id: "invoice.attach-evidence", mode: "basic", help: "invoice.attach-evidence", guide: "sales.invoices#attach-evidence" },
  { kind: "action", id: "invoice.spread", mode: "expert", help: "invoice.spread", guide: "sales.invoices#spread-revenue-over-months" },
  { kind: "action", id: "invoice.discount", mode: "expert", help: "invoice.discount", guide: "sales.invoices#enter-an-invoice" },
  { kind: "action", id: "public.download", mode: "basic", help: "public.download", guide: "sales.invoices#what-the-customer-sees" },
  { kind: "action", id: "credit-note.credits", mode: "basic", help: "credit-note.credits", guide: "sales.invoices#credit-notes" },
  { kind: "action", id: "invoice.record-payment", mode: "basic", help: "invoice.record-payment", guide: "sales.invoices#record-a-payment" },
  { kind: "action", id: "invoice.settle-in-full", mode: "basic", help: "invoice.settle-in-full", guide: "sales.invoices#record-a-payment" },
  // Recurring invoices (FR-AR-07): Basic; issue and send needs another person's approval of the content.
  { kind: "action", id: "recurring-invoice.save", mode: "basic", help: "recurring-invoice.save", guide: "sales.invoices#recurring-invoices" },
  { kind: "action", id: "recurring-invoice.check", mode: "basic", help: "recurring-invoice.check", guide: "sales.invoices#recurring-invoices" },
  { kind: "action", id: "recurring-invoice.pause", mode: "basic", help: "recurring-invoice.pause", guide: "sales.invoices#recurring-invoices" },
  { kind: "action", id: "recurring-invoice.resume", mode: "basic", help: "recurring-invoice.resume", guide: "sales.invoices#recurring-invoices" },
  { kind: "action", id: "recurring-invoice.mode", mode: "basic", help: "recurring-invoice.mode", guide: "sales.invoices#invoices-the-scheduler-issues-and-sends" },
  { kind: "action", id: "recurring-invoice.ask-approval", mode: "basic", help: "recurring-invoice.ask-approval", guide: "sales.invoices#invoices-the-scheduler-issues-and-sends" },
  // Reminders and statements (FR-AR-08): Basic; reminders wait for a person until bank import.
  { kind: "action", id: "sales.reminders", mode: "basic", help: "sales.reminders", guide: "sales.statements#reminders-due" },
  { kind: "action", id: "reminder.send", mode: "basic", help: "reminder.send", guide: "sales.statements#reminders-due" },
  { kind: "action", id: "reminder.skip", mode: "basic", help: "reminder.skip", guide: "sales.statements#reminders-due" },
  { kind: "action", id: "sales.statements", mode: "basic", help: "sales.statements", guide: "sales.statements#statements-of-account" },
  { kind: "action", id: "statement.show", mode: "basic", help: "statement.show", guide: "sales.statements#statements-of-account" },
  { kind: "action", id: "statement.make", mode: "basic", help: "statement.make", guide: "sales.statements#statements-of-account" },
  { kind: "action", id: "statement.make-only", mode: "basic", help: "statement.make-only", guide: "sales.statements#statements-of-account" },
  { kind: "action", id: "sales.reminder-schedules", mode: "basic", help: "sales.reminder-schedules", guide: "sales.statements#reminder-schedules" },
  { kind: "action", id: "contact.customer-group", mode: "basic", help: "contact.customer-group", guide: "sales.statements#reminder-schedules" },
  // Quotes and sales orders (FR-AR-01, FR-AR-02): Basic.
  { kind: "action", id: "quote.save", mode: "basic", help: "quote.save", guide: "sales.quotes#prepare-a-quote" },
  { kind: "action", id: "quote.send", mode: "basic", help: "quote.send", guide: "sales.quotes#send-a-quote" },
  { kind: "action", id: "quote.email", mode: "basic", help: "quote.email", guide: "sales.quotes#send-a-quote" },
  { kind: "action", id: "quote.accept", mode: "basic", help: "quote.accept", guide: "sales.quotes#the-customers-answer" },
  { kind: "action", id: "quote.decline", mode: "basic", help: "quote.decline", guide: "sales.quotes#the-customers-answer" },
  { kind: "action", id: "quote.cancel", mode: "basic", help: "quote.cancel", guide: "sales.quotes#cancel-a-quote" },
  { kind: "action", id: "quote.invoice", mode: "basic", help: "quote.invoice", guide: "sales.quotes#invoice-an-accepted-quote" },
  { kind: "action", id: "sales.pipeline", mode: "basic", help: "sales.pipeline", guide: "sales.quotes#the-pipeline" },
  // Tracking and paying bills (FR-AP-13): the board, the calendar, planning and the reminders are Basic.
  // The categorise queue (FR-BNK-06…09; p1-bank-import-reconcile task 10.3): one line at a time is Basic; the grid view,
  // splitting a line, transfers, matching to chosen documents and suspense are Expert (§7.8).
  { kind: "action", id: "banking.categorise-grid", mode: "expert", help: "banking.categorise-grid", guide: "banking.accounts#categorise-the-waiting-lines" },
  { kind: "action", id: "bank-line.split", mode: "expert", help: "bank-line.split", guide: "banking.accounts#categorise-the-waiting-lines" },
  { kind: "action", id: "bank-line.transfer", mode: "expert", help: "bank-line.transfer", guide: "banking.accounts#categorise-the-waiting-lines" },
  { kind: "action", id: "bank-line.match", mode: "expert", help: "bank-line.match", guide: "banking.accounts#categorise-the-waiting-lines" },
  { kind: "action", id: "bank-line.suspense", mode: "expert", help: "bank-line.suspense", guide: "banking.accounts#categorise-the-waiting-lines" },
  { kind: "action", id: "bank-line.accept", mode: "basic", help: "bank-line.accept", guide: "banking.accounts#categorise-the-waiting-lines" },
  { kind: "action", id: "bank-line.change", mode: "basic", help: "bank-line.change", guide: "banking.accounts#categorise-the-waiting-lines" },
  { kind: "action", id: "banking.reconcile-now", mode: "basic", help: "banking.reconcile-now", guide: "banking.accounts#categorise-the-waiting-lines" },
  { kind: "action", id: "bank-rule.auto-apply", mode: "expert", help: "bank-rule.auto-apply", guide: "banking.accounts#bank-rules" },
  { kind: "action", id: "bank-reconciliation.sign", mode: "expert", help: "bank-reconciliation.sign", guide: "banking.accounts#reconcile-an-account-for-a-month" },
  { kind: "action", id: "bill.calendar", mode: "basic", help: "bill.calendar", guide: "purchases.bills#plan-and-track-payments" },
  { kind: "action", id: "bill.plan-payment", mode: "basic", help: "bill.plan-payment", guide: "purchases.bills#plan-and-track-payments" },
  { kind: "action", id: "inbox.bills-to-pay", mode: "basic", help: "inbox.bills-to-pay", guide: "purchases.bills#plan-and-track-payments" },
  { kind: "action", id: "bill.nature", mode: "basic", help: "bill.nature", guide: "purchases.bills#non-resident-suppliers" },
  { kind: "action", id: "bill.withholding", mode: "basic", help: "bill.withholding", guide: "purchases.bills#non-resident-suppliers" },
  { kind: "action", id: "bill.record-decision", mode: "basic", help: "bill.record-decision", guide: "purchases.bills#non-resident-suppliers" },
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
  { kind: "nav", id: "nav.my-claims", route: "/me/claims", icon: Wallet, group: "personal", roles: ["employee"], mode: "basic", help: "claims.my-claims", guide: "claims.my-claims" },
  { kind: "nav", id: "nav.inbox", route: "/inbox", icon: Inbox, group: "main", roles: "all", bottom: true, mode: "basic", help: "inbox.overview", guide: "approvals.decide#approve-or-reject" },
  { kind: "nav", id: "nav.contacts", route: "/contacts", icon: Contact, group: "main", roles: ["approver", "bookkeeper", "ar_specialist", "ap_specialist", "payment_releaser", "accountant", "controller", "owner", "auditor", "tax_agent", "dpo"], mode: "basic", help: "contacts.overview", guide: "contacts.manage" },
  { kind: "nav", id: "nav.sales", route: "/sales/invoices", icon: Receipt, group: "main", roles: ["approver", "bookkeeper", "ar_specialist", "accountant", "controller", "owner", "auditor", "tax_agent"], mode: "basic", help: "nav.sales", guide: "sales.invoices" },
  // Accounting is Expert (experience/modes "Basic and Expert navigation"); its first page is the chart.
  { kind: "nav", id: "nav.purchases", route: "/purchases/bills", icon: ReceiptText, group: "main", roles: ["approver", "bookkeeper", "ap_specialist", "accountant", "controller", "owner", "auditor", "tax_agent"], mode: "basic", help: "nav.purchases", guide: "purchases.bills" },
  // Banking shows to the roles that may view bank transactions (Appendix D, V or more); the API decides the rest.
  { kind: "nav", id: "nav.claims-review", route: "/purchases/claims", icon: ReceiptText, group: "main", roles: ["approver", "controller", "accountant", "ap_specialist", "bookkeeper", "owner", "auditor", "tax_agent"], mode: "basic", help: "claims.review", guide: "claims.approve-team#approve-or-return-a-claim" },
  { kind: "nav", id: "nav.banking", route: "/banking", icon: Landmark, group: "main", roles: ["bookkeeper", "accountant", "controller", "owner", "ar_specialist", "ap_specialist", "payment_releaser", "auditor", "tax_agent"], mode: "basic", help: "banking.accounts", guide: "banking.accounts" },
  { kind: "nav", id: "nav.accounting", route: "/accounting", icon: BookOpen, group: "main", roles: ["approver", "bookkeeper", "ar_specialist", "ap_specialist", "payment_releaser", "payroll_administrator", "accountant", "controller", "owner", "auditor", "tax_agent"], mode: "expert", help: "nav.accounting", guide: "accounting.journals" },
  { kind: "nav", id: "nav.settings", route: "/settings", icon: Settings, group: "footer", roles: "all", mode: "basic", help: "settings.overview", guide: "home.overview#settings" },
] as const satisfies readonly NavFeature[];

// The Settings page's own navigation (docs/design/screens OrganisationSettings, UsersRoles): your
// settings, then the organisation's. Cut like the sidebar, by role and mode; the API still decides.
export const settingsNav = [
  { kind: "nav", id: "settings.profile", route: "/settings/profile", icon: UserRound, group: "you", roles: "all", mode: "basic", help: "settings.profile", guide: "preferences.security" },
  { kind: "nav", id: "settings.notifications", route: "/settings/notifications", icon: Bell, group: "you", roles: "all", mode: "basic", help: "settings.notifications", guide: "settings.notifications" },
  // The organisation's pages show to the roles that may view them (Appendix D, V or more); the API decides the rest.
  { kind: "nav", id: "settings.organisation", route: "/settings/organisation", icon: Building, group: "organisation", roles: "all", mode: "expert", help: "settings.organisation", guide: "settings.organisation" },
  { kind: "nav", id: "settings.accounting", route: "/settings/accounting", icon: BookCheck, group: "organisation", roles: ["owner", "administrator", "controller", "accountant", "bookkeeper", "ar_specialist", "ap_specialist", "payroll_administrator", "payment_releaser", "approver", "auditor", "tax_agent", "board_viewer", "company_secretary", "dpo"], mode: "expert", help: "settings.accounting", guide: "settings.accounting" },
  { kind: "nav", id: "settings.members", route: "/settings/members", icon: Users, group: "organisation", roles: ["owner", "administrator", "controller", "auditor", "dpo"], mode: "expert", help: "settings.members", guide: "settings.members" },
  { kind: "nav", id: "settings.team-mode", route: "/settings/team-mode", icon: ShieldCheck, group: "organisation", roles: ["owner", "administrator", "controller", "auditor", "dpo"], mode: "expert", help: "settings.team-mode", guide: "settings.team-mode" },
  { kind: "nav", id: "settings.approval-policies", route: "/settings/approval-policies", icon: Stamp, group: "organisation", roles: ["owner", "administrator", "controller", "accountant", "auditor", "dpo"], mode: "expert", help: "settings.approval-policies", guide: "approvals.policies" },
  { kind: "nav", id: "settings.statutory-packs", route: "/settings/statutory-packs", icon: Scale, group: "organisation", roles: ["owner", "controller", "accountant", "tax_agent", "payroll_administrator", "auditor"], mode: "expert", help: "settings.statutory-packs", guide: "settings.statutory-packs" },
  { kind: "nav", id: "settings.expense-policy", route: "/settings/expense-policy", icon: Wallet, group: "organisation", roles: ["owner", "administrator", "controller", "approver", "accountant", "auditor"], mode: "expert", help: "settings.expense-policy", guide: "claims.approve-team#set-the-expense-policy" },
  { kind: "nav", id: "settings.templates", route: "/settings/templates", icon: LayoutTemplate, group: "organisation", roles: ["owner", "controller", "accountant", "ar_specialist", "ap_specialist", "auditor"], mode: "basic", help: "settings.templates", guide: "settings.templates" },
  { kind: "nav", id: "settings.integrations", route: "/settings/integrations", icon: Plug, group: "organisation", roles: ["owner", "administrator", "controller", "auditor", "dpo"], mode: "expert", help: "settings.integrations", guide: "settings.integrations" },
  { kind: "nav", id: "settings.announcements", route: "/settings/announcements", icon: Megaphone, group: "organisation", roles: ["owner", "administrator", "auditor"], mode: "expert", help: "settings.announcements", guide: "settings.announcements" },
] as const satisfies readonly NavFeature[];

export type RouteId = (typeof routes)[number]["id"];
export type ActionId = (typeof actions)[number]["id"];

export const registry: readonly Entry[] = [...routes, ...actions, ...nav, ...settingsNav];

export function entry(id: RouteId | ActionId): Entry {
  const e = registry.find((x) => x.id === id);
  if (!e) throw new Error(`no registry entry ${id}`);
  return e;
}
