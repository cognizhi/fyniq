import { useQuery } from "@tanstack/react-query";
import { TriangleAlert } from "lucide-react";
import { useId } from "react";
import type { components } from "../../api/schema";
import { api } from "../../api/client";
import { Icon } from "../../ui";
import { listAccounts } from "../accounting/data";
import { listHolders } from "./Holders";
import { kindWords, listTransactions, reviewText, type TransactionKind } from "./transactionWords";

export type TransactionDraft = components["schemas"]["EquityTransactionDraft"];
type ShareClass = components["schemas"]["ShareClassView"];

export const blankDraft: TransactionDraft = { kind: "issue_cash" };

async function recordedClasses(): Promise<ShareClass[]> {
  const { data, error } = await api.GET("/v1/share-classes", { params: { query: { page_size: 200 } } });
  if (error) throw error;
  return (data.items ?? []).filter((c) => c.status === "recorded");
}

// bodyOf is a draft as the API takes it: a field left empty is left out, so an ID is never sent blank.
export function bodyOf(d: TransactionDraft): TransactionDraft {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(d)) {
    const s = typeof v === "string" ? v.trim() : "";
    if (s && k !== "review") out[k] = k === "shares" || k === "to_shares" ? s.replace(/,/g, "") : s;
  }
  return out as TransactionDraft;
}

// previewOf says what the transaction will post when it takes effect, in words: the figures come from the server once
// it is saved (FR-UX-22: no optimistic money).
export function previewOf(d: TransactionDraft, codeOf: (id?: string) => string): string {
  switch (d.kind) {
    case "issue_cash":
      return `When ACRA's register shows it, it posts Dr 2040 Share application monies, naming the subscriber · Cr ${codeOf(d.class_account_id) || "the class"}, with shares × price. It waits until the subscriber's money is in share application monies.`;
    case "transfer":
      return "No entry in the company's books: the shares move between holders. Share duty on the instrument of transfer is the parties' matter.";
    case "class_conversion":
      return d.class_account_id && d.to_class_account_id && d.class_account_id !== d.to_class_account_id
        ? `When ACRA's register shows it, it posts Dr ${codeOf(d.class_account_id)} · Cr ${codeOf(d.to_class_account_id)} with the carrying amount you enter.`
        : "No entry when both classes are on one share-capital account: the shares move between classes.";
    case "reversal":
      return "Undoes every holding movement and entry of the transaction it names, on its effective date.";
    case "instrument_conversion":
      return `When ACRA's register shows it, it posts Dr 2700 Convertible instruments, naming the instrument · Cr ${codeOf(d.class_account_id) || "the class"}, with shares × the conversion price. What is left of the instrument's money stays on 2700 until it is settled under its terms.`;
  }
}

