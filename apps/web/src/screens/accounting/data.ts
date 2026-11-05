import { api } from "../../api/client";
import type { components } from "../../api/schema";

// Reads the accounting pages share. Each reads every page the API answers, a page at a time.

export type Account = components["schemas"]["Account"];
export type Period = components["schemas"]["Period"];
export type PeriodPage = components["schemas"]["PeriodPage"];

export async function listAccounts(): Promise<Account[]> {
  const out: Account[] = [];
  let cursor: string | undefined;
  do {
    const { data, error } = await api.GET("/v1/accounts", { params: { query: { page_size: 500, cursor } } });
    if (error) throw error;
    out.push(...(data.items ?? []));
    cursor = data.next_cursor;
  } while (cursor);
  return out;
}

export type Dimension = components["schemas"]["Dimension"];
export type DimensionValue = components["schemas"]["DimensionValue"];

// listDimensions answers every dimension, Department and Project first, then the custom ones by place, and every value,
// by dimension then code (FR-DIM-01).
export async function listDimensions(): Promise<{ dimensions: Dimension[]; values: DimensionValue[] }> {
  const dimensions: Dimension[] = [];
  const values: DimensionValue[] = [];
  let cursor: string | undefined;
  do {
    const { data, error } = await api.GET("/v1/dimensions", { params: { query: { page_size: 500, cursor } } });
    if (error) throw error;
    dimensions.push(...(data.items ?? []));
    cursor = data.next_cursor;
  } while (cursor);
  do {
    const { data, error } = await api.GET("/v1/dimension-values", { params: { query: { page_size: 500, cursor } } });
    if (error) throw error;
    values.push(...(data.items ?? []));
    cursor = data.next_cursor;
  } while (cursor);
  return { dimensions, values };
}

// listPeriods answers every period, earliest first, and the financial year that opens next.
export async function listPeriods(): Promise<{ items: Period[]; next_year?: PeriodPage["next_year"] }> {
  const items: Period[] = [];
  let cursor: string | undefined;
  let next: PeriodPage["next_year"];
  do {
    const { data, error } = await api.GET("/v1/periods", { params: { query: { page_size: 500, cursor } } });
    if (error) throw error;
    items.push(...(data.items ?? []));
    next = data.next_year;
    cursor = data.next_cursor;
  } while (cursor);
  return { items, next_year: next };
}

// postable are the accounts a line may post to: active and not a group, by code.
export function postable(accounts: readonly Account[]): Account[] {
  return accounts.filter((a) => a.active && !a.group).sort((x, y) => x.code.localeCompare(y.code, "en", { numeric: true }));
}
