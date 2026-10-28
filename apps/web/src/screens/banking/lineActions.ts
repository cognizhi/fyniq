import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { minus, negate, sign } from "../../format/decimal";

// The requests the categorise queue sends (FR-BNK-05…09; p1-bank-import-reconcile task 10.3). Basic's card and Expert's
// grid build them here, so the same action sends the same request in both modes (FR-UX-24, QA-UI-04).

export type Line = components["schemas"]["BankLine"];
export type Suggestion = components["schemas"]["BankSuggested"];
export type Rule = components["schemas"]["BankRule"];
export type Categorisation = components["schemas"]["BankCategorisation"];
type Money = components["schemas"]["Money"];

// Request is a line action: its path and body, sent with Idempotency-Key and the line's If-Match.
export type Request =
  | { action: "categorise"; body: Categorisation }
  | { action: "match-invoices"; body: components["schemas"]["BankInvoicesMatch"] }
  | { action: "match-bills"; body: components["schemas"]["BankBillsMatch"] }
  | { action: "match-recorded"; body: { document_id: string } }
  | { action: "suspend" };

// settles is what a line settles of a document: what is open of it, or the line's size when the line is smaller, in
// the document's currency; across currencies, what is open.
export function settles(line: Line, open: Money): Money {
  const size = sign(line.amount.amount) < 0 ? negate(line.amount.amount) : line.amount.amount;
  if (open.currency !== line.amount.currency) return open;
  return sign(minus(size, open.amount)) < 0 ? { amount: size, currency: open.currency } : open;
}

// acceptRequest is the request that accepts a suggestion: a match for a document, or a categorisation by a rule's action.
// A rule suggestion needs the rule, read when it is accepted.
export function acceptRequest(line: Line, s: Suggestion, rule?: Rule): Request | null {
  switch (s.kind) {
    case "receipt":
      if (!s.document_id || !s.contact_id || !s.open) return null;
      return {
        action: "match-invoices",
        body: {
          customer_id: s.contact_id,
          currency: s.open.currency === line.amount.currency ? undefined : s.open.currency,
          invoices: [{ invoice_id: s.document_id, amount: settles(line, s.open) }],
        },
      };
    case "bill_payment":
      if (!s.document_id || !s.contact_id || !s.open) return null;
      return {
        action: "match-bills",
        body: {
          supplier_id: s.contact_id,
          currency: s.open.currency === line.amount.currency ? undefined : s.open.currency,
          bills: [{ bill_id: s.document_id, amount: settles(line, s.open) }],
        },
      };
    case "recorded":
      return s.document_id ? { action: "match-recorded", body: { document_id: s.document_id } } : null;
    case "categorised":
      if (!rule) return null;
      return {
        action: "categorise",
        body: {
          contact_id: rule.then.contact_id,
          reference: rule.then.reference,
          parts: [{ account_id: rule.then.account_id, gst_code: rule.then.gst_code }],
        },
      };
  }
  return null;
}

// send sends a line action with a new Idempotency-Key and the line's ETag.
export async function send(line: { id: string }, etag: string, r: Request) {
  const params = { path: { line_id: line.id }, header: { "Idempotency-Key": newKey(), "If-Match": etag } };
  switch (r.action) {
    case "categorise":
      return api.POST("/v1/bank-lines/{line_id}/categorise", { params, body: r.body });
    case "match-invoices":
      return api.POST("/v1/bank-lines/{line_id}/match-invoices", { params, body: r.body });
    case "match-bills":
      return api.POST("/v1/bank-lines/{line_id}/match-bills", { params, body: r.body });
    case "match-recorded":
      return api.POST("/v1/bank-lines/{line_id}/match-recorded", { params, body: r.body });
    case "suspend":
      return api.POST("/v1/bank-lines/{line_id}/suspend", { params });
  }
}

// rule reads the rule a suggestion names.
export async function rule(id: string): Promise<Rule> {
  const { data, error } = await api.GET("/v1/bank-rules/{rule_id}", { params: { path: { rule_id: id } } });
  if (error) throw error;
  return data;
}

// What a suggestion is, in words: a document's payment, money already recorded (by hand or by a batch's release), or a rule.
export function suggestionWords(line: Line, s: Suggestion): string {
  const open = s.open ? ` (${s.open.amount} ${s.open.currency} open)` : "";
  switch (s.kind) {
    case "receipt":
      return `Payment of ${s.number ?? "an invoice"} from ${s.contact ?? "the customer"}${open}`;
    case "bill_payment":
      return `Payment of ${s.number ?? "a bill"} to ${s.contact ?? "the supplier"}${open}`;
    case "recorded":
      return `${sign(line.amount.amount) > 0 ? "Money received" : "Money paid"}, already recorded: ${s.number ?? "a document"}`;
    default:
      return `Rule: ${s.rule_name ?? "a bank rule"}`;
  }
}

export const found: Record<Suggestion["found"], string> = {
  recorded: "Recorded already",
  reference: "Number matches",
  normalised: "Number matches, once tidied",
  amount: "Amount and name match",
  rule: "A rule",
};

export interface PartForm {
  key: string;
  account_id: string;
  amount: string;
  gst_code: string;
  description: string;
}

// checkCategorisation reads the change or split form: every part's account, at most one part without its amount, and
// amounts as plain decimals; every problem at once (ENG-23).
export function checkCategorisation(parts: PartForm[], contact: string, reference: string, currency: string): { body?: Categorisation; errors: Record<string, string> } {
  const errors: Record<string, string> = {};
  let open = 0;
  for (const p of parts) {
    if (!p.account_id) errors[`${p.key}.account`] = "Choose the account it posts to.";
    if (p.amount.trim() === "") open++;
    else if (!/^-?\d+(\.\d{1,4})?$/.test(p.amount.trim())) errors[`${p.key}.amount`] = "Enter the amount as a plain decimal, such as -120.50.";
  }
  if (parts.length > 1 && open > 1) errors.parts = "Leave the amount out of one part at most: that part takes the rest of the line.";
  if (Object.keys(errors).length > 0) return { errors };
  return {
    errors,
    body: {
      contact_id: contact || undefined,
      reference: reference.trim() || undefined,
      parts: parts.map((p) => ({
        account_id: p.account_id,
        amount: p.amount.trim() ? { amount: p.amount.trim(), currency } : undefined,
        gst_code: p.gst_code || undefined,
        description: p.description.trim() || undefined,
      })),
    },
  };
}
