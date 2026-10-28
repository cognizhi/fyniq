import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, CircleX, Download, Hourglass, TimerOff } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { dateTime, timeZone } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf } from "../../ui";

type Export = components["schemas"]["ReportExportOut"];
type Report = components["schemas"]["ReportExportRequest"]["report"];

// reports are the accountant exports (FR-RPT-10), each over a range of days or as at a day.
const reports: { report: Report; name: string; range: boolean }[] = [
  { report: "general_ledger", name: "General ledger", range: true },
  { report: "trial_balance", name: "Trial balance", range: false },
  { report: "journals", name: "Journal entries", range: true },
  { report: "fixed_assets", name: "Fixed asset schedule", range: false },
  { report: "receivables", name: "Receivables in detail", range: false },
  { report: "payables", name: "Payables in detail", range: false },
];

const nameOf = (r: string) => reports.find((x) => x.report === r)?.name ?? r;

function today(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
}

// save gives the person a file the server wrote, in base64.
function save(name: string, type: string, base64: string) {
  const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
  const url = URL.createObjectURL(new Blob([bytes], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}

function State({ x }: { x: Export }) {
  switch (x.status) {
    case "ready":
      return (
        <span className="fy-chip fy-chip--good">
          <Icon of={CircleCheck} />
          Ready until {dateTime(x.expires_at)}
        </span>
      );
    case "failed":
      return (
        <span className="fy-chip fy-chip--critical" title={x.why}>
          <Icon of={CircleX} />
          Could not be made
        </span>
      );
    case "expired":
      return (
        <span className="fy-chip fy-chip--neutral">
          <Icon of={TimerOff} />
          Expired
        </span>
      );
    default:
      return (
        <span className="fy-chip fy-chip--neutral">
          <Icon of={Hourglass} />
          Being prepared
        </span>
      );
  }
}

// AccountantExports is the reports index's accountant exports (docs/design/screens ExportDialog; FR-RPT-09, FR-RPT-10):
// ask for the general ledger, the trial balance, the journal entries, the fixed asset schedule or the receivables or
// payables in detail as Excel or CSV; the worker prepares it, and it downloads for 24 hours, by the person who asked.
export function AccountantExports() {
  const id = useId();
  const queries = useQueryClient();
  const first = `${today().slice(0, 8)}01`;
  const [report, setReport] = useState<Report>("general_ledger");
  const [from, setFrom] = useState(first);
  const [to, setTo] = useState(today);
  const [format, setFormat] = useState<"xlsx" | "csv">("xlsx");
  const [problem, setProblem] = useState<ReturnType<typeof problemOf> | null>(null);
  const range = reports.find((r) => r.report === report)?.range ?? false;
  const mine = useQuery({
    queryKey: ["report-exports"],
    queryFn: async () => {
      const r = await api.GET("/v1/report-exports");
      if (r.error) throw r.error;
      return r.data.items;
    },
    refetchInterval: (q) => ((q.state.data ?? []).some((x) => x.status === "pending") ? 3000 : false),
  });
  const ask = useMutation({
    mutationFn: async () => {
      const r = await api.POST("/v1/report-exports", { body: { report, to, format, ...(range ? { from } : {}) } });
      if (r.error) throw r.error;
      return r.data;
    },
    onMutate: () => setProblem(null),
    onError: (e) => setProblem(problemOf(e)),
    onSuccess: () => queries.invalidateQueries({ queryKey: ["report-exports"] }),
  });
  const download = async (x: Export) => {
    setProblem(null);
    const r = await api.GET("/v1/report-exports/{export_id}/file", { params: { path: { export_id: x.id } } });
    if (r.error) return setProblem(problemOf(r.error));
    save(r.data.file_name, r.data.media_type, r.data.content_base64);
  };
  const h = helpText("reports.accountant-exports");

  return (
    <section className="fy-card fy-card--pad fy-stack fy-stack--3 fy-expert-only" aria-labelledby={`${id}-title`}>
      <div className="fy-stack fy-stack--1">
        <h2 className="fy-h3" id={`${id}-title`}>
          {h.label}
        </h2>
        <p className="fy-small fy-secondary">{h.tooltip}</p>
      </div>
      <div className="fy-row fy-row--wrap gap-3">
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-report`}>
            Report
          </label>
          <select className="fy-select" id={`${id}-report`} value={report} onChange={(e) => setReport(e.target.value as Report)}>
            {reports.map((r) => (
              <option key={r.report} value={r.report}>
                {r.name}
              </option>
            ))}
          </select>
        </div>
        {range && (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-from`}>
              From
            </label>
            <input className="fy-input" id={`${id}-from`} type="date" value={from} onChange={(e) => setFrom(e.target.value)} />
          </div>
        )}
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-to`}>
            {range ? "To" : "As at"}
          </label>
          <input className="fy-input" id={`${id}-to`} type="date" value={to} onChange={(e) => setTo(e.target.value)} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-format`}>
            Format
          </label>
          <select className="fy-select" id={`${id}-format`} value={format} onChange={(e) => setFormat(e.target.value as "xlsx" | "csv")}>
            <option value="xlsx">Excel</option>
            <option value="csv">CSV</option>
          </select>
        </div>
        <Button variant="primary" className="self-end" disabled={ask.isPending} onClick={() => ask.mutate()}>
          Prepare the export
        </Button>
      </div>
      {problem && <Refusal problem={problem} title="The export could not be asked for or downloaded" />}
      {(mine.data ?? []).length > 0 && (
        <ul className="fy-list" aria-label="My exports">
          {(mine.data ?? []).map((x) => (
            <li key={x.id} className="fy-list__item">
              <span className="fy-list__main">
                <span className="fy-list__title">
                  {nameOf(x.report)}, {x.from ? `${x.from} to ${x.to}` : `at ${x.to}`} ({x.format === "xlsx" ? "Excel" : "CSV"})
                </span>
                <span className="fy-list__meta">
                  <State x={x} />
                  {x.status === "failed" && x.why ? ` ${x.why}` : ""}
                </span>
              </span>
              {x.status === "ready" && (
                <Button size="sm" onClick={() => download(x)} aria-label={`Download ${x.file_name ?? nameOf(x.report)}`}>
                  <Icon of={Download} />
                  Download
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      <p className="fy-small fy-muted">Each export is prepared in the background, downloads for 24 hours by the person who asked for it, and is recorded in the audit trail.</p>
    </section>
  );
}
