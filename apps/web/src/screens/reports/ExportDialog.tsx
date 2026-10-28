import { Download, FileDown, FileSpreadsheet, FileText, type LucideIcon } from "lucide-react";
import { useId, useState, type KeyboardEvent } from "react";
import { api } from "../../api/client";
import type { operations } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { Icon, problemOf } from "../../ui";
import { MyExports, save } from "./AccountantExports";

type Query = NonNullable<operations["exportFinancialStatement"]["parameters"]["query"]>;
export type Format = NonNullable<Query["format"]>;

const formats: { format: Format; name: string; about: string; icon: LucideIcon }[] = [
  { format: "xlsx", name: "Excel (XLSX)", about: "Formatted, with a data tab of every figure", icon: FileSpreadsheet },
  { format: "pdf", name: "PDF", about: "To print or send, with page numbers and the ledger revision on every page", icon: FileText },
  { format: "csv", name: "CSV", about: "The rows as plain numbers, for another tool", icon: FileDown },
];

const short: Record<Format, string> = { xlsx: "Excel", pdf: "PDF", csv: "CSV" };

// ExportDialog exports a statement as on screen (docs/design/screens ExportDialog; FR-RPT-05, FR-RPT-09): as Excel, PDF
// or CSV, with the period, comparison and columns shown, read at the ledger revision shown. The file downloads at once
// and the export is recorded in the audit trail; the person's background exports are listed beneath with their states.
// The build leaves out, for their tasks: another period or columns than the screen's, the audit-tool layouts and the
// lines behind each figure (FR-RPT-10's formats), and the organisation's logo (FR-ORG-09).
export function ExportDialog({
  open,
  onOpenChange,
  kind,
  name,
  query,
  shown,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  kind: "pl" | "bs" | "cf" | "tb";
  name: string;
  query: Omit<Query, "format">;
  shown: string; // what is on screen, such as "1 Oct 2026 to 31 Oct 2026, with the period before"
}) {
  const id = useId();
  const [format, setFormat] = useState<Format>("xlsx");
  const [exporting, setExporting] = useState(false);
  const [problem, setProblem] = useState<ReturnType<typeof problemOf> | null>(null);
  const move = (e: KeyboardEvent<HTMLButtonElement>, f: Format) => {
    const step = e.key === "ArrowRight" || e.key === "ArrowDown" ? 1 : e.key === "ArrowLeft" || e.key === "ArrowUp" ? -1 : 0;
    if (!step) return;
    e.preventDefault();
    const all = formats.map((x) => x.format);
    const next = all[(all.indexOf(f) + step + all.length) % all.length] ?? f;
    setFormat(next);
    (e.currentTarget.parentElement?.querySelector(`[data-format="${next}"]`) as HTMLElement | null)?.focus();
  };
  // run downloads the statement as the server wrote it, at the revision shown, and audited (FR-RPT-05, FR-RPT-09).
  const run = async () => {
    setExporting(true);
    setProblem(null);
    const r = await api.GET("/v1/reports/statements/{kind}/export", { params: { path: { kind }, query: { ...query, format } } });
    setExporting(false);
    if (r.error) return setProblem(problemOf(r.error));
    save(r.data.file_name, r.data.media_type, r.data.content_base64);
    onOpenChange(false);
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => (setProblem(null), onOpenChange(o))}
      title={`Export the ${name.toLowerCase()}`}
      description={`${shown} · ledger revision ${query.as_at_revision ?? ""}`}
      footer={
        <>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant="primary" disabled={exporting} onClick={run}>
            <Icon of={Download} />
            {exporting ? "Exporting…" : `Export ${short[format]}`}
          </Button>
        </>
      }
    >
      <fieldset className="fy-fieldset">
        <legend id={`${id}-format`}>Format</legend>
        <div className="fy-grid fy-grid--2 gap-2" role="radiogroup" aria-labelledby={`${id}-format`}>
          {formats.map((f) => (
            <button
              key={f.format}
              type="button"
              role="radio"
              data-format={f.format}
              className="fy-choice"
              aria-checked={format === f.format}
              tabIndex={format === f.format ? 0 : -1}
              onClick={() => setFormat(f.format)}
              onKeyDown={(e) => move(e, f.format)}
            >
              <span className="fy-radio" aria-hidden="true" />
              <span className="fy-stack gap-0.5">
                <span className="fy-row gap-1.5 fy-strong">
                  <Icon of={f.icon} />
                  {f.name}
                </span>
                <span className="fy-small fy-secondary">{f.about}</span>
              </span>
            </button>
          ))}
        </div>
      </fieldset>
      <p className="fy-small fy-secondary">
        Exports what is on screen: {shown}, read at ledger revision {query.as_at_revision}, so the figures match what you see.
      </p>
      {problem && <Refusal problem={problem} title={`The ${name.toLowerCase()} could not be exported`} />}
      <p className="fy-small fy-muted">
        Amounts go out as plain numbers with an ASCII minus. Any text that starts with =, +, − or @ stays text, so nothing can turn into a formula. This export
        is recorded in the audit trail: who, what, the filters and the rows.
      </p>
      <MyExports limit={3} title="Your recent exports" />
    </Dialog>
  );
}
