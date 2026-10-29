import { Download, FileDown } from "lucide-react";
import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { useAppearance, useMotion } from "../../appearance";
import { formatAmount } from "../../format/amount";
import { csvCell } from "../../format/csv";
import { Icon } from "../../ui";
import { Button } from "../ui/button";
import { chartOption, fold, type Form, type Series, type Theme } from "./option";

type Instance = ReturnType<Awaited<typeof import("./echarts")>["init"]>;

// themeOf reads the chart's colours from the design tokens where it is drawn, so a theme or contrast change redraws it
// in that theme's own steps (FR-VIZ-07's validated palette).
function themeOf(el: HTMLElement): Theme {
  const css = getComputedStyle(el);
  const v = (name: string) => css.getPropertyValue(name).trim();
  return {
    series: Array.from({ length: 8 }, (_, i) => v(`--chart-${i + 1}`)),
    text: v("--text-primary"),
    axis: v("--text-secondary"),
    grid: v("--border"),
    surface: v("--surface-1"),
    font: css.fontFamily,
  };
}

function download(name: string, blob: Blob) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}

// Chart is a chart that follows the dataviz method (MASTER.md §10; FR-VIZ-01 to FR-VIZ-08): its form chosen by the data's
// job, one value axis, the categorical colours in fixed order with more than eight series folded into Other, a legend
// from two series and direct labels up to four, a tooltip that the arrow keys also move, a table view of the exact
// figures, and PNG and CSV downloads. ECharts loads only when a chart is first shown. The summary says in words what the
// chart shows, for every reader. Status colours are never series colours: a state is a chip with its icon and label.
export function Chart({
  title,
  summary,
  form,
  label,
  categories,
  series,
  unit,
  file,
  height = 280,
  level = 2,
}: {
  title: string;
  summary: string;
  form: Form;
  label: string; // the categories' heading in the table view, such as "Month"
  categories: readonly string[];
  series: readonly Series[];
  unit: string; // the figures' currency or unit, such as "SGD"
  file: string; // the downloads' base name
  height?: number;
  level?: 2 | 3; // the heading's level where the chart sits: 2 under a page's title, 3 inside a section
}) {
  const id = useId();
  const [view, setView] = useState<"chart" | "table">("chart");
  const plot = useRef<HTMLDivElement>(null);
  const chart = useRef<Instance | null>(null);
  const [at, setAt] = useState(-1);
  const appearance = useAppearance();
  const motion = useMotion();
  const shown = useMemo(() => fold(series, categories.length), [series, categories]);
  const data = JSON.stringify([form, categories, series]);
  const Heading = level === 2 ? "h2" : "h3";

  useEffect(() => {
    const el = plot.current;
    if (view !== "chart" || !el) return;
    let gone = false;
    let stop = () => {};
    void import("./echarts").then(({ init }) => {
      if (gone) return;
      const instance = init(el, undefined, { renderer: "canvas" });
      chart.current = instance;
      instance.setOption(chartOption(form, categories, series, themeOf(el), motion === "on"));
      const resized = new ResizeObserver(() => instance.resize());
      resized.observe(el);
      stop = () => {
        resized.disconnect();
        instance.dispose();
        chart.current = null;
      };
    });
    return () => {
      gone = true;
      stop();
    };
    // data stands for form, categories and series; the appearance redraws it in the theme's colours.
  }, [view, data, appearance, motion]);

  // The arrow keys move the tooltip from category to category, and Escape hides it: the tooltip is never pointer-only.
  const keys = (e: KeyboardEvent<HTMLDivElement>) => {
    const c = chart.current;
    if (!c || categories.length === 0) return;
    if (e.key === "Escape") {
      c.dispatchAction({ type: "hideTip" });
      return setAt(-1);
    }
    const step = e.key === "ArrowRight" || e.key === "ArrowDown" ? 1 : e.key === "ArrowLeft" || e.key === "ArrowUp" ? -1 : 0;
    if (!step) return;
    e.preventDefault();
    const next = Math.min(categories.length - 1, Math.max(0, at + step));
    setAt(next);
    c.dispatchAction({ type: "showTip", seriesIndex: 0, dataIndex: next });
  };

  const png = () => {
    const c = chart.current;
    if (!c || !plot.current) return;
    // The data URL is decoded here, not fetched: the content security policy lets the app fetch nothing but its API.
    const url = c.getDataURL({ type: "png", pixelRatio: 2, backgroundColor: themeOf(plot.current).surface });
    const bytes = Uint8Array.from(atob(url.slice(url.indexOf(",") + 1)), (ch) => ch.charCodeAt(0));
    download(`${file}.png`, new Blob([bytes], { type: "image/png" }));
  };
  const csv = () => {
    const head = [label, ...shown.map((s) => (shown.length === 1 ? `${s.name} ${unit}` : s.name))];
    const rows = categories.map((c, i) => [csvCell(c), ...shown.map((s) => csvCell(s.values[i] ?? "", true))].join(","));
    download(`${file}.csv`, new Blob([[head.map((h) => csvCell(h)).join(","), ...rows].join("\r\n") + "\r\n"], { type: "text/csv;charset=utf-8" }));
  };

  return (
    <figure className="fy-card fy-card--pad fy-stack fy-stack--3 m-0" aria-labelledby={`${id}-title`}>
      <div className="fy-row fy-row--wrap fy-row--between gap-2">
        <Heading className="fy-h3" id={`${id}-title`}>
          {title}
        </Heading>
        <div className="fy-row gap-2">
          <div className="fy-segmented" role="radiogroup" aria-label={`Show ${title} as`}>
            {(["chart", "table"] as const).map((v) => (
              <button key={v} type="button" role="radio" aria-checked={view === v} onClick={() => setView(v)}>
                {v === "chart" ? "Chart" : "Table"}
              </button>
            ))}
          </div>
          <Button size="sm" variant="ghost" onClick={png} disabled={view !== "chart"} aria-label={`Download ${title} as PNG`} title="Download as PNG">
            <Icon of={Download} />
            PNG
          </Button>
          <Button size="sm" variant="ghost" onClick={csv} aria-label={`Download ${title}'s data as CSV`} title="Download the data as CSV">
            <Icon of={FileDown} />
            CSV
          </Button>
        </div>
      </div>
      {view === "chart" ? (
        <div
          ref={plot}
          role="img"
          tabIndex={0}
          aria-label={`${title}. ${summary} Use the arrow keys to read each figure, or show the table.`}
          className="w-full rounded-md focus-visible:outline-2"
          style={{ height }}
          onKeyDown={keys}
          data-testid="chart-plot"
        />
      ) : (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--compact">
            <caption className="fy-sr">{title}</caption>
            <thead>
              <tr>
                <th scope="col">{label}</th>
                {shown.map((s) => (
                  <th key={s.key} scope="col" className="fy-num">
                    {shown.length === 1 ? `${s.name} ${unit}` : s.name}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {categories.map((c, i) => (
                <tr key={`${c}-${i}`}>
                  <th scope="row">{c}</th>
                  {shown.map((s) => (
                    <td key={s.key} className="fy-num">
                      {s.values[i] ? formatAmount(s.values[i] ?? "") : "–"}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <figcaption className="fy-small fy-secondary">
        {summary}
        {shown.length > 1 && unit ? ` Figures in ${unit}.` : ""}
      </figcaption>
    </figure>
  );
}
