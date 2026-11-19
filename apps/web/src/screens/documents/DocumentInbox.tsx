import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Ban, CircleCheck, Clock, Copy, Download, FileText, Mail, Sparkles, TriangleAlert, Upload } from "lucide-react";
import { useId, useState, type ClipboardEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listAccounts } from "../accounting/data";

type Capture = components["schemas"]["DocumentCapture"];
type Reading = NonNullable<Capture["reading"]>;
type ReadingLine = NonNullable<Reading["lines"]>[number];

// Who captures, reads and drafts: those who may enter bills (Appendix D). The API decides; the page offers the actions to them only.
const preparers = ["bookkeeper", "ap_specialist", "accountant"];

const filters: [string, string][] = [
  ["", "All"],
  ["received", "Waiting"],
  ["read", "Read"],
  ["failed", "Failed"],
  ["drafted", "Drafted"],
  ["duplicate", "Duplicates"],
];

const statusWords: Record<string, string> = {
  received: "Waiting to be read",
  reading: "Being read",
  read: "Read: check it",
  duplicate: "Duplicate",
  drafted: "Drafted",
  dismissed: "Dismissed",
  failed: "Could not be read",
};

const text = (t: { value?: string | null } | undefined) => t?.value ?? "";

// ConfidenceChip says how sure the model was of a field, and says it in words: a field below 80% is flagged for the person's eye.
function ConfidenceChip({ of, low }: { of?: number; low: boolean }) {
  if (of === undefined) return null;
  return low ? (
    <span className="fy-chip fy-chip--warning">
      <Icon of={TriangleAlert} />
      {Math.round(of * 100)}% sure: check it
    </span>
  ) : (
    <span className="fy-chip fy-chip--neutral">{Math.round(of * 100)}% sure</span>
  );
}

