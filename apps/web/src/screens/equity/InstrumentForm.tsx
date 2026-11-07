import { useQuery } from "@tanstack/react-query";
import { useId } from "react";
import { listAccounts } from "../accounting/data";
import { listHolders } from "./Holders";
import { capped, denominatorWords, formWords, listTemplates, triggerWords, type InstrumentDraft } from "./instrumentWords";

export const blankInstrument: InstrumentDraft = {
  form: "post_money_safe",
  holder_id: "",
  amount: "",
  signed_on: "",
  trigger: "priced_round",
  class_account_id: "",
  denominator: "post_money_capitalisation",
  price_decimals: 4,
};

// instrumentBody is a draft as the API takes it: a field left empty is left out, the terms its form does not have too,
// and an MFN SAFE carries its clause.
export function instrumentBody(d: InstrumentDraft, classAccount: string): InstrumentDraft {
  const out: InstrumentDraft = { ...d, class_account_id: d.class_account_id || classAccount, mfn: d.form === "mfn_safe" || d.mfn || undefined };
  if (!capped(d.form)) delete out.valuation_cap;
  if (d.form !== "convertible_note") {
    delete out.interest_rate;
    delete out.maturity_on;
    delete out.interest_converts;
  }
  if (d.denominator !== "as_terms") delete out.denominator_note;
  for (const k of ["valuation_cap", "discount", "interest_rate", "maturity_on", "denominator_note", "classification", "template", "note", "class_id"] as const) {
    const v = out[k];
    if (v === undefined || (typeof v === "string" && v.trim() === "")) delete out[k];
  }
  out.amount = d.amount.trim().replace(/,/g, "");
  if (out.valuation_cap) out.valuation_cap = out.valuation_cap.replace(/,/g, "");
  return out;
}

