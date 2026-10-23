import { useQuery, useQueryClient } from "@tanstack/react-query";
import { FileText, Lock, Paperclip, Trash2 } from "lucide-react";
import { useId, useRef, useState } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Evidence = components["schemas"]["Evidence"] | components["schemas"]["BillEvidence"];
type DataClass = "general" | "personal" | "bank";

// Who attaches evidence to a journal: C on "Evidence: add, supersede" and a view of the journal (Appendix D): the Owner,
// the Controller, the Accountant and the Tax agent. The API decides.
export const attachers = ["owner", "controller", "accountant", "tax_agent"];

// Who attaches evidence to a bill: C on "Evidence: add, supersede" and a view of bills (Appendix D). The API decides.
export const billAttachers = ["owner", "controller", "accountant", "bookkeeper", "ap_specialist", "approver", "tax_agent"];

// A record evidence goes on: a journal, or a bill or vendor credit (FR-DOC-05), at its ETag.
export interface EvidenceRecord {
  kind: "journal" | "bill";
  id: string;
  etag: string;
}

// The kinds of file each record usually rests on; any other name is accepted.
const kindsOf = {
  journal: ["calculation", "invoice", "receipt", "statement", "agreement", "board resolution", "screenshot"],
  bill: ["invoice", "receipt", "quotation", "statement", "agreement", "screenshot"],
};

// Result is an evidence call's answer, whichever record it went to.
type Result = { data?: Evidence; error?: unknown };

// files are the record's evidence, oldest first.
async function files(r: EvidenceRecord): Promise<Evidence[]> {
  const out =
    r.kind === "bill"
      ? await api.GET("/v1/bills/{bill_id}/evidence", { params: { path: { bill_id: r.id }, query: { page_size: 100 } } })
      : await api.GET("/v1/journals/{journal_id}/evidence", { params: { path: { journal_id: r.id }, query: { page_size: 100 } } });
  if (out.error) throw out.error;
  return out.data.items ?? [];
}

const classes: [DataClass, string][] = [
  ["general", "No personal data"],
  ["personal", "Personal data, such as a name with an address or phone number"],
  ["bank", "Bank details"],
];

