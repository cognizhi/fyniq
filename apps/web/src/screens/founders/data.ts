import { useQuery } from "@tanstack/react-query";
import { api } from "../../api/client";
import type { components } from "../../api/schema";

// What the founders' pages share (PRD-001 §8.22; p1-founders-money design D1 to D5): the directors and shareholders,
// the director's accounts' roles, and the words for where an amount came from (FR-EQ-01).

export type Contact = components["schemas"]["Contact"];
export type DirectorAccount = components["schemas"]["DirectorAccount"];
export type DirectorStatement = components["schemas"]["DirectorStatement"];

// The roles of a director's account (2500, 2120): a line on one names its person and origin (B.7).
export const directorRoles: readonly string[] = ["director-loan", "deferred-remuneration"];
export const isDirectorAccount = (a?: { role?: string }) => !!a?.role && directorRoles.includes(a.role);

// The origins a person enters on a journal or opening balance line, then the ones only a source gives.
export const enteredOrigins = ["lent", "paid_personally", "pay", "fee", "paid_out", "other"] as const;
export type Origin = (typeof enteredOrigins)[number] | "converted" | "waived";

export const originWords: Record<Origin, string> = {
  lent: "Lent to the company",
  paid_personally: "Paid personally",
  pay: "Pay deferred",
  fee: "Director's fee",
  paid_out: "Paid out to them",
  converted: "Converted to shares",
  waived: "Waived",
  other: "Other",
};

export function originLabel(o: string): string {
  return originWords[o as Origin] ?? o;
}

// listDirectors reads every contact marked director or shareholder, a page at a time.
export async function listDirectors(): Promise<Contact[]> {
  const out: Contact[] = [];
  let cursor: string | undefined;
  do {
    const { data, error } = await api.GET("/v1/contacts", { params: { query: { role: "director", page_size: 500, cursor } } });
    if (error) throw error;
    out.push(...(data.items ?? []));
    cursor = data.next_cursor;
  } while (cursor);
  return out;
}

export function useDirectors(enabled = true) {
  return useQuery({ queryKey: ["contacts", "director"], queryFn: listDirectors, enabled, staleTime: 60_000 });
}
