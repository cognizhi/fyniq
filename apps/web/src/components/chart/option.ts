import { formatAmount } from "../../format/amount";
import { sum } from "../../format/decimal";

// The chart's form, chosen by the data's job (FR-VIZ-01): a line for change over time, bars to compare, horizontal bars
// to rank named things with long labels. A single headline is a stat tile, not a chart.
export type Form = "line" | "bar" | "bar-horizontal";

// Series is one entity's figures, one per category, as exact decimal strings from the API (ENG-12). Slot is the entity's
// fixed colour (FR-VIZ-03): a caller that filters series keeps each entity's slot, so a filter never repaints the rest.
export interface Series {
  key: string;
  name: string;
  values: readonly string[];
  slot?: number;
}

// Theme is what the chart reads from the design tokens where it is drawn: the eight categorical colours in their order,
// text in text colours (FR-VIZ-08), recessive axes and gridlines.
export interface Theme {
  series: readonly string[];
  text: string;
  axis: string;
  grid: string;
  surface: string;
  font: string;
}

export const MAX_SERIES = 8;

// fold keeps the first seven series and adds up the rest, exactly, as "Other" when there are more than eight (FR-VIZ-03).
export function fold(series: readonly Series[], categories: number): Series[] {
  if (series.length <= MAX_SERIES) return [...series];
  const rest = series.slice(MAX_SERIES - 1);
  const other = Array.from({ length: categories }, (_, i) => sum(rest.map((s) => s.values[i] ?? "")));
  return [...series.slice(0, MAX_SERIES - 1), { key: "other", name: "Other", values: other, slot: MAX_SERIES - 1 }];
}

// plotted is a figure as the chart plots it. ECharts places marks by number; every figure a person reads, in a
// tooltip, a label or the table view, is the exact decimal string (RND-DISPLAY).
export function plotted(value: string): number | null {
  return value === "" ? null : Number(value);
}

const escapes: Record<string, string> = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };

// escaped is text made safe for the tooltip's HTML: a customer's or account's name is data, never markup.
export function escaped(text: string): string {
  return text.replace(/[&<>"']/g, (c) => escapes[c] ?? c);
}

const compact = new Intl.NumberFormat("en-SG", { notation: "compact", maximumFractionDigits: 1 });

interface TipItem {
  seriesIndex?: number;
  dataIndex?: number;
  marker?: unknown;
}

// chartOption is the ECharts option for a chart (FR-VIZ-01 to FR-VIZ-08): one value axis; the colours in their fixed
// order by slot; a legend from two series, and the series named at the line ends up to four; values on the bars of a
// single series of at most twelve; thin marks, rounded at the data end; recessive axes; and a tooltip that shows the
// exact figures.
export function chartOption(form: Form, categories: readonly string[], series: readonly Series[], theme: Theme, animate = true) {
  const shown = fold(series, categories.length);
  const bar = form !== "line";
  const horizontal = form === "bar-horizontal";
  const legend = shown.length >= 2;
  const named = !bar && legend && shown.length <= 4;
  const valued = bar && shown.length === 1 && categories.length <= 12;
  const valueAxis = {
    type: "value" as const,
    axisLabel: { color: theme.axis, formatter: (v: number) => compact.format(v) },
    splitLine: { lineStyle: { color: theme.grid } },
  };
  const categoryAxis = {
    type: "category" as const,
    data: [...categories],
    axisLabel: { color: theme.axis },
    axisLine: { lineStyle: { color: theme.grid } },
    axisTick: { show: false },
  };
  return {
    animation: animate,
    color: shown.map((s, i) => theme.series[(s.slot ?? i) % theme.series.length] ?? theme.text),
    textStyle: { fontFamily: theme.font, color: theme.text },
    grid: { left: 8, right: named ? 112 : valued && horizontal ? 72 : 16, top: legend ? 36 : valued && !horizontal ? 24 : 12, bottom: 8, containLabel: true },
    legend: legend ? { top: 0, left: 0, icon: "roundRect", itemWidth: 12, itemHeight: 12, textStyle: { color: theme.text } } : { show: false },
    tooltip: {
      trigger: "axis" as const,
      axisPointer: { type: bar ? ("shadow" as const) : ("line" as const) },
      backgroundColor: theme.surface,
      borderColor: theme.grid,
      textStyle: { color: theme.text },
      formatter: (items: TipItem | TipItem[]) => {
        const list = Array.isArray(items) ? items : [items];
        const at = list[0]?.dataIndex ?? 0;
        const rows = list.map((p) => {
          const s = shown[p.seriesIndex ?? 0];
          const v = s?.values[p.dataIndex ?? 0] ?? "";
          return `${typeof p.marker === "string" ? p.marker : ""}${escaped(s?.name ?? "")}: <b>${v === "" ? "–" : formatAmount(v)}</b>`;
        });
        return [escaped(categories[at] ?? ""), ...rows].join("<br/>");
      },
    },
    xAxis: horizontal ? valueAxis : categoryAxis,
    yAxis: horizontal ? { ...categoryAxis, inverse: true } : valueAxis,
    series: shown.map((s) => ({
      type: bar ? ("bar" as const) : ("line" as const),
      name: s.name,
      data: s.values.map(plotted),
      ...(bar
        ? {
            barMaxWidth: 28,
            barGap: "8%",
            itemStyle: { borderRadius: horizontal ? [0, 4, 4, 0] : [4, 4, 0, 0] },
            label: valued
              ? { show: true, position: horizontal ? ("right" as const) : ("top" as const), color: theme.text, formatter: (p: { dataIndex: number }) => (s.values[p.dataIndex] ? formatAmount(s.values[p.dataIndex] ?? "") : "") }
              : { show: false },
          }
        : {
            lineStyle: { width: 2 },
            symbolSize: 8,
            showSymbol: false,
            endLabel: named ? { show: true, formatter: s.name, color: theme.text } : { show: false },
          }),
    })),
  };
}