// TransactionFields are the fields of an equity transaction for its kind (FR-EQ-17). The kind is fixed once drafted.
export function TransactionFields({ form, set, kindFixed }: { form: TransactionDraft; set: (d: TransactionDraft) => void; kindFixed: boolean }) {
  const id = useId();
  const holders = useQuery({ queryKey: ["holders"], queryFn: listHolders });
  const chart = useQuery({ queryKey: ["accounts", "all"], queryFn: listAccounts });
  const classes = useQuery({ queryKey: ["share-classes", "recorded"], queryFn: recordedClasses });
  const txs = useQuery({ queryKey: ["equity-transactions"], queryFn: listTransactions, enabled: form.kind === "reversal" });
  const capital = (chart.data ?? []).filter((a) => a.role === "share-capital" && a.active && !a.group);
  const field = (k: keyof TransactionDraft) => (e: { target: { value: string } }) => set({ ...form, [k]: e.target.value });
  const holderSelect = (k: "holder_id" | "seller_id" | "buyer_id", label: string, who: (h: { shareholder: boolean; investor: boolean; director: boolean }) => boolean) => (
    <div className="fy-field">
      <label className="fy-label" htmlFor={`${id}-${k}`}>
        {label}
      </label>
      <select className="fy-select" id={`${id}-${k}`} value={form[k] ?? ""} onChange={field(k)}>
        <option value="">Choose a holder</option>
        {(holders.data ?? [])
          .filter(who)
          .map((h) => (
            <option key={h.contact_id} value={h.contact_id}>
              {h.name}
            </option>
          ))}
      </select>
    </div>
  );
  const classSelect = (acc: "class_account_id" | "to_class_account_id", cls: "class_id" | "to_class_id", label: string) => (
    <>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-${acc}`}>
          {label}
        </label>
        <select className="fy-select" id={`${id}-${acc}`} value={form[acc] ?? ""} onChange={(e) => set({ ...form, [acc]: e.target.value, [cls]: "" })}>
          <option value="">Choose a share-capital account</option>
          {capital.map((a) => (
            <option key={a.id} value={a.id}>
              {a.code} {a.name}
            </option>
          ))}
        </select>
      </div>
      {(classes.data ?? []).some((c) => c.account_id === form[acc]) && (
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-${cls}`}>
            Class
          </label>
          <select className="fy-select" id={`${id}-${cls}`} value={form[cls] ?? ""} onChange={field(cls)}>
            <option value="">The account's first class</option>
            {(classes.data ?? [])
              .filter((c) => c.account_id === form[acc])
              .map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
          </select>
        </div>
      )}
    </>
  );
  const text = (k: keyof TransactionDraft, label: string, help?: string, max = 500) => (
    <div className="fy-field">
      <label className="fy-label" htmlFor={`${id}-${k}`}>
        {label}
      </label>
      <input className="fy-input" id={`${id}-${k}`} maxLength={max} value={(form[k] as string | undefined) ?? ""} onChange={field(k)}
        aria-describedby={help ? `${id}-${k}-help` : undefined} />
      {help && (
        <span className="fy-help" id={`${id}-${k}-help`}>
          {help}
        </span>
      )}
    </div>
  );
  const subscriber = (h: { shareholder: boolean; investor: boolean }) => h.shareholder || h.investor;
  const anyHolder = () => true;
  return (
    <div className="fy-form fy-form--2">
      <div className="fy-field fy-field--full">
        <label className="fy-label" htmlFor={`${id}-kind`}>
          Type
        </label>
        <select className="fy-select" id={`${id}-kind`} value={form.kind} disabled={kindFixed}
          onChange={(e) => set({ ...blankDraft, kind: e.target.value as TransactionKind })}>
          {(Object.keys(kindWords) as TransactionKind[]).filter((k) => k !== "instrument_conversion" || form.kind === k).map((k) => (
            <option key={k} value={k}>
              {kindWords[k]}
            </option>
          ))}
        </select>
      </div>
      {form.kind === "issue_cash" && (
        <>
          {holderSelect("holder_id", "Subscriber", subscriber)}
          {classSelect("class_account_id", "class_id", "Share capital")}
          {text("shares", "Shares", "A whole number, such as 250000")}
          {text("price", "Price per share", "In the base currency, such as 2.40")}
          <div className="fy-field--full">{text("resolution_refs", "Resolutions", "The shareholders' approval and the board resolution, by reference")}</div>
        </>
      )}
      {form.kind === "transfer" && (
        <>
          {holderSelect("seller_id", "From", anyHolder)}
          {holderSelect("buyer_id", "To", subscriber)}
          {classSelect("class_account_id", "class_id", "Share capital")}
          {text("shares", "Shares", "A whole number the seller holds on the day")}
          {text("price", "Price agreed per share (optional)", "For information: share duty is the parties' matter")}
          {text("resolution_refs", "Board approval (optional)")}
        </>
      )}
      {form.kind === "class_conversion" && (
        <>
          {holderSelect("holder_id", "Holder", anyHolder)}
          {classSelect("class_account_id", "class_id", "From share capital")}
          {classSelect("to_class_account_id", "to_class_id", "Into share capital")}
          {text("shares", "Shares out")}
          {text("to_shares", "Shares in")}
          {form.class_account_id && form.to_class_account_id && form.class_account_id !== form.to_class_account_id &&
            text("amount", "Carrying amount", "The share capital the converted shares carry, moved between the two accounts [the accountant confirms the basis]")}
          <div className="fy-field--full">{text("resolution_refs", "Resolutions")}</div>
        </>
      )}
      {form.kind === "reversal" && (
        <>
          <div className="fy-field fy-field--full">
            <label className="fy-label" htmlFor={`${id}-reverses`}>
              Transaction it undoes
            </label>
            <select className="fy-select" id={`${id}-reverses`} value={form.reverses_id ?? ""} onChange={field("reverses_id")}>
              <option value="">Choose an effective transaction</option>
              {(txs.data ?? [])
                .filter((t) => t.status === "effective" && t.details.kind !== "reversal")
                .map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.number} {kindWords[t.details.kind].toLowerCase()}, {t.parties.map((p) => p.name).join(", ")}
                  </option>
                ))}
            </select>
          </div>
          <div className="fy-field--full">{text("reason", "Why", "Such as: ACRA's register of members shows 50,000 shares; attach the extract")}</div>
        </>
      )}
      {form.kind === "instrument_conversion" && (
        <>
          {form.review && (
            <p className="fy-field--full fy-small" role="note">
              <Icon of={TriangleAlert} /> {reviewText(form.review)}
            </p>
          )}
          <p className="fy-field--full fy-small fy-secondary">The holder and the share class are the instrument's.</p>
          {text("shares", "Shares", "A whole number within the instrument's amount, such as 52631")}
          {text("price", "Conversion price per share", "The lower of the cap price and the discounted round price, such as 2.375")}
        </>
      )}
      <div className="fy-field--full">{text("note", "Note (optional)", undefined, 1000)}</div>
    </div>
  );
}
