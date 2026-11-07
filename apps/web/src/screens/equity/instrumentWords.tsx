import { CircleCheck, Hourglass, PencilLine } from "lucide-react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { Icon } from "../../ui";

export type Instrument = components["schemas"]["InstrumentView"];
export type InstrumentDraft = components["schemas"]["InstrumentDraft"];
export type Template = components["schemas"]["InstrumentTemplateView"];
export type Preview = components["schemas"]["ConversionPreview"];
export type Working = components["schemas"]["ConversionWorking"];
type Form = InstrumentDraft["form"];

// The roles that draft an instrument and ask for its approval (the Accountant's C/P on the cap table, Appendix D).
export const drafters = ["accountant"];

export const formWords: Record<Form, string> = {
  post_money_safe: "Post-money SAFE",
  pre_money_safe: "Pre-money SAFE",
  discount_safe: "Discount-only SAFE",
  mfn_safe: "MFN SAFE",
  convertible_note: "Convertible note",
};

export const triggerWords: Record<InstrumentDraft["trigger"], string> = {
  priced_round: "The next priced round",
  maturity: "Its maturity",
  liquidity_event: "A liquidity event",
  other: "As the terms say",
};

export const denominatorWords: Record<InstrumentDraft["denominator"], string> = {
  post_money_capitalisation: "The company after the SAFEs convert (post-money)",
  pre_money_shares: "The shares before the round (pre-money)",
  as_terms: "As the terms say",
};

// capped are the forms with a valuation cap.
export const capped = (f: Form) => f === "post_money_safe" || f === "pre_money_safe";

export async function listInstruments(): Promise<Instrument[]> {
  const { data, error } = await api.GET("/v1/instruments", { params: { query: { page_size: 200 } } });
  if (error) throw error;
  return data.items ?? [];
}

export async function instrumentOf(id: string): Promise<{ i: Instrument; etag: string }> {
  const { data, error, response } = await api.GET("/v1/instruments/{instrument_id}", { params: { path: { instrument_id: id } } });
  if (error) throw error;
  return { i: data, etag: response.headers.get("ETag") ?? `"${data.version}-${data.status === "draft" ? "draft" : "posted"}"` };
}

export async function listTemplates(): Promise<Template[]> {
  const { data, error } = await api.GET("/v1/instrument-templates", { params: { query: { page_size: 50 } } });
  if (error) throw error;
  return data.items ?? [];
}

// termsText is an instrument's terms in a line: its cap, its discount, a note's interest and maturity.
export function termsText(i: InstrumentDraft): string {
  const parts = [
    i.valuation_cap ? `cap ${formatAmount(i.valuation_cap)}` : "",
    i.discount ? `${i.discount}% discount` : "",
    i.mfn ? "most-favoured nation" : "",
    i.interest_rate ? `${i.interest_rate}% a year` : "",
    i.maturity_on ? `matures ${date(i.maturity_on)}` : "",
  ].filter(Boolean);
  return parts.length ? parts.join(", ") : "None";
}

// classificationText is the accountant's classification, liability or equity, on 2700 [verify per instrument].
export function classificationText(i: InstrumentDraft): string {
  return i.classification ? `${i.classification === "liability" ? "Liability" : "Equity"} · 2700` : "Not classified yet";
}

// InstrumentStatus is where an instrument stands: drafted, waiting for approval, recorded, converting or converted.
export function InstrumentStatus({ i }: { i: Instrument }) {
  if (i.conversion) {
    const done = i.conversion.status === "effective";
    return (
      <span className={`fy-chip ${done ? "fy-chip--good" : "fy-chip--neutral"}`}>
        <Icon of={done ? CircleCheck : Hourglass} />
        {done ? "Converted" : "Converting"}
      </span>
    );
  }
  if (i.status === "recorded") {
    return (
      <span className="fy-chip fy-chip--good">
        <Icon of={CircleCheck} />
        Recorded
      </span>
    );
  }
  return (
    <span className="fy-chip fy-chip--neutral">
      <Icon of={i.approval_id ? Hourglass : PencilLine} />
      {i.approval_id ? "Waiting for approval" : "Draft"}
    </span>
  );
}
