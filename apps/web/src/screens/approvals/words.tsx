import { Ban, CircleCheck, CircleX, ClockAlert, Hourglass, type LucideIcon } from "lucide-react";
import type { components } from "../../api/schema";
import { Icon } from "../../ui";

// The words the approval screens use for what the API returns (ENG-23: the UI's own names, plain).

export type Approval = components["schemas"]["Approval"];
export type ReviewEntry = components["schemas"]["ReviewLogItem"];

const operations: Record<Approval["operation"], string> = {
  "organisation_setting.change": "Change an organisation setting",
  "role.assign": "Give a role",
  "role.revoke": "End a role",
  "member.remove": "Remove a member",
  "member_invitation.create": "Invite someone",
  "approval_policy.change": "Change an approval policy",
  "statutory_pack.activate": "Activate a statutory pack version",
  "team_mode.change": "Switch the team mode",
  "period.reopen": "Reopen a closed period",
  "journal.post": "Post a journal",
  "journal.reverse": "Reverse a journal",
  "journal.edit": "Edit a posted journal",
  "opening_balance.edit": "Change the posted opening balances",
  "bill.post": "Post a bill",
  "bill.void": "Void a bill",
  "bill.edit": "Edit a posted bill",
  "bill_payment.void": "Void a recorded payment",
  "credit_application.void": "Void a credit applied to a bill",
  "supplier_refund.void": "Void a refund from a supplier",
  "purchase_order.approve": "Approve a purchase order",
  "document_template.publish": "Publish a document template",
  "invoice.post": "Issue an invoice",
  "invoice.void": "Void an invoice",
  "invoice.edit": "Edit an issued invoice",
  "customer_receipt.void": "Void a customer receipt",
  "write_off.post": "Write off an invoice",
  "write_off.void": "Void a write-off",
  "customer_credit_application.void": "Void a credit applied to an invoice",
  "customer_refund.void": "Void a refund to a customer",
  "customer_deposit.void": "Void a customer deposit",
  "recurring_invoice.approve": "Approve a recurring invoice to issue and send",
  "connector.change": "Change a connector",
  "bank_rule.auto_apply": "Let a bank rule apply by itself",
  "expense_claim.post": "Approve an expense claim",
  "claim_reimbursement.void": "Void a reimbursement of expense claims",
  "cash_advance.void": "Void a cash advance or its refund",
  "asset_disposal.void": "Void the sale or write-off of a fixed asset",
  "fixed_asset.void": "Void a fixed asset registered by mistake",
  "payee_bank_change.approve": "Change a supplier's bank details",
  "claimant_bank_change.approve": "Change a person's bank account for reimbursements",
  "payment_batch.approve": "Approve a payment batch",
  "payment_batch.reapprove": "Approve a payment batch again after a payee's account changed",
  "contact_member_link.change": "Link a director or shareholder to the member who signs in as them",
  "director_fee.post": "Post a director's fee voted by the shareholders",
  "director_settlement.approve": "Settle what the company owes a director or shareholder",
  "director_repayment.void": "Void a repayment to a director or shareholder",
  "account.merge": "Merge an account into another",
  "bank_reconciliation.sign": "Sign off a reconciliation under an override of SOD-5",
  "holder_record.record": "Record a holder's details in the equity register",
  "share_class.record": "Record a share class and its rights",
  "equity_transaction.approve": "Approve an equity transaction: an issue, a transfer, a conversion or a reversal",
  "instrument.record": "Record a SAFE or convertible note",
  "instrument_template.enable": "Enable a SAFE template on its lawyer's confirmation",
  "option_pool.record": "Set the option pool by a resolution",
  "option_grant.record": "Record a grant of options",
  "option_event.record": "Record a forfeiture or expiry of options",
  "round_scenario.approve": "Approve a round, drafting its issues",
  "investor_right.record": "Record an investor's rights",
  "controller_entry.record": "Record a registrable controller conclusion",
  "budget.record": "Approve and lock a budget",
  "migration.commit": "Commit the workbook import",
  "tenant_export.prepare": "Prepare the full export",
  "ai_connection.create": "Connect an AI provider",
  "ai_connection.change": "Change an AI provider's connection",
  "ai_connection.credential": "Replace an AI provider's API key",
  "ai_connection.enable": "Switch on an AI provider's connection",
  "ai_data_policy.change": "Change where the AI may send each kind of data",
};

export function operationLabel(op: string): string {
  return operations[op as Approval["operation"]] ?? op;
}