// sizeOf shows a file's size in KB or MB.
function sizeOf(bytes: number): string {
  if (bytes < 1024 * 1024) return `${Math.max(1, Math.round(bytes / 1024))} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function State({ e }: { e: Evidence }) {
  if (e.state === "final") {
    return (
      <span className="fy-chip fy-chip--neutral" title={e.retain_until ? `Kept, and never deleted, until ${date(e.retain_until)}` : undefined}>
        <Icon of={Lock} />
        Kept
      </span>
    );
  }
  if (e.state === "removed") return <span className="fy-chip fy-chip--neutral">Removed</span>;
  return <span className="fy-chip fy-chip--good">Attached</span>;
}

// JournalEvidence is a journal's evidence panel (docs/design/screens JournalEditor "Evidence").
export function JournalEvidence({ journalId, journalEtag, draft, mayAttach, onChanged }: { journalId: string; journalEtag: string; draft: boolean; mayAttach: boolean; onChanged: (message: string) => void }) {
  return <EvidencePanel record={{ kind: "journal", id: journalId, etag: journalEtag }} draft={draft} mayAttach={mayAttach} onChanged={onChanged} />;
}

// EvidencePanel is a record's evidence (FR-DOC-05, FR-DOC-07, FR-DOC-08, FR-DOC-11): the files attached to it, oldest
// first. Those who attach evidence add a file with what it is; on a draft a file comes off again, and once the record
// is posted its files are kept, append-only, until the retention ends.
export function EvidencePanel({ record, draft, mayAttach, onChanged }: { record: EvidenceRecord; draft: boolean; mayAttach: boolean; onChanged: (message: string) => void }) {
  const noun = record.kind === "bill" ? "bill" : "journal";
  const kinds = kindsOf[record.kind];
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const input = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [kind, setKind] = useState("");
  const [note, setNote] = useState("");
  const [dataClass, setDataClass] = useState<DataClass>("general");
  const [missing, setMissing] = useState<{ file?: boolean; kind?: boolean }>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [failed, setFailed] = useState("");
  const attach = helpText(`${noun}.attach-evidence`);
  const q = useQuery({ queryKey: [`${noun}-evidence`, record.id], queryFn: () => files(record) });
  const items = q.data ?? [];
  const refresh = () => queries.invalidateQueries({ queryKey: [`${noun}-evidence`, record.id] });

  const add = async () => {
    const lack = { file: !file, kind: !kind.trim() };
    setMissing(lack);
    if (!file || lack.kind) return;
    setBusy(true);
    setProblem(null);
    const key = newKey();
    // The generated body names the form's fields; the request sends them as multipart/form-data with the file itself.
    const body = { file: file.name, evidence_type: kind.trim(), note: note.trim() || undefined, data_class: dataClass, source: "upload" as const };
    const bodySerializer = () => {
      const form = new FormData();
      form.append("file", file, file.name);
      form.append("evidence_type", kind.trim());
      if (note.trim()) form.append("note", note.trim());
      form.append("data_class", dataClass);
      form.append("source", "upload");
      return form;
    };
    const header = { "If-Match": record.etag, "Idempotency-Key": key };
    const post = (): Promise<Result> =>
      record.kind === "bill"
        ? api.POST("/v1/bills/{bill_id}/evidence", { params: { path: { bill_id: record.id }, header }, body, bodySerializer })
        : api.POST("/v1/journals/{journal_id}/evidence", { params: { path: { journal_id: record.id }, header }, body, bodySerializer });
    const out = await guarded(confirm, post);
    setBusy(false);
    setFailed("The file was not attached");
    if (!out.data) return setProblem(problemOf(out.error));
    setFile(null);
    setKind("");
    setNote("");
    setDataClass("general");
    if (input.current) input.current.value = "";
    await refresh();
    onChanged(draft ? `Attached ${out.data.file_name}. It can come off while the ${noun} is a draft.` : `Attached ${out.data.file_name}. It is kept with the posted ${noun}.`);
  };

  const remove = async (e: Evidence) => {
    setBusy(true);
    setProblem(null);
    const read: Result & { response: Response } =
      record.kind === "bill"
        ? await api.GET("/v1/bills/{bill_id}/evidence/{evidence_id}", { params: { path: { bill_id: record.id, evidence_id: e.id } } })
        : await api.GET("/v1/journals/{journal_id}/evidence/{evidence_id}", { params: { path: { journal_id: record.id, evidence_id: e.id } } });
    const header = { "If-Match": etagOf(read.response) };
    const del = (): Promise<Result> =>
      record.kind === "bill"
        ? api.DELETE("/v1/bills/{bill_id}/evidence/{evidence_id}", { params: { path: { bill_id: record.id, evidence_id: e.id }, header } })
        : api.DELETE("/v1/journals/{journal_id}/evidence/{evidence_id}", { params: { path: { journal_id: record.id, evidence_id: e.id }, header } });
    const out = read.error ? read : await guarded(confirm, del);
    setBusy(false);
    setFailed("The file was not removed");
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh();
    onChanged(`Removed ${e.file_name}.`);
  };

  return (
    <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-evidence`}>
      <h2 className="fy-h3" id={`${id}-evidence`}>
        Evidence
      </h2>
      <p className="fy-small fy-muted">
        {record.kind === "bill"
          ? draft
            ? "The supplier's invoice, and any other file the bill rests on. While it is a draft, a file can come off again."
            : "The supplier's invoice, and any other file the bill rests on. Once it is posted they are kept, and never deleted, for as long as the records are."
          : draft
            ? "Files that support the journal, such as a calculation or an invoice. While it is a draft, a file can come off again."
            : "Files that support the journal. Once it is posted they are kept, and never deleted, for as long as the records are."}
      </p>
      {q.isError && <Refusal problem={problemOf(q.error)} title="The evidence could not be shown" />}
      {problem && <Refusal problem={problem} title={failed} />}
      {q.data && items.length === 0 && <p className="fy-small">No files are attached.</p>}
      {items.length > 0 && (
        <ul className="fy-list" aria-label="Files attached">
          {items.map((e) => (
            <li className="fy-list__item" key={e.id}>
              <span className="fy-list__icon">
                <Icon of={FileText} size={20} />
              </span>
              <span className="fy-list__main">
                <span className={`fy-list__title whitespace-normal${e.state === "removed" ? " fy-muted" : ""}`}>{e.file_name}</span>
                <span className="fy-list__meta whitespace-normal">
                  {[e.evidence_type, sizeOf(e.size_bytes), `added ${dateTime(e.uploaded_at)}`, e.note].filter(Boolean).join(" · ")}
                </span>
              </span>
              <State e={e} />
              {draft && mayAttach && e.state === "attached" && (
                <Button variant="ghost" size="sm" onClick={() => void remove(e)} disabled={busy} aria-label={`Remove ${e.file_name}`} title="Take the file off the draft">
                  <Icon of={Trash2} />
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      {mayAttach && (
        <div className="fy-stack fy-stack--3">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-file`}>
              File
            </label>
            <input
              ref={input}
              className="fy-input"
              id={`${id}-file`}
              type="file"
              accept=".pdf,.png,.jpg,.jpeg,.webp,.txt,.csv,.xlsx,.docx,.eml,.msg"
              onChange={(e) => {
                setFile(e.target.files?.[0] ?? null);
                setMissing({ ...missing, file: false });
              }}
              aria-invalid={missing.file || undefined}
              aria-describedby={`${id}-file-help${missing.file ? ` ${id}-file-error` : ""}`}
            />
            <span className="fy-help" id={`${id}-file-help`}>
              PDF, image, CSV, text, XLSX, DOCX or email, up to 25 MB. Fyniq scans it and strips image metadata such as location.
            </span>
            {missing.file && (
              <span className="fy-error" id={`${id}-file-error`}>
                Choose the file to attach.
              </span>
            )}
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-kind`}>
              What it is
            </label>
            <input
              className="fy-input"
              id={`${id}-kind`}
              list={`${id}-kinds`}
              value={kind}
              maxLength={100}
              onChange={(e) => {
                setKind(e.target.value);
                setMissing({ ...missing, kind: false });
              }}
              aria-invalid={missing.kind || undefined}
              aria-describedby={missing.kind ? `${id}-kind-error` : undefined}
            />
            <datalist id={`${id}-kinds`}>
              {kinds.map((k) => (
                <option key={k} value={k} />
              ))}
            </datalist>
            {missing.kind && (
              <span className="fy-error" id={`${id}-kind-error`}>
                {record.kind === "bill" ? "Say what the file is, such as the invoice or a receipt." : "Say what the file is, such as a calculation or an invoice."}
              </span>
            )}
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-note`}>
              Note (optional)
            </label>
            <input className="fy-input" id={`${id}-note`} value={note} maxLength={1000} onChange={(e) => setNote(e.target.value)} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-class`}>
              Personal data in it
            </label>
            <select className="fy-select" id={`${id}-class`} value={dataClass} onChange={(e) => setDataClass(e.target.value as DataClass)}>
              {classes.map(([v, label]) => (
                <option key={v} value={v}>
                  {label}
                </option>
              ))}
            </select>
          </div>
          <Button className="self-start" onClick={() => void add()} disabled={busy} title={attach.tooltip}>
            <Icon of={Paperclip} />
            {attach.label}
          </Button>
        </div>
      )}
    </section>
  );
}
