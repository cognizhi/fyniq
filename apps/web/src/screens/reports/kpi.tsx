import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { api } from "../../api/client";
import { HelpPopover } from "../../components/HelpPopover";
import type { components } from "../../api/schema";
import { formatAmount } from "../../format/amount";
import { month as monthName } from "../../format/date";
import type { Period } from "../accounting/data";

export type KPIs = components["schemas"]["KPIs"];
export type KPI = components["schemas"]["KPIItem"];
export type Figure = components["schemas"]["KPIFigure"];

// readKpis reads a month's KPIs (GET /v1/kpis; FR-KPI-02, FR-KPI-03): every figure as the server worked it out, never
// recomputed here (FR-UX-22, RND-DISPLAY).
export async function readKpis(month: string): Promise<KPIs> {
  const { data, error } = await api.GET("/v1/kpis", { params: { query: { month } } });
  if (error) throw error;
  return data;
}

export function useKpis(month: string | null, enabled = true) {
  return useQuery({ queryKey: ["kpis", month], enabled: enabled && month !== null, retry: false, queryFn: () => readKpis(month!) });
}

const ym = (p: Period) => p.starts_on.slice(0, 7);

// lastMonths are up to n months of the books ending with month, earliest first: what the dashboard's charts read.
export function lastMonths(periods: readonly Period[], month: string, n = 12): { from: string; to: string } | null {
  const i = periods.findIndex((p) => ym(p) === month);
  if (i < 0) return null;
  return { from: ym(periods[Math.max(0, i - n + 1)]!), to: month };
}

// figureOf is a KPI's figure as its tile shows it: the value with what follows it (the currency, months or days), the
// words that stand for it such as "Not burning", or "—" with why it has none (FR-KPI-03, never 0).
export function figureOf(k: KPI, f: Figure | undefined, currency: string): { text: string; unit?: string; why?: string } {
  if (!f) return { text: "—", why: "the books have no month before" };
  if (f.words) return { text: f.words };
  if (f.value === undefined) return { text: "—", why: f.undefined };
  const v = formatAmount(f.value);
  switch (k.unit) {
    case "amount":
      return { text: v, unit: currency };
    case "percent":
      return { text: `${v}%` };
    case "months":
      return { text: v, unit: "months" };
    case "days":
      return { text: v, unit: "days" };
    default:
      return { text: v };
  }
}

// figureText is the figure in one string, as a table cell or a sentence shows it.
export function figureText(k: KPI, f: Figure | undefined, currency: string): string {
  const x = figureOf(k, f, currency);
  return x.unit ? `${x.text} ${x.unit}` : x.text;
}

// sourceNames say where each KPI comes from, in words.
export const sourceNames: Record<string, string> = {
  ledger: "The ledger",
  entered: "Counts a person entered",
  "ledger and entered": "The ledger and counts a person entered",
  "subscription data": "Subscription data, from P2",
};

// About is a KPI's definition behind an ⓘ (MASTER.md "Help and onboarding"; FR-UX-31): it opens on click, Enter, hover
// and focus, stays while the pointer is over it, and Esc closes it. It says how the figure is worked out, the dates it
// reads, where it comes from and the definition's version (FR-KPI-03).
export function About({ k }: { k: KPI }) {
  return (
    <HelpPopover title={k.name}>
      <span className="fy-small text-foreground whitespace-normal">{k.formula}</span>
      <span className="fy-small fy-muted whitespace-normal">
        Reads {k.date_basis}. Source: {(sourceNames[k.source] ?? k.source).toLowerCase()}. Definition {k.version}.
      </span>
    </HelpPopover>
  );
}

// Tile is one KPI as a stat tile (FR-KPI-01; MASTER.md §10, a single headline is a tile, not a chart): its name and
// definition, its figure, which opens what is behind it (FR-UX-04), and the month before's figure or why it has none.
export function Tile({ k, kpis, to, className }: { k: KPI; kpis: KPIs; to?: string; className?: string }) {
  const now = figureOf(k, k.month, kpis.currency);
  const value = (
    <>
      {now.text}
      {now.unit && <> <small>{now.unit}</small></>}
    </>
  );
  const before = monthName(previous(kpis.month));
  return (
    <div className={["fy-tile", className].filter(Boolean).join(" ")} data-kpi={k.key}>
      <span className="fy-tile__label">
        {k.name}
        <About k={k} />
      </span>
      {to ? (
        <Link className="fy-tile__value fy-num text-foreground" to={to}>
          {value}
        </Link>
      ) : (
        <span className="fy-tile__value fy-num">{value}</span>
      )}
      <span className="fy-tile__meta">
        {now.why ? (
          <span className="whitespace-normal">No figure: {now.why}</span>
        ) : k.before ? (
          <span>
            {before}: {figureText(k, k.before, kpis.currency)}
          </span>
        ) : null}
        {k.month.entered && <span className="fy-badge fy-badge--quiet">Entered</span>}
      </span>
    </div>
  );
}

// previous is the month before a YYYY-MM month.
export function previous(month: string): string {
  const [y, m] = month.split("-").map(Number) as [number, number];
  return m === 1 ? `${y - 1}-12` : `${y}-${String(m - 1).padStart(2, "0")}`;
}

// find is the KPI with the key, when the API gave it.
export function find(kpis: KPIs | undefined, key: string): KPI | undefined {
  return kpis?.kpis.find((k) => k.key === key);
}