// InstrumentFields are a SAFE's or note's terms (FR-EQ-18, design D6): who holds it, its form and amount, when it was
// signed, the terms its form has, what converts it, the class it converts into, how its terms count the shares, the
// conversion price's decimals, the accountant's classification and the template it follows. The holder stays once drafted.
export function InstrumentFields({ form, set, holderFixed }: { form: InstrumentDraft; set: (d: InstrumentDraft) => void; holderFixed: boolean }) {
  const id = useId();
  const holders = useQuery({ queryKey: ["holders"], queryFn: listHolders });
  const chart = useQuery({ queryKey: ["accounts", "all"], queryFn: listAccounts });
  const templates = useQuery({ queryKey: ["instrument-templates"], queryFn: listTemplates });
  const capital = (chart.data ?? []).filter((a) => a.role === "share-capital" && a.active && !a.group);
  const field = (k: keyof InstrumentDraft) => (e: { target: { value: string } }) => set({ ...form, [k]: e.target.value });
  const text = (k: "amount" | "valuation_cap" | "discount" | "interest_rate", label: string, help: string, decimal = true) => (
    <div className="fy-field">
      <label className="fy-label" htmlFor={`${id}-${k}`}>
        {label}
      </label>
      <input className={`fy-input${decimal ? " fy-num" : ""}`} id={`${id}-${k}`} inputMode={decimal ? "decimal" : undefined} value={form[k] ?? ""} onChange={field(k)}
        aria-describedby={`${id}-${k}-help`} />
      <span className="fy-help" id={`${id}-${k}-help`}>
        {help}
      </span>
    </div>
  );
  const fits = (templates.data ?? []).filter((t) => t.form === form.form);
  return (
    <div className="fy-form fy-form--2">
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-holder`}>
          Holder
        </label>
        <select className="fy-select" id={`${id}-holder`} value={form.holder_id} disabled={holderFixed} onChange={field("holder_id")}>
          <option value="">Choose an investor or shareholder</option>
          {(holders.data ?? [])
            .filter((h) => h.investor || h.shareholder)
            .map((h) => (
              <option key={h.contact_id} value={h.contact_id}>
                {h.name}
              </option>
            ))}
        </select>
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-form`}>
          Form
        </label>
        <select className="fy-select" id={`${id}-form`} value={form.form}
          onChange={(e) => {
            const f = e.target.value as InstrumentDraft["form"];
            set({ ...form, form: f, denominator: f === "post_money_safe" ? "post_money_capitalisation" : form.denominator === "post_money_capitalisation" ? "pre_money_shares" : form.denominator, template: "" });
          }}>
          {(Object.keys(formWords) as InstrumentDraft["form"][]).map((f) => (
            <option key={f} value={f}>
              {formWords[f]}
            </option>
          ))}
        </select>
      </div>
      {text("amount", "Amount paid", "In the base currency, such as 125000.00")}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-signed`}>
          Signed on
        </label>
        <input className="fy-input" type="date" id={`${id}-signed`} value={form.signed_on} onChange={field("signed_on")} />
      </div>
      {capped(form.form) && text("valuation_cap", "Valuation cap", "In the base currency, such as 2500000")}
      {form.form !== "mfn_safe" && text("discount", form.form === "discount_safe" ? "Discount" : "Discount (optional)", "A percentage off the round's price, such as 20")}
      {form.form === "convertible_note" && (
        <>
          {text("interest_rate", "Interest", "A percentage a year, such as 8")}
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-maturity`}>
              Matures on
            </label>
            <input className="fy-input" type="date" id={`${id}-maturity`} value={form.maturity_on ?? ""} onChange={field("maturity_on")} />
          </div>
          <label className="fy-row gap-2">
            <input type="checkbox" checked={Boolean(form.interest_converts)} onChange={(e) => set({ ...form, interest_converts: e.target.checked })} />
            Its interest converts with the principal
          </label>
        </>
      )}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-trigger`}>
          Converts at
        </label>
        <select className="fy-select" id={`${id}-trigger`} value={form.trigger} onChange={field("trigger")}>
          {(Object.keys(triggerWords) as InstrumentDraft["trigger"][]).map((t) => (
            <option key={t} value={t}>
              {triggerWords[t]}
            </option>
          ))}
        </select>
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-class`}>
          Converts into
        </label>
        <select className="fy-select" id={`${id}-class`} value={form.class_account_id || capital[0]?.id || ""} onChange={field("class_account_id")}>
          {capital.map((a) => (
            <option key={a.id} value={a.id}>
              {a.code} {a.name}
            </option>
          ))}
        </select>
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-denominator`}>
          Its price divides by
        </label>
        <select className="fy-select" id={`${id}-denominator`} value={form.denominator} onChange={field("denominator")}>
          {(Object.keys(denominatorWords) as InstrumentDraft["denominator"][]).map((d) => (
            <option key={d} value={d}>
              {denominatorWords[d]}
            </option>
          ))}
        </select>
      </div>
      {form.denominator === "as_terms" && (
        <div className="fy-field fy-field--full">
          <label className="fy-label" htmlFor={`${id}-denominator-note`}>
            How the terms count the shares
          </label>
          <input className="fy-input" id={`${id}-denominator-note`} maxLength={1000} value={form.denominator_note ?? ""} onChange={field("denominator_note")} />
        </div>
      )}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-decimals`}>
          Price decimals
        </label>
        <select className="fy-select" id={`${id}-decimals`} value={form.price_decimals} onChange={(e) => set({ ...form, price_decimals: Number(e.target.value) })}
          aria-describedby={`${id}-decimals-help`}>
          {[0, 1, 2, 3, 4, 5, 6].map((n) => (
            <option key={n} value={n}>
              {n}
            </option>
          ))}
        </select>
        <span className="fy-help" id={`${id}-decimals-help`}>
          The decimals the terms give the conversion price; Fyniq cuts the price down to them
        </span>
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-classification`}>
          Classification (the accountant's)
        </label>
        <select className="fy-select" id={`${id}-classification`} value={form.classification ?? ""} onChange={field("classification")}>
          <option value="">Not decided yet</option>
          <option value="liability">Liability</option>
          <option value="equity">Equity</option>
        </select>
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-template`}>
          Template (optional)
        </label>
        <select className="fy-select" id={`${id}-template`} value={form.template ?? ""} onChange={field("template")}>
          <option value="">It follows no template</option>
          {fits.map((t) => (
            <option key={t.key} value={t.key}>
              {t.name}
            </option>
          ))}
        </select>
      </div>
      <label className="fy-row gap-2">
        <input type="checkbox" checked={Boolean(form.pro_rata)} onChange={(e) => set({ ...form, pro_rata: e.target.checked })} />
        A pro rata side letter
      </label>
      <div className="fy-field fy-field--full">
        <label className="fy-label" htmlFor={`${id}-note`}>
          Note (optional)
        </label>
        <input className="fy-input" id={`${id}-note`} maxLength={1000} value={form.note ?? ""} onChange={field("note")} />
      </div>
    </div>
  );
}
