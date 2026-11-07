import { CircleCheck, ClockAlert, Landmark, PencilLine, TriangleAlert, Undo2 } from "lucide-react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Icon } from "../../ui";

export type EquityTransaction = components["schemas"]["EquityTransaction"];
export type TransactionKind = components["schemas"]["EquityTransactionDraft"]["kind"];

// The roles that draft and ask (the Accountant's C/P on the cap table), and those that approve, record the ACRA steps
// and the effective date (A: an Owner or the Controller), as Appendix D's "Cap table" row gives them.
export const drafters = ["accountant"];
export const recorders = ["owner", "controller"];

export const kindWords: Record<TransactionKind, string> = {
  issue_cash: "Issue for cash",
  transfer: "Transfer",
  class_conversion: "Conversion between classes",
  reversal: "Reversal",
};

// The ACRA steps each kind records, in order, and what each is called (FR-EQ-09).
export const stepsOf: Record<TransactionKind, { kind: string; label: string }[]> = {
  issue_cash: [
    { kind: "resolved", label: "Shareholders' approval to issue the shares" },
    { kind: "approval_lodged", label: "Approval lodged with ACRA" },
    { kind: "agm_due", label: "The next AGM, when the approval lapses" },
    { kind: "allotment_lodged", label: "Return of allotment lodged" },
  ],
  transfer: [
    { kind: "instrument_signed", label: "Instrument of transfer signed" },
    { kind: "transfer_lodged", label: "Notice of transfer lodged with ACRA" },
  ],
  class_conversion: [
    { kind: "resolved", label: "Shareholders' resolution" },
    { kind: "conversion_lodged", label: "Conversion lodged with ACRA" },
  ],
  reversal: [],
};

export async function listTransactions(): Promise<EquityTransaction[]> {
  const { data, error } = await api.GET("/v1/equity-transactions", { params: { query: { page_size: 200 } } });
  if (error) throw error;
  return data.items ?? [];
}

export async function transactionOf(id: string): Promise<{ t: EquityTransaction; etag: string }> {
  const { data, error, response } = await api.GET("/v1/equity-transactions/{transaction_id}", { params: { path: { transaction_id: id } } });
  if (error) throw error;
  return { t: data, etag: response.headers.get("ETag") ?? `"${data.version}-${data.status === "draft" ? "draft" : "posted"}"` };
}

// describe is a transaction's type as a list shows it: a reversal names what it undoes.
export function describe(t: EquityTransaction, numbers: Map<string, string>): string {
  if (t.details.kind === "reversal") {
    const of = t.details.reverses_id ? numbers.get(t.details.reverses_id) : undefined;
    return of ? `Reversal of ${of}` : "Reversal";
  }
  return kindWords[t.details.kind];
}

export function holdersText(t: EquityTransaction): string {
  return t.parties.map((p) => (t.details.kind === "transfer" ? `${p.name} (${p.role})` : p.name)).join(", ") || "–";
}

export function sharesText(shares?: string): string {
  if (!shares) return "–";
  const n = Number(shares);
  return Number.isSafeInteger(n) ? n.toLocaleString("en-SG") : shares;
}

// StatusChip is where a transaction stands, with what its ACRA steps raise.
export function StatusChip({ t }: { t: EquityTransaction }) {
  const chip = {
    draft: { cls: "fy-chip--neutral", icon: PencilLine, text: "Draft" },
    approved: { cls: "fy-chip--info", icon: Landmark, text: "Waiting for ACRA" },
    effective: { cls: "fy-chip--good", icon: CircleCheck, text: "Effective" },
    reversed: { cls: "fy-chip--neutral", icon: Undo2, text: "Reversed" },
  }[t.status];
  return (
    <span className="fy-row fy-row--wrap gap-2">
      <span className={`fy-chip ${chip.cls}`}>
        <Icon of={chip.icon} />
        {chip.text}
      </span>
      {t.status === "approved" &&
        (t.acra?.flags ?? []).map((f) => (
          <span key={f.code} className={`fy-chip ${f.kind === "due" ? "fy-chip--serious" : "fy-chip--warning"}`} title={`${f.message}. ${f.fix}.`}>
            <Icon of={f.kind === "due" ? ClockAlert : TriangleAlert} />
            {f.kind === "due" ? "Due" : "Review"}
          </span>
        ))}
    </span>
  );
}