// The Reading card is the model's reading of the document, apart from the file and labelled so (FR-DOC-14): data to check against the
// document, in the AI's violet. It never decides a figure, a GST eligibility or a posting.
function ReadingCard({ c }: { c: Capture }) {
  const id = useId();
  const r = c.reading;
  if (!r) return null;
  const low = new Set(c.low_confidence ?? []);
  const field = (label: string, name: string, value: string) => (
    <div className="fy-row fy-row--between gap-2" key={name}>
      <dt className="fy-small fy-muted">{label}</dt>
      <dd className="fy-row gap-2">
        <span className="fy-mono">{value || "not shown"}</span>
        {value ? <ConfidenceChip of={c.confidence?.[name]} low={low.has(name)} /> : null}
      </dd>
    </div>
  );
  return (
    <section className="fy-ai-card" aria-labelledby={`${id}-h`}>
      <div className="fy-ai-card__head">
        <span className="fy-ai-badge">
          <Icon of={Sparkles} />
          AI
        </span>
        <h3 className="fy-h3" id={`${id}-h`}>
          What the model read
        </h3>
      </div>
      <div className="fy-ai-card__body fy-stack fy-stack--3">
        <p className="fy-small fy-muted">The model's reading, to check against the document. It is not a figure Fyniq decided, and nothing is drafted until you do it.</p>
        <dl className="fy-stack fy-stack--2">
          {field("Supplier", "supplier", text(r.supplier))}
          {field("Number", "number", text(r.number))}
          {field("Date", "date", text(r.date))}
          {field("Due", "due_date", text(r.due_date))}
          {field("Currency", "currency", text(r.currency))}
          {field("GST total", "gst_total", text(r.gst_total))}
          {field("Total", "total", text(r.total))}
          {field("GST registration", "gst_registration_no", text(r.gst_registration_no))}
        </dl>
        {(r.lines ?? []).length > 0 && (
          <table className="fy-table" aria-label="Lines the model read">
            <thead>
              <tr>
                <th scope="col">Line</th>
                <th scope="col">Net</th>
                <th scope="col">GST</th>
              </tr>
            </thead>
            <tbody>
              {(r.lines ?? []).map((l: ReadingLine, i) => (
                <tr key={i}>
                  <td>{text(l.description) || "not shown"}</td>
                  <td className="fy-num">{text(l.net)}</td>
                  <td className="fy-num">{text(l.gst)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {(c.checks ?? []).length > 0 && (
          <ul className="fy-list" aria-label="What Fyniq checked">
            {(c.checks ?? []).map((k, i) => (
              <li key={i} className="fy-row gap-2">
                <Icon of={k.severity === "warning" ? TriangleAlert : FileText} />
                <span className="fy-small">{k.message}</span>
              </li>
            ))}
          </ul>
        )}
        <p className="fy-small fy-muted fy-expert-only">
          Read by <span className="fy-mono">{c.model}</span>; the file's SHA-256 is <span className="fy-mono">{c.sha256}</span>.
        </p>
      </div>
    </section>
  );
}

interface DraftLine {
  account_id: string;
  description: string;
  net: string;
  gst_code: string;
  gst: string;
}

// BillForm is the bill the person makes from the reading, filled from it and theirs to correct (FR-AP-04, FR-AP-12): the supplier is the
// contact the reading matched, else one they choose; the account is always theirs to choose. Amounts stay decimal strings.
function BillForm({ c, onDone }: { c: Capture; onDone: (said: string) => void }) {
  const id = useId();
  const r = c.reading;
  const accounts = useQuery({ queryKey: ["accounts"], queryFn: listAccounts });
  const vendors = useQuery({
    queryKey: ["contacts", "vendor"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/contacts", { params: { query: { role: "vendor", page_size: 500 } } });
      if (error) throw error;
      return (data.items ?? []).filter((v) => v.active);
    },
  });
  const [supplier, setSupplier] = useState(c.supplier?.id ?? "");
  const [ref, setRef] = useState(text(r?.number));
  const [day, setDay] = useState(text(r?.date));
  const [due, setDue] = useState(text(r?.due_date));
  const [currency, setCurrency] = useState(text(r?.currency) || "SGD");
  const [lines, setLines] = useState<DraftLine[]>(
    (r?.lines ?? []).map((l) => ({
      account_id: c.supplier?.defaults?.account_id ?? "", // the supplier's usual account, suggested from its bills (FR-AP-12): the person still checks it
      description: text(l.description),
      net: text(l.net),
      gst_code: text(l.gst_code) || c.supplier?.defaults?.gst_code || "",
      gst: text(l.gst) || "0.00",
    })),
  );
  const [saveDefaults, setSaveDefaults] = useState(false);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const set = (i: number, patch: Partial<DraftLine>) => setLines(lines.map((l, j) => (j === i ? { ...l, ...patch } : l)));

  const save = async () => {
    setBusy(true);
    setProblem(null);
    const money = (amount: string) => ({ amount: amount.trim(), currency });
    const r2 = await api.POST("/v1/captures/{capture_id}/bill", {
      params: { path: { capture_id: c.id }, header: { "If-Match": `"${c.version}-${c.status}"`, "Idempotency-Key": newKey() } },
      body: {
        supplier_id: supplier,
        supplier_ref: ref.trim(),
        date: day,
        due_date: due || undefined,
        currency,
        lines: lines.map((l) => ({ account_id: l.account_id, description: l.description.trim(), gst_code: l.gst_code.trim(), net: money(l.net), gst: money(l.gst) })),
        duplicate_reason: reason.trim() || undefined,
        save_defaults: saveDefaults || undefined,
      },
    });
    setBusy(false);
    if (r2.error) return setProblem(problemOf(r2.error));
    onDone("The draft bill is saved with the document attached. Check its input tax, then post it.");
  };

  return (
    <section className="fy-card" aria-labelledby={`${id}-h`}>
      <div className="fy-card__head">
        <h3 className="fy-h3" id={`${id}-h`}>
          Make a draft bill
        </h3>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <p className="fy-small fy-muted">Check each figure against the document. The bill is a draft: nothing posts until it is checked and posted, and its input tax is yours to decide.</p>
        <div className="fy-grid fy-grid--2">
          <label className="fy-field">
            <span className="fy-label">Supplier</span>
            <select className="fy-select" value={supplier} onChange={(e) => setSupplier(e.target.value)}>
              <option value="">Choose the supplier</option>
              {(vendors.data ?? []).map((v) => (
                <option key={v.id} value={v.id}>
                  {v.legal_name}
                </option>
              ))}
            </select>
            {c.supplier && <span className="fy-small fy-muted">Matched by {c.supplier.by}: {c.supplier.name}</span>}
          </label>
          <label className="fy-field">
            <span className="fy-label">Supplier's number</span>
            <input className="fy-input" value={ref} onChange={(e) => setRef(e.target.value)} />
          </label>
          <label className="fy-field">
            <span className="fy-label">Date</span>
            <input className="fy-input" type="date" value={day} onChange={(e) => setDay(e.target.value)} />
          </label>
          <label className="fy-field">
            <span className="fy-label">Due</span>
            <input className="fy-input" type="date" value={due} onChange={(e) => setDue(e.target.value)} />
          </label>
          <label className="fy-field">
            <span className="fy-label">Currency</span>
            <input className="fy-input" value={currency} maxLength={3} onChange={(e) => setCurrency(e.target.value.toUpperCase())} />
          </label>
        </div>
        {lines.map((l, i) => (
          <fieldset key={i} className="fy-stack fy-stack--2">
            <legend className="fy-label">Line {i + 1}</legend>
            <div className="fy-grid fy-grid--2">
              <label className="fy-field">
                <span className="fy-label">Account</span>
                <select className="fy-select" value={l.account_id} onChange={(e) => set(i, { account_id: e.target.value })} aria-label={`Account of line ${i + 1}`}>
                  <option value="">Choose the account</option>
                  {(accounts.data ?? [])
                    .filter((a) => a.active && !a.group && (a.type === "expense" || a.type === "asset"))
                    .map((a) => (
                      <option key={a.id} value={a.id}>
                        {a.code} {a.name}
                      </option>
                    ))}
                </select>
              </label>
              <label className="fy-field">
                <span className="fy-label">Description</span>
                <input className="fy-input" value={l.description} onChange={(e) => set(i, { description: e.target.value })} aria-label={`Description of line ${i + 1}`} />
              </label>
              <label className="fy-field">
                <span className="fy-label">Net</span>
                <input className="fy-input fy-num" inputMode="decimal" value={l.net} onChange={(e) => set(i, { net: e.target.value })} aria-label={`Net of line ${i + 1}`} />
              </label>
              <label className="fy-field">
                <span className="fy-label">GST code</span>
                <input className="fy-input" value={l.gst_code} onChange={(e) => set(i, { gst_code: e.target.value.toUpperCase() })} aria-label={`GST code of line ${i + 1}`} />
              </label>
              <label className="fy-field">
                <span className="fy-label">GST the document states</span>
                <input className="fy-input fy-num" inputMode="decimal" value={l.gst} onChange={(e) => set(i, { gst: e.target.value })} aria-label={`GST of line ${i + 1}`} />
              </label>
            </div>
          </fieldset>
        ))}
        {c.supplier?.defaults && (
          <p className="fy-small fy-muted">
            {c.supplier.defaults.from_bills > 0
              ? `Suggested from ${c.supplier.name}'s ${c.supplier.defaults.from_bills} posted bill${c.supplier.defaults.from_bills === 1 ? "" : "s"}.`
              : `${c.supplier.name}'s own defaults.`}
          </p>
        )}
        {c.supplier && !c.supplier.defaults?.saved_on_contact && (
          <label className="fy-row gap-2">
            <input type="checkbox" checked={saveDefaults} onChange={(e) => setSaveDefaults(e.target.checked)} />
            <span className="fy-small">Keep the first line's account and GST code as {c.supplier.name}'s defaults</span>
          </label>
        )}
        <label className="fy-field">
          <span className="fy-label">If it duplicates a bill already entered, why you enter it anyway</span>
          <input className="fy-input" value={reason} onChange={(e) => setReason(e.target.value)} />
        </label>
        {problem && <Refusal problem={problem} title="The draft was not made" />}
        <div className="fy-row gap-2">
          <Button onClick={() => void save()} disabled={busy || !supplier || lines.length === 0 || lines.some((l) => !l.account_id)}>
            Save draft bill
          </Button>
        </div>
      </div>
    </section>
  );
}

// DocumentInbox is the document inbox (FR-DOC-12, FR-DOC-14, FR-AP-04, FR-AP-12): every file captured by upload, paste or accepted
// mail, what the model read from it beside the file, the checks, and the draft a person made. The server decides everything: this only
// lists, asks and shows its answer. Nothing here is a ledger figure.
export function DocumentInbox() {
  const { session } = useSignedIn();
  const queries = useQueryClient();
  const idPrefix = useId();
  const help = helpText("documents.inbox");
  const mayDraft = (session.roles ?? []).some((r) => preparers.includes(r));
  const [status, setStatus] = useState("");
  const [open, setOpen] = useState("");
  const [busy, setBusy] = useState("");
  const [said, setSaid] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [reason, setReason] = useState("");

  const list = useQuery({
    queryKey: ["captures", status],
    queryFn: async () => {
      const r = await api.GET("/v1/captures", { params: { query: { status: (status || undefined) as never, page_size: 100 } } });
      if (r.error || !r.data) throw r.error ?? new Error("no captures");
      return r.data.items;
    },
  });
  const addresses = useQuery({
    queryKey: ["capture-addresses"],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/organisation/email/capture-addresses");
      if (r.error || !r.data) throw r.error ?? new Error("no addresses");
      return r.data;
    },
  });
  const chosen = (list.data ?? []).find((c) => c.id === open);
  const refresh = async (message: string) => {
    setSaid(message);
    setBusy("");
    await queries.invalidateQueries({ queryKey: ["captures"] });
  };
  const fail = (e: unknown) => {
    setProblem(problemOf(e));
    setBusy("");
    void queries.invalidateQueries({ queryKey: ["captures"] });
  };

  const capture = async (file: File, source: "upload" | "paste") => {
    setBusy("upload");
    setProblem(null);
    const body = { file: file.name, source };
    const bodySerializer = () => {
      const form = new FormData();
      form.append("file", file, file.name);
      form.append("source", source);
      return form;
    };
    const r = await api.POST("/v1/captures", { params: { header: { "Idempotency-Key": newKey() } }, body, bodySerializer });
    if (r.error) return fail(r.error);
    setOpen(r.data?.id ?? "");
    await refresh(r.data?.status === "duplicate" ? `${file.name} is in the inbox already.` : `${file.name} is captured. Have it read, then check what was read.`);
  };
  const onPaste = (e: ClipboardEvent) => {
    const file = e.clipboardData.files[0];
    if (file) {
      e.preventDefault();
      void capture(file, "paste");
    }
  };

  const read = async (c: Capture) => {
    setBusy(c.id);
    setProblem(null);
    const r = await api.POST("/v1/captures/{capture_id}/read", { params: { path: { capture_id: c.id }, header: { "If-Match": `"${c.version}-${c.status}"` } } });
    if (r.error) return fail(r.error);
    await refresh("It is read. Check each figure against the document before you make a draft.");
  };
  const dismiss = async (c: Capture) => {
    setBusy(c.id);
    setProblem(null);
    const r = await api.POST("/v1/captures/{capture_id}/dismiss", {
      params: { path: { capture_id: c.id }, header: { "If-Match": `"${c.version}-${c.status}"`, "Idempotency-Key": newKey() } },
      body: { reason: reason.trim() || undefined },
    });
    if (r.error) return fail(r.error);
    setReason("");
    await refresh("It is dismissed.");
  };
  const download = async (c: Capture) => {
    const r = await api.GET("/v1/captures/{capture_id}/file", { params: { path: { capture_id: c.id } }, parseAs: "blob" });
    if (r.error || !r.data) return fail(r.error);
    const url = URL.createObjectURL(r.data as Blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = c.file_name;
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="fy-page fy-enter" onPaste={onPaste}>
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Purchases</span>
          <h1 className="fy-h1" title={help.tooltip}>
            {help.label}
          </h1>
          <p className="fy-secondary">Bills and receipts uploaded, pasted or emailed in, what the model read from each, and the draft a person made. Nothing posts until a person checks it.</p>
        </div>
        {mayDraft && (
          <div className="fy-pagehead__actions">
            <label className="fy-btn fy-btn--primary" htmlFor={`${idPrefix}-file`}>
              <Icon of={Upload} />
              Upload a document
            </label>
            <input
              id={`${idPrefix}-file`}
              className="fy-sr"
              type="file"
              accept=".pdf,.png,.jpg,.jpeg,.webp,.heic"
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (f) void capture(f, "upload");
                e.target.value = "";
              }}
            />
          </div>
        )}
      </div>

      {addresses.data?.domain && (addresses.data.organisation || addresses.data.mine) && (
        <section className="fy-grid fy-grid--2" aria-label="Where to email documents">
          {addresses.data.organisation && (
            <div className="fy-card fy-card--pad fy-stack fy-stack--2">
              <span className="fy-tile__label">Bills and receipts for the company</span>
              <div className="fy-row fy-row--between">
                <span className="fy-mono fy-truncate">{addresses.data.organisation.address}</span>
                <Button variant="ghost" size="sm" onClick={() => void navigator.clipboard?.writeText(addresses.data?.organisation?.address ?? "")} aria-label="Copy the company's address">
                  <Icon of={Copy} />
                  Copy
                </Button>
              </div>
            </div>
          )}
        </section>
      )}

      <p className="fy-small fy-muted">
        <Icon of={Mail} /> Mail from a sender you accept is captured from <Link to="/settings/organisation">the email-in settings</Link>; paste a screenshot anywhere on this page to capture it.
      </p>

      {said && (
        <div className="fy-banner fy-banner--good" role="status">
          <Icon of={CircleCheck} size={20} />
          <div className="fy-banner__text">
            <span className="fy-small">{said}</span>
          </div>
        </div>
      )}
      {problem && <Refusal problem={problem} title="That could not be done" />}

      <div className="fy-segmented self-start" role="group" aria-label="Show">
        {filters.map(([v, label]) => (
          <button key={v} type="button" className="fy-segmented__item" aria-pressed={status === v} onClick={() => setStatus(v)}>
            {label}
          </button>
        ))}
      </div>

      {list.data && list.data.length === 0 && <p className="fy-secondary">Nothing here yet. Upload a bill or a receipt, or paste a screenshot.</p>}
      {(list.data ?? []).length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-label="Captured documents">
            <thead>
              <tr>
                <th scope="col">Captured</th>
                <th scope="col">Document</th>
                <th scope="col">Read as</th>
                <th scope="col">State</th>
              </tr>
            </thead>
            <tbody>
              {(list.data ?? []).map((c) => (
                <tr key={c.id}>
                  <td data-card="meta">{dateTime(c.created_at)}</td>
                  <td data-card="title">
                    <button type="button" className="fy-link" onClick={() => setOpen(c.id)} aria-label={`Open ${c.file_name}`}>
                      {c.file_name}
                    </button>
                  </td>
                  <td data-card="hide">{c.kind ? `${c.kind.replace("_", " ")}${c.supplier ? `, ${c.supplier.name}` : ""}` : "not read yet"}</td>
                  <td data-card="status">
                    <span className={`fy-chip ${c.status === "read" ? "fy-chip--info" : c.status === "drafted" ? "fy-chip--good" : c.status === "failed" || c.status === "duplicate" ? "fy-chip--warning" : "fy-chip--neutral"}`}>
                      <Icon of={c.status === "drafted" ? CircleCheck : c.status === "failed" || c.status === "duplicate" ? TriangleAlert : c.status === "read" ? FileText : Clock} />
                      {statusWords[c.status] ?? c.status}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {chosen && (
        <section className="fy-stack fy-stack--3" aria-label={`${chosen.file_name}`}>
          <div className="fy-row fy-row--between gap-2">
            <h2 className="fy-h2">{chosen.file_name}</h2>
            <Button variant="secondary" size="sm" onClick={() => void download(chosen)}>
              <Icon of={Download} />
              Download the file
            </Button>
          </div>
          {chosen.nric_flagged && (
            <div className="fy-banner fy-banner--warning" role="status">
              <Icon of={TriangleAlert} size={20} />
              <div className="fy-banner__text">
                <span className="fy-strong">It shows an NRIC or FIN in full</span>
                <span className="fy-small">That number never goes to the model, so this document is not read. Mask it and capture it again, or type the bill in.</span>
              </div>
            </div>
          )}
          {chosen.status === "failed" && <p className="fy-small">{chosen.note}</p>}
          {chosen.status === "duplicate" && <p className="fy-small">The same file is in the inbox already.</p>}
          {chosen.draft && (
            <p className="fy-small">
              A draft {chosen.draft.kind} was made of it:{" "}
              <Link to={chosen.draft.kind === "bill" ? "/purchases/bills/$billId" : "/me/claims/$claimId"} params={chosen.draft.kind === "bill" ? { billId: chosen.draft.id } : { claimId: chosen.draft.id }}>
                open it
              </Link>
              .
            </p>
          )}
          <ReadingCard c={chosen} />
          {mayDraft && ["received", "reading", "failed", "read"].includes(chosen.status) && (
            <div className="fy-row fy-row--wrap gap-2">
              {chosen.status !== "reading" && !chosen.nric_flagged && (
                <Button variant="secondary" onClick={() => void read(chosen)} disabled={busy !== ""}>
                  <Icon of={Sparkles} />
                  {chosen.status === "read" ? "Read it again" : "Have it read"}
                </Button>
              )}
              <label className="fy-sr" htmlFor={`${idPrefix}-why`}>
                Why you dismiss it
              </label>
              <input id={`${idPrefix}-why`} className="fy-input max-w-64" placeholder="Why, if you dismiss it" value={reason} onChange={(e) => setReason(e.target.value)} />
              <Button variant="ghost" onClick={() => void dismiss(chosen)} disabled={busy !== ""}>
                <Icon of={Ban} />
                Dismiss
              </Button>
            </div>
          )}
          {mayDraft && chosen.status === "read" && <BillForm key={chosen.id + chosen.version} c={chosen} onDone={(m) => void refresh(m)} />}
        </section>
      )}
    </div>
  );
}
