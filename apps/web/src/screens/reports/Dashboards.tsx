import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowDown, ArrowUp, Plus, RotateCcw, Save, Trash2 } from "lucide-react";
import { useId, useMemo, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listPeriods } from "../accounting/data";
import { find, Tile, useKpis } from "./kpi";
import { runOf } from "./Statement";

type Tiles = components["schemas"]["DashboardTile"][];

const widths = [
  { w: 4, name: "A third" },
  { w: 6, name: "Half" },
  { w: 12, name: "Full width" },
];

// layoutOf places tiles in the order given, left to right, wrapping at the twelve columns: what a person arranges is the order and the widths.
function layoutOf(order: { tile: string; w: number }[]): Tiles {
  const out: Tiles = [];
  let x = 0;
  let y = 0;
  for (const t of order) {
    if (x + t.w > 12) {
      x = 0;
      y += 1;
    }
    out.push({ tile: t.tile, x, y, w: t.w, h: 1 });
    x += t.w;
  }
  return out;
}

// Dashboards is the configurable dashboard (docs/design/screens Dashboards; FR-VIZ-09, FR-KPI-05): tiles a person adds, removes, reorders and
// resizes and saves for themselves, over the layout their role or the default gives them; each tile shows its KPI as the server worked it out,
// when it was read, and opens what is behind it. A tile shows only what the person's own rights read.
export function Dashboards() {
  const id = useId();
  const client = useQueryClient();
  const mine = useQuery({
    queryKey: ["dashboard", "mine"],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/dashboards/mine", {});
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const catalogue = useQuery({
    queryKey: ["dashboard-tiles"],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/dashboard-tiles", { params: { query: { page_size: 100 } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  const periods = useQuery({ queryKey: ["periods"], queryFn: listPeriods, retry: false });
  const month = useMemo(() => runOf("month", periods.data?.items ?? [], { from: "", to: "" })?.to ?? null, [periods.data]);
  const kpis = useKpis(month);
  const [order, setOrder] = useState<{ tile: string; w: number }[] | null>(null);
  const [adding, setAdding] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("reports.dashboards");
  const shown = order ?? (mine.data?.tiles ?? []).map((t) => ({ tile: t.tile, w: t.w }));
  const names = new Map((catalogue.data ?? []).map((t) => [t.tile, t.name]));
  const move = (i: number, by: number) => {
    const next = [...shown];
    const [t] = next.splice(i, 1);
    next.splice(Math.min(Math.max(i + by, 0), next.length), 0, t!);
    setOrder(next);
  };
  const save = async (tiles: Tiles) => {
    const { error } = await api.PUT("/v1/dashboards/mine", { params: { header: { "If-Match": `"${mine.data?.version ?? 0}"` } }, body: { tiles } });
    if (error) return setProblem(problemOf(error));
    setProblem(null);
    setOrder(null);
    await client.invalidateQueries({ queryKey: ["dashboard"] });
  };

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/reports">Reports</Link>
          </nav>
          <h1 className="fy-h1">Dashboard</h1>
          <p className="fy-secondary">
            {h.tooltip}
            {mine.data && ` This is ${mine.data.source === "user" ? "your own arrangement" : mine.data.source === "role" ? `the arrangement set for ${mine.data.role}` : "the default"}.`}
          </p>
        </div>
        <div className="fy-pagehead__actions">
          <Button onClick={() => save(layoutOf(shown))} disabled={order === null}>
            <Icon of={Save} /> Save my dashboard
          </Button>
          <Button variant="secondary" onClick={() => save([])} disabled={mine.data?.source !== "user"}>
            <Icon of={RotateCcw} /> Use the default
          </Button>
        </div>
      </div>
      {mine.error && <Refusal problem={problemOf(mine.error)} title="The dashboard could not be shown" />}
      {problem && <Refusal problem={problem} title="The dashboard could not be saved" />}
      <div className="fy-card fy-card--pad fy-row fy-row--wrap gap-3">
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-add`}>
            Add a tile
          </label>
          <select className="fy-select" id={`${id}-add`} value={adding} onChange={(e) => setAdding(e.target.value)}>
            <option value="">Choose a KPI</option>
            {(catalogue.data ?? [])
              .filter((t) => !shown.some((s) => s.tile === t.tile))
              .map((t) => (
                <option key={t.tile} value={t.tile}>
                  {t.name}
                </option>
              ))}
          </select>
        </div>
        <Button variant="secondary" disabled={adding === "" || shown.length >= 24} onClick={() => { setOrder([...shown, { tile: adding, w: 4 }]); setAdding(""); }}>
          <Icon of={Plus} /> Add it
        </Button>
      </div>
      {kpis.data && (
        <p className="fy-small fy-muted">
          Read at {dateTime(mine.data?.generated_at ?? "")}, for the KPIs' month {kpis.data.month} at ledger revision {kpis.data.ledger_revision}.
        </p>
      )}
      {shown.length === 0 && !mine.isPending && <p className="fy-secondary">No tile to show: you may not read the KPIs, or none is placed.</p>}
      <ul className="fy-grid fy-grid--3 fy-list--bare" aria-label="Dashboard tiles">
        {shown.map((t, i) => {
          const k = kpis.data ? find(kpis.data, t.tile) : undefined;
          return (
            <li key={t.tile} style={{ gridColumn: `span ${Math.max(1, Math.round(t.w / 4))}` }}>
              {k && kpis.data ? <Tile k={k} kpis={kpis.data} to="/reports/kpis" /> : <div className="fy-tile">{names.get(t.tile) ?? t.tile}</div>}
              <div className="fy-row gap-2">
                <Button size="sm" variant="ghost" aria-label={`Move ${names.get(t.tile) ?? t.tile} earlier`} onClick={() => move(i, -1)} disabled={i === 0}>
                  <Icon of={ArrowUp} />
                </Button>
                <Button size="sm" variant="ghost" aria-label={`Move ${names.get(t.tile) ?? t.tile} later`} onClick={() => move(i, 1)} disabled={i === shown.length - 1}>
                  <Icon of={ArrowDown} />
                </Button>
                <select
                  className="fy-select"
                  aria-label={`Width of ${names.get(t.tile) ?? t.tile}`}
                  value={t.w}
                  onChange={(e) => setOrder(shown.map((s, j) => (j === i ? { ...s, w: Number(e.target.value) } : s)))}
                >
                  {widths.map((w) => (
                    <option key={w.w} value={w.w}>
                      {w.name}
                    </option>
                  ))}
                </select>
                <Button size="sm" variant="ghost" aria-label={`Remove ${names.get(t.tile) ?? t.tile}`} onClick={() => setOrder(shown.filter((_, j) => j !== i))}>
                  <Icon of={Trash2} />
                </Button>
              </div>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