const objects: Record<string, string> = {
  organisation_setting: "Organisation setting",
  membership: "Member's roles",
  member_invitation: "Invitation",
  approval_policy: "Approval policy",
  statutory_pack: "Statutory pack",
  team_mode: "Team mode",
  break_glass: "Break-glass",
  period: "Accounting period",
  journal: "Journal",
  opening_balance: "Opening balances",
  bill: "Bill",
  bill_payment: "Payment of bills",
  credit_application: "Credit applied to a bill",
  supplier_refund: "Refund from a supplier",
  purchase_order: "Purchase order",
  document_template: "Document template",
  invoice: "Invoice",
  customer_receipt: "Customer receipt",
  write_off: "Write-off",
  customer_credit_application: "Credit applied to an invoice",
  customer_refund: "Refund to a customer",
  customer_deposit: "Customer deposit",
  recurring_invoice: "Recurring invoice",
  connector: "Connector",
  bank_rule: "Bank rule",
  expense_claim: "Expense claim",
  claim_reimbursement: "Reimbursement of expense claims",
  cash_advance: "Cash advance",
  asset_disposal: "Disposal of a fixed asset",
  fixed_asset: "Fixed asset",
  payee_bank_change: "Supplier's bank details",
  claimant_bank_change: "Bank account for reimbursements",
  payment_batch: "Payment batch",
  contact_member_link: "Director's or shareholder's member",
  director_fee: "Director's fee",
  director_settlement: "Settlement of a founder's balance",
  director_repayment: "Repayment to a director",
  account: "Account",
  bank_reconciliation: "Bank reconciliation",
  holder_record: "Holder record",
  share_class: "Share class",
  equity_transaction: "Equity transaction",
  instrument: "SAFE or convertible note",
  instrument_template: "SAFE template",
  option_pool: "Option pool resolution",
  option_grant: "Grant of options",
  option_event: "Forfeiture or expiry of options",
  round_scenario: "Round scenario",
  investor_right: "Investor rights",
  controller_entry: "Registrable controller entry",
  budget: "Budget",
  migration_commit: "Workbook import",
  tenant_export: "Full export",
  ai_connection: "AI provider connection",
  ai_data_policy: "AI data-class policy",
};

export function objectLabel(t: string): string {
  return objects[t] ?? t;
}

const roles: Record<string, string> = {
  owner: "Owner",
  administrator: "Administrator",
  controller: "Controller",
  accountant: "Accountant",
  bookkeeper: "Bookkeeper",
  ar_specialist: "AR specialist",
  ap_specialist: "AP specialist",
  payment_releaser: "Payment releaser",
  payroll_administrator: "Payroll administrator",
  approver: "Approver",
  employee: "Employee",
  auditor: "Auditor",
  tax_agent: "Tax agent",
  board_viewer: "Board viewer",
  company_secretary: "Company secretary",
  dpo: "Data protection officer",
};

// roleNames are the roles of an organisation, in §7.1's order, as the API names them.
export const roleNames = Object.keys(roles);

// roleLabel names a role as people say it ("AR specialist").
export function roleLabel(r: string): string {
  return roles[r] ?? r;
}

const statuses: Record<Approval["status"], { label: string; icon: LucideIcon; tone: string }> = {
  pending: { label: "Waiting", icon: Hourglass, tone: "neutral" },
  approved: { label: "Approved", icon: CircleCheck, tone: "good" },
  rejected: { label: "Rejected", icon: CircleX, tone: "critical" },
  void: { label: "Void", icon: Ban, tone: "neutral" },
  expired: { label: "Expired", icon: ClockAlert, tone: "serious" },
};

// StatusChip is a request's status with its icon and words: colour never says it alone (FR-UX-09).
export function StatusChip({ status }: { status: Approval["status"] }) {
  const s = statuses[status];
  return (
    <span className={`fy-chip fy-chip--${s.tone}`}>
      <Icon of={s.icon} />
      {s.label}
    </span>
  );
}

// stepWho says who may sign a step: its roles and named people, and how many approvals it needs.
export function stepWho(step: { quorum: number; roles?: string[] | null; users?: string[] | null }): string {
  const who = [...(step.roles ?? []).map(roleLabel), ...((step.users ?? []).length ? [`${step.users?.length} named ${step.users?.length === 1 ? "person" : "people"}`] : [])];
  const n = step.quorum === 1 ? "1 approval" : `${step.quorum} approvals`;
  return `${n} from ${who.length ? who.join(" or ") : "anyone with the right"}`;
}
