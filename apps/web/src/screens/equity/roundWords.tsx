import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { typed } from "../../format/decimal";

export type RoundScenario = components["schemas"]["RoundScenarioView"];
export type RoundDraft = components["schemas"]["RoundScenarioDraft"];
export type RoundOutcome = components["schemas"]["RoundOutcomeView"];
export type RoundHolder = components["schemas"]["RoundHolderView"];

// Basis is what sets a round's price: a pre-money valuation, or a price per share typed in.
export type Basis = "pre_money" | "price";

// RoundForm is a scenario as the simulator edits it: the draft, with both the valuation and the price kept while the
// person switches between them; roundBody sends only the one chosen.
export interface RoundForm {
  draft: RoundDraft;
  basis: Basis;
}

export function blankRound(on: string, classAccount: string, name: string): RoundForm {
  return {
    basis: "pre_money",
    draft: {
      name,
      on,
      pre_money: "",
      price: "",
      price_decimals: 4,
      class_account_id: classAccount,
      investors: [{ holder_id: "", amount: "" }],
      top_up: "",
      top_up_before: true,
      instruments: [],
    },
  };
}

// formOf is a saved scenario as the simulator edits it.
export function formOf(s: RoundScenario): RoundForm {
  const d: RoundDraft = {
    name: s.name,
    on: s.on,
    pre_money: s.pre_money ?? "",
    price: s.price ?? "",
    price_decimals: s.price_decimals,
    class_account_id: s.class_account_id,
    investors: s.investors.map((i) => ({ ...i })),
    top_up: s.top_up ?? "",
    top_up_before: s.top_up_before ?? false,
    instruments: [...s.instruments],
    resolution_refs: s.resolution_refs ?? "",
    note: s.note ?? "",
  };
  return { basis: s.price ? "price" : "pre_money", draft: d };
}

// plain is an amount or a count as the API takes it: what was typed, read as a decimal ("1,500,000" → "1500000"), or
// left as typed when it is not one, so the server says what is wrong with it.
function plain(v: string | undefined): string {
  const t = typed(v ?? "");
  return t === null ? (v ?? "").trim() : t;
}

// roundBody is a scenario as the API takes it: the price or the valuation the basis names, amounts and counts without
// separators, and the optional fields left out when empty. Basic and Expert send the same body (FR-UX-24).
export function roundBody(f: RoundForm): RoundDraft {
  const d = f.draft;
  const out: RoundDraft = {
    name: d.name.trim(),
    on: d.on,
    price_decimals: d.price_decimals,
    class_account_id: d.class_account_id,
    investors: d.investors.map((i) => ({ holder_id: i.holder_id, amount: plain(i.amount) })),
    instruments: [...d.instruments],
  };
  if (f.basis === "price") out.price = plain(d.price);
  else out.pre_money = plain(d.pre_money);
  const top = plain(d.top_up);
  if (top !== "" && top !== "0") {
    out.top_up = top;
    out.top_up_before = d.top_up_before ?? false;
  }
  if (d.resolution_refs?.trim()) out.resolution_refs = d.resolution_refs.trim();
  if (d.note?.trim()) out.note = d.note.trim();
  return out;
}

// sameScenario says whether the form still holds what the saved scenario does.
export function sameScenario(f: RoundForm, s: RoundScenario): boolean {
  return JSON.stringify(roundBody(f)) === JSON.stringify(roundBody(formOf(s)));
}

// points shows a holder's dilution as the change in their part: the server's points lost, "5.98", read as "−5.98"; a
// gain, "-0.40", as "+0.40". Only the sign is turned; the figure is the server's.
export function points(dilution: string | undefined): string {
  if (!dilution) return "–";
  if (/^0(\.0+)?$/.test(dilution)) return "0.00";
  return dilution.startsWith("-") ? `+${dilution.slice(1)}` : `−${dilution}`;
}

export async function listScenarios(): Promise<RoundScenario[]> {
  const { data, error } = await api.GET("/v1/round-scenarios", { params: { query: { page_size: 50 } } });
  if (error) throw error;
  return data.items ?? [];
}

export async function scenarioOf(id: string): Promise<{ s: RoundScenario; etag: string }> {
  const { data, error, response } = await api.GET("/v1/round-scenarios/{scenario_id}", { params: { path: { scenario_id: id } } });
  if (error) throw error;
  return { s: data, etag: response.headers.get("ETag") ?? "" };
}

// modelRound tries a scenario in the sandbox: its outcome against today's books, saving nothing.
export function modelRound(d: RoundDraft) {
  return api.POST("/v1/round-models", { body: d });
}

export function saveScenario(d: RoundDraft, saved?: { id: string; etag: string }) {
  if (!saved) return api.POST("/v1/round-scenarios", { params: { header: { "Idempotency-Key": newKey() } }, body: d });
  return api.PUT("/v1/round-scenarios/{scenario_id}", { params: { path: { scenario_id: saved.id }, header: { "If-Match": saved.etag, "Idempotency-Key": newKey() } }, body: d });
}
