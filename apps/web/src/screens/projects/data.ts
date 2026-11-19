import { api } from "../../api/client";
import type { components } from "../../api/schema";

// What the Projects pages share (FR-PRJ-01): the project and its people and tasks, as the API gives them, and the lists
// that read every page.

export type Project = components["schemas"]["Project"];
export type ProjectPerson = components["schemas"]["ProjectPerson"];
export type ProjectTask = components["schemas"]["ProjectTask"];
export type ProjectDraft = components["schemas"]["ProjectDraft"];
export type ProjectChange = components["schemas"]["ProjectChange"];

// The roles that keep projects: C on "Chart of accounts, dimensions" (Appendix D), which the API decides.
export const keepers = ["accountant", "tax_agent"];

export const billingNames: Record<NonNullable<Project["billing_method"]>, string> = {
  time_and_materials: "Time and materials",
  fixed_fee: "Fixed fee",
  retainer: "Retainer",
  internal: "Internal",
};

export const statusNames: Record<Project["status"], string> = { planned: "Planned", active: "Active", on_hold: "On hold", closed: "Closed" };

// basisWords say where a time cost's rates came from (FR-PRJ-06): the server names the basis, and the pages say it in words.
export const basisWords: Record<string, string> = {
  person: "each person's own rate",
  role_average: "the average rate of their role",
  standard: "the standard rate",
  mixed: "more than one rate",
  none: "hours only",
};

// monthsBack is the month n months before the month of day, as YYYY-MM (UTC).
export function monthsBack(day: Date, n: number): string {
  return new Date(Date.UTC(day.getUTCFullYear(), day.getUTCMonth() - n, 1)).toISOString().slice(0, 7);
}

// hours writes a number of minutes as hours and minutes: 150 is "2 h 30 min", 24000 is "400 h".
export function hours(minutes: number): string {
  const sign = minutes < 0 ? "-" : "";
  const m = Math.abs(minutes);
  const h = Math.floor(m / 60);
  const rest = m % 60;
  if (rest === 0) return `${sign}${h} h`;
  return h === 0 ? `${sign}${rest} min` : `${sign}${h} h ${rest} min`;
}

// minutesOf reads hours typed as "400", "7.5" or "7:30" into whole minutes, or null when it is not hours.
export function minutesOf(text: string): number | null {
  const t = text.trim();
  if (/^\d+:[0-5]\d$/.test(t)) {
    const [h, m] = t.split(":").map(Number);
    return (h ?? 0) * 60 + (m ?? 0);
  }
  if (/^\d+(\.\d{1,2})?$/.test(t)) return Math.round(Number(t) * 60);
  return null;
}

export async function listProjects(): Promise<Project[]> {
  const out: Project[] = [];
  let cursor: string | undefined;
  do {
    const { data, error } = await api.GET("/v1/projects", { params: { query: { page_size: 500, cursor } } });
    if (error) throw error;
    out.push(...(data.items ?? []));
    cursor = data.next_cursor;
  } while (cursor);
  return out;
}

export async function listProjectPeople(): Promise<ProjectPerson[]> {
  const out: ProjectPerson[] = [];
  let cursor: string | undefined;
  do {
    const { data, error } = await api.GET("/v1/project-people", { params: { query: { page_size: 500, cursor } } });
    if (error) throw error;
    out.push(...(data.items ?? []));
    cursor = data.next_cursor;
  } while (cursor);
  return out;
}

export async function listCustomers(): Promise<{ id: string; legal_name: string }[]> {
  const out: { id: string; legal_name: string }[] = [];
  let cursor: string | undefined;
  do {
    const { data, error } = await api.GET("/v1/contacts", { params: { query: { page_size: 500, cursor, role: "customer" } } });
    if (error) throw error;
    out.push(...(data.items ?? []).map((c) => ({ id: c.id, legal_name: c.legal_name })));
    cursor = data.next_cursor;
  } while (cursor);
  return out;
}
