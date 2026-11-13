import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, CircleX, Download, FileSpreadsheet, Hourglass, PenLine, PlayCircle, Save, ShieldCheck, TriangleAlert, Upload } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { SettingsPage } from "./SettingsPage";

type Migration = components["schemas"]["MigrationView"];
type Report = components["schemas"]["MigrationReportView"];
type Mapping = components["schemas"]["MigrationMappingView"];
type AccountMap = components["schemas"]["MigrationAccountMap"];
type Problem = components["schemas"]["MigrationProblem"];
type ImportReport = components["schemas"]["ImportReport"];
type Entity = ImportReport["entity"];
type Commit = components["schemas"]["MigrationCommitView"];
type Reconciliation = components["schemas"]["MigrationReconciliation"];
type Difference = components["schemas"]["MigrationDifference"];
type CheckParity = components["schemas"]["MigrationCheckParity"];
type Decision = components["schemas"]["MigrationRegisterDecision"];

// absent is an answer the record is not there yet: none started, no workbook uploaded, no mapping proposed.
const absent = (r: Response) => r.status === 404;

async function migrationNow(): Promise<{ m: Migration; etag: string } | null> {
  const list = await api.GET("/v1/migrations");
  if (list.error) throw list.error;
  const first = (list.data.items ?? [])[0];
  if (!first) return null;
  const r = await api.GET("/v1/migrations/{migration_id}", { params: { path: { migration_id: first.id } } });
  if (r.error) throw r.error;
  return { m: { ...r.data, steps: r.data.steps ?? [], files: r.data.files ?? [] }, etag: etagOf(r.response) };
}

async function reportOf(id: string): Promise<Report | null> {
  const r = await api.GET("/v1/migrations/{migration_id}/report", { params: { path: { migration_id: id } } });
  if (r.error) {
    if (absent(r.response)) return null;
    throw r.error;
  }
  return { ...r.data, sheets: r.data.sheets ?? [], problems: r.data.problems ?? [] };
}

async function commitOf(id: string): Promise<Commit> {
  const r = await api.GET("/v1/migrations/{migration_id}/commit", { params: { path: { migration_id: id } } });
  if (r.error) throw r.error;
  return { ...r.data, kinds: r.data.kinds ?? [] };
}

async function reconciliationOf(id: string): Promise<{ rec: Reconciliation; etag: string }> {
  const r = await api.GET("/v1/migrations/{migration_id}/reconciliation", { params: { path: { migration_id: id } } });
  if (r.error) throw r.error;
  return {
    rec: { ...r.data, differences: r.data.differences ?? [], checks: (r.data.checks ?? []).map((c) => ({ ...c, fyniq: c.fyniq ?? [] })), not_compared: r.data.not_compared ?? [] },
    etag: etagOf(r.response),
  };
}

async function mappingOf(id: string): Promise<{ map: Mapping; etag: string } | null> {
  const r = await api.GET("/v1/migrations/{migration_id}/mapping", { params: { path: { migration_id: id } } });
  if (r.error) {
    if (absent(r.response)) return null;
    throw r.error;
  }
  return { map: { ...r.data, accounts: r.data.accounts ?? [], problems: r.data.problems ?? [] }, etag: etagOf(r.response) };
}

// The six steps of §17.1, as the page names them.
const stepNames: [string, string][] = [
  ["M1", "Upload and check the workbook"],
  ["M2", "Map and sign"],
  ["M3", "Commit"],
  ["M4", "Reconcile and sign"],
  ["M5", "Run a parallel month"],
  ["M6", "Archive and go live"],
];

function Steps({ done, current }: { done: Set<string>; current: string }) {
  return (
    <ol className="fy-steps" aria-label="Import steps">
      {stepNames.map(([id, name], i) => (
        <li key={id} className={`fy-step ${done.has(id) ? "fy-step--done" : ""}`} aria-current={id === current ? "step" : undefined}>
          <span className="fy-step__n">{done.has(id) ? <Icon of={CircleCheck} /> : i + 1}</span>
          {name}
        </li>
      ))}
    </ol>
  );
}

// where names a problem's place: its sheet, row and column as the file numbers them.
function where(p: Problem): string {
  return [p.sheet, p.row ? `row ${p.row}` : "", p.column ? `column ${p.column}` : ""].filter(Boolean).join(", ");
}

function Problems({ problems, label }: { problems: Problem[]; label: string }) {
  const shown = [...problems].sort((a, b) => (a.severity === b.severity ? 0 : a.severity === "error" ? -1 : 1)).slice(0, 100);
  if (shown.length === 0) return null;
  return (
    <ul className="fy-list" aria-label={label}>
      {shown.map((p, i) => (
        <li key={i} className="fy-list__item items-start">
          <span className={`fy-list__icon ${p.severity === "error" ? "fy-list__icon--critical" : ""}`}>
            <Icon of={p.severity === "error" ? CircleX : TriangleAlert} size={20} />
          </span>
          <span className="fy-list__main">
            <span className="fy-list__title whitespace-normal">
              {where(p) ? `${where(p)}: ` : ""}
              {p.message}
            </span>
            <span className="fy-list__meta whitespace-normal">
              {p.fix} <span className="fy-mono fy-expert-only">{p.code}</span>
            </span>
          </span>
        </li>
      ))}
      {problems.length > shown.length && <li className="fy-list__item fy-small fy-secondary">And {problems.length - shown.length} more.</li>}
    </ul>
  );
}

// Upload is M1: the frozen workbook, checked in the background, its report by sheet.
function Upload1({ m, etag, report }: { m: Migration; etag: string; report: Report | null }) {
  const queries = useQueryClient();
  const fileId = useId();
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("workbook-import.upload");
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!file) return;
    setBusy(true);
    setProblem(null);
    const bodySerializer = () => {
      const form = new FormData();
      form.append("file", file, file.name);
      return form;
    };
    const r = await api.POST("/v1/migrations/{migration_id}/workbook", {
      params: { path: { migration_id: m.id }, header: { "If-Match": etag, "Idempotency-Key": newKey() } },
      body: { file: file.name },
      bodySerializer,
    });
    setBusy(false);
    if (r.error) return setProblem(problemOf(r.error));
    setFile(null);
    await queries.invalidateQueries({ queryKey: ["migration"] });
  };
  const sheets = report?.sheets ?? [];
  return (
    <section className="fy-card" aria-labelledby="m1-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="m1-title">
          1 · Upload and check the workbook
        </h2>
        {report && (
          <span className={`fy-chip ${report.state === "valid" ? "fy-chip--good" : report.state === "invalid" ? "fy-chip--critical" : "fy-chip--neutral"}`}>
            <Icon of={report.state === "valid" ? CircleCheck : report.state === "invalid" ? CircleX : Hourglass} />
            {report.state === "valid" ? `${report.warnings} warnings` : report.state === "invalid" ? `${report.errors} errors to fix` : "Checking…"}
          </span>
        )}
      </div>
      <form className="fy-card__body fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <p className="fy-small fy-secondary">
          Upload the frozen workbook, the published .xlsx saved after its checks pass. Nothing goes into the books here: each sheet is read and checked, and
          every problem is named with its row and column.
        </p>
        {problem && <Refusal problem={problem} title="The workbook was not uploaded" />}
        <div className="fy-field">
          <label className="fy-label" htmlFor={fileId}>
            Workbook (.xlsx)
          </label>
          <input className="fy-input" id={fileId} type="file" accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
        </div>
        <div className="fy-row fy-row--wrap justify-end">
          <Button variant={report ? "secondary" : "primary"} type="submit" disabled={busy || !file} title={h.tooltip}>
            <Icon of={Upload} />
            {busy ? "Uploading…" : report ? "Upload a corrected workbook" : h.label}
          </Button>
        </div>
      </form>
      {report && report.state !== "validating" && (
        <>
          <div className="fy-tablewrap">
            <table className="fy-table fy-table--compact" aria-label="The workbook's sheets">
              <thead>
                <tr>
                  <th scope="col">Sheet</th>
                  <th scope="col" className="fy-num">
                    Rows
                  </th>
                  <th scope="col">Result</th>
                </tr>
              </thead>
              <tbody>
                {sheets.map((s) => (
                  <tr key={s.sheet}>
                    <td>{s.sheet}</td>
                    <td className="fy-num">{s.rows.toLocaleString("en-SG")}</td>
                    <td>
                      {s.errors > 0 ? (
                        <span className="fy-chip fy-chip--critical">
                          <Icon of={CircleX} />
                          {s.errors} {s.errors === 1 ? "error" : "errors"}
                        </span>
                      ) : s.warnings > 0 ? (
                        <span className="fy-chip fy-chip--warning">
                          <Icon of={TriangleAlert} />
                          {s.warnings} {s.warnings === 1 ? "warning" : "warnings"}
                        </span>
                      ) : (
                        <span className="fy-chip fy-chip--good">
                          <Icon of={CircleCheck} />
                          OK
                        </span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Problems problems={report.problems ?? []} label="The workbook's problems" />
          <p className="fy-card__body fy-small fy-secondary">
            {report.company} · books start {report.books_start} · base currency {report.base_currency}
            {report.validated_at && ` · checked ${dateTime(report.validated_at)}`}
          </p>
        </>
      )}
    </section>
  );
}

// MapAndSign is M2: the workbook's set-up mapped onto Fyniq's, each account changed where it should map elsewhere, and
// the Accountant's sign-off of the latest version.
function MapAndSign({ m, mapping }: { m: Migration; mapping: { map: Mapping; etag: string } }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const { session } = useSignedIn();
  const [edits, setEdits] = useState<Record<string, AccountMap>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const sign = helpText("workbook-import.sign-mapping");
  const map = mapping.map;
  const accountant = (session.roles ?? []).includes("accountant");
  const created = map.accounts.filter((a) => a.action === "create").length;
  const errors = map.problems.filter((p) => p.severity === "error");
  const edit = (a: AccountMap, change: Partial<AccountMap>) => setEdits((x) => ({ ...x, [a.workbook]: { ...(x[a.workbook] ?? a), ...change } }));
  const changed = Object.values(edits);
  const saveMap = async () => {
    setBusy(true);
    setProblem(null);
    const r = await api.PATCH("/v1/migrations/{migration_id}/mapping", {
      params: { path: { migration_id: m.id }, header: { "If-Match": mapping.etag, "Idempotency-Key": newKey() } },
      body: { accounts: changed },
    });
    setBusy(false);
    if (r.error) return setProblem(problemOf(r.error));
    setEdits({});
    await queries.invalidateQueries({ queryKey: ["migration"] });
  };
  const signOff = async () => {
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const r = await guarded(confirm, () =>
      api.POST("/v1/migrations/{migration_id}/mapping/sign-off", { params: { path: { migration_id: m.id }, header: { "If-Match": mapping.etag, "Idempotency-Key": key } } }),
    );
    setBusy(false);
    if (r.error) return setProblem(problemOf(r.error));
    await queries.invalidateQueries({ queryKey: ["migration"] });
  };
  return (
    <section className="fy-card" aria-labelledby="m2-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="m2-title">
          2 · Map and sign
        </h2>
        {map.signed ? (
          <span className="fy-chip fy-chip--good">
            <Icon of={ShieldCheck} />
            Signed {map.signed_at ? dateTime(map.signed_at) : ""}
          </span>
        ) : (
          <span className="fy-chip fy-chip--warning">
            <Icon of={PenLine} />
            Version {map.version}, not signed
          </span>
        )}
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <p className="fy-small fy-secondary">
          Each of the workbook's {map.accounts.length} accounts maps onto one of Fyniq's: {map.accounts.length - created} onto an account the chart has, {created}{" "}
          created from the Accounts sheet. Change where an account goes, then the Accountant signs the latest version; the commit needs it.
        </p>
        {problem && <Refusal problem={problem} title="The mapping was not saved" />}
      </div>
      <Problems problems={errors} label="What stops the sign-off" />
      <div className="fy-tablewrap">
        <table className="fy-table fy-table--compact fy-table--cards" aria-label="The accounts' mapping">
          <thead>
            <tr>
              <th scope="col">Workbook account</th>
              <th scope="col">Fyniq account</th>
              <th scope="col">How</th>
            </tr>
          </thead>
          <tbody>
            {map.accounts.map((a) => {
              const e = edits[a.workbook] ?? a;
              return (
                <tr key={a.workbook}>
                  <td data-card="title" className="whitespace-normal">
                    <span className="fy-mono fy-muted me-1.5">{a.workbook}</span>
                    {a.name}
                  </td>
                  <td data-card="meta">
                    <label className="fy-sr" htmlFor={`fyniq-${a.workbook}`}>
                      Fyniq account for {a.workbook}
                    </label>
                    <input
                      className="fy-input fy-mono max-w-28"
                      id={`fyniq-${a.workbook}`}
                      value={e.fyniq}
                      onChange={(ev) => edit(a, { fyniq: ev.target.value.trim() })}
                    />
                  </td>
                  <td data-card="meta">
                    <label className="fy-sr" htmlFor={`action-${a.workbook}`}>
                      How {a.workbook} maps
                    </label>
                    <select className="fy-select max-w-48" id={`action-${a.workbook}`} value={e.action} onChange={(ev) => edit(a, { action: ev.target.value as AccountMap["action"] })}>
                      <option value="existing">Onto the chart's account</option>
                      <option value="create">Created from the workbook</option>
                    </select>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <div className="fy-card__foot fy-row fy-row--wrap justify-end gap-3">
        {!accountant && !map.signed && <span className="fy-small fy-secondary">Only the Accountant signs the mapping</span>}
        <Button onClick={() => void saveMap()} disabled={busy || changed.length === 0}>
          <Icon of={Save} />
          Save {changed.length > 0 ? `${changed.length} ${changed.length === 1 ? "change" : "changes"}` : "changes"}
        </Button>
        <Button variant="primary" onClick={() => void signOff()} disabled={busy || map.signed || !accountant || changed.length > 0 || errors.length > 0} title={sign.tooltip}>
          <Icon of={ShieldCheck} />
          {sign.label}
        </Button>
      </div>
    </section>
  );
}

// kindNames are the records the commit imports, by kind, as the page names them.
const kindNames: Record<string, string> = {
  settings: "Settings",
  account: "Accounts",
  contact: "Contacts",
  item: "Items",
  opening_balances: "Opening balances",
  invoice: "Invoices",
  credit_note: "Credit notes",
  bill: "Bills",
  vendor_credit: "Vendor credits",
  journal: "Journals",
  bank_transaction: "Bank transactions",
  asset: "Fixed assets",
  depreciation: "Depreciation",
};

// kindName is a kind of record in words: its name above, else its words.
function kindName(kind: string): string {
  return kindNames[kind] ?? kind.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase());
}

// Commit3 is M3: the commit asked for by its path, approved by an Owner who did not ask, then imported batch by batch,
// each record through the command that enters it by hand.
function Commit3({ m, mapping, commit }: { m: Migration; mapping: { map: Mapping; etag: string }; commit: Commit }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const pathId = useId();
  const [path, setPath] = useState<"history" | "clean_start">("history");
  const [busy, setBusy] = useState(false);
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const ask = helpText("workbook-import.commit");
  const run = helpText("workbook-import.run");
  const request = async () => {
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const r = await guarded(confirm, () =>
      api.POST("/v1/migrations/{migration_id}/commit", {
        params: { path: { migration_id: m.id }, header: { "If-Match": mapping.etag, "Idempotency-Key": key } },
        body: { path },
      }),
    );
    setBusy(false);
    if (r.error) return setProblem(problemOf(r.error));
    await queries.invalidateQueries({ queryKey: ["migration"] });
  };
  // importAll runs the batches one after another until the import is committed or a record stops it.
  const importAll = async () => {
    setBusy(true);
    setProblem(null);
    for (;;) {
      const r = await api.POST("/v1/migrations/{migration_id}/commit:run", { params: { path: { migration_id: m.id } } });
      if (r.error) {
        setProblem(problemOf(r.error));
        break;
      }
      setProgress({ done: r.data.done, total: r.data.total });
      if (r.data.status === "committed") break;
    }
    setBusy(false);
    await queries.invalidateQueries({ queryKey: ["migration"] });
  };
  const shown = progress ?? { done: commit.done, total: commit.total };
  return (
    <section className="fy-card" aria-labelledby="m3-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="m3-title">
          3 · Commit
        </h2>
        {commit.status === "committed" && (
          <span className="fy-chip fy-chip--good">
            <Icon of={CircleCheck} />
            Committed {commit.committed_at ? dateTime(commit.committed_at) : ""}
          </span>
        )}
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        {problem && <Refusal problem={problem} title="The import did not go on" />}
        {commit.status === "none" && (
          <>
            <p className="fy-small fy-secondary">
              Ask to commit the signed mapping. An Owner who did not ask approves it; then the workbook's records are entered, each through the same command
              as entering it by hand.
            </p>
            <div className="fy-field">
              <label className="fy-label" htmlFor={pathId}>
                What to import
              </label>
              <select className="fy-select max-w-96" id={pathId} value={path} onChange={(e) => setPath(e.target.value as "history" | "clean_start")}>
                <option value="history">The year's history, from the books start</option>
                <option value="clean_start">A clean start from the Carry Forward sheet</option>
              </select>
            </div>
            <div className="fy-row justify-end">
              <Button variant="primary" onClick={() => void request()} disabled={busy || !mapping.map.signed} title={ask.tooltip}>
                <Icon of={ShieldCheck} />
                {ask.label}
              </Button>
            </div>
            {!mapping.map.signed && <span className="fy-small fy-secondary">The Accountant signs the mapping first.</span>}
          </>
        )}
        {commit.status === "pending" && (
          <div className="fy-banner fy-banner--info" role="status">
            <Icon of={Hourglass} size={20} />
            <div className="fy-banner__text">
              <span className="fy-strong">Waiting for an Owner who did not ask to approve the commit</span>
              <span className="fy-small">
                {commit.total.toLocaleString("en-SG")} records planned ({commit.path === "clean_start" ? "a clean start" : "the year's history"}).{" "}
                {commit.approval_id && (
                  <Link to="/approvals/$approvalId" params={{ approvalId: commit.approval_id }}>
                    Open the approval
                  </Link>
                )}
              </span>
            </div>
          </div>
        )}
        {(commit.status === "running" || commit.status === "committed") && (
          <>
            <div className="fy-stack fy-stack--2">
              <span className="fy-label" id="m3-progress">
                {shown.done.toLocaleString("en-SG")} of {shown.total.toLocaleString("en-SG")} records imported
              </span>
              <progress className="w-full" aria-labelledby="m3-progress" max={Math.max(shown.total, 1)} value={shown.done} />
            </div>
            <ul className="fy-row fy-row--wrap gap-2" aria-label="Imported by kind">
              {commit.kinds.map((k) => (
                <li key={k.kind} className="fy-tag">
                  {kindName(k.kind)} {k.done.toLocaleString("en-SG")}
                </li>
              ))}
            </ul>
            {commit.status === "running" && (
              <div className="fy-row justify-end">
                <Button variant="primary" onClick={() => void importAll()} disabled={busy} title={run.tooltip}>
                  <Icon of={PlayCircle} />
                  {busy ? "Importing…" : commit.done > 0 ? "Continue the import" : run.label}
                </Button>
              </div>
            )}
          </>
        )}
      </div>
    </section>
  );
}

const classes: Record<Decision["class"], string> = {
  a: "a · cent allocation of spreads and depreciation",
  b: "b · a deliberate rule change",
  c: "c · a workbook defect",
};

// Decide is one line of the register: the Accountant's class, with a note for a defect, made on the report as read.
function Decide({ m, line, decision, proposed, etag }: { m: Migration; line: string; decision?: Decision; proposed?: string; etag: string }) {
  const queries = useQueryClient();
  const id = useId();
  const [cls, setCls] = useState<Decision["class"] | "">(decision?.class ?? (proposed as Decision["class"]) ?? "");
  const [note, setNote] = useState(decision?.note ?? "");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const save = async () => {
    if (!cls) return;
    setBusy(true);
    setProblem(null);
    const r = await api.PATCH("/v1/migrations/{migration_id}/reconciliation/register/{line}", {
      params: { path: { migration_id: m.id, line }, header: { "If-Match": etag, "Idempotency-Key": newKey() } },
      body: { class: cls, ...(note.trim() ? { note: note.trim() } : {}) },
    });
    setBusy(false);
    if (r.error) return setProblem(problemOf(r.error));
    await queries.invalidateQueries({ queryKey: ["migration"] });
  };
  return (
    <span className="fy-stack fy-stack--2">
      {decision && !decision.stale && (
        <span className="fy-chip fy-chip--good self-start">
          <Icon of={CircleCheck} />
          Class {decision.class}
        </span>
      )}
      {decision?.stale && (
        <span className="fy-chip fy-chip--warning self-start">
          <Icon of={TriangleAlert} />
          The figures moved: decide again
        </span>
      )}
      <span className="fy-row fy-row--wrap gap-2">
        <label className="fy-sr" htmlFor={`${id}-class`}>
          Class of {line}
        </label>
        <select className="fy-select max-w-72" id={`${id}-class`} value={cls} onChange={(e) => setCls(e.target.value as Decision["class"])}>
          <option value="">Choose a class</option>
          {(Object.keys(classes) as Decision["class"][]).map((c) => (
            <option key={c} value={c}>
              {classes[c]}
            </option>
          ))}
        </select>
        <label className="fy-sr" htmlFor={`${id}-note`}>
          Note on {line}
        </label>
        <input className="fy-input max-w-72" id={`${id}-note`} placeholder={cls === "c" ? "What the defect is (needed)" : "Note (optional)"} value={note} maxLength={500} onChange={(e) => setNote(e.target.value)} />
        <Button size="sm" onClick={() => void save()} disabled={busy || !cls || (cls === "c" && !note.trim())}>
          Decide
        </Button>
      </span>
      {problem && <Refusal problem={problem} title="The line was not decided" />}
    </span>
  );
}

const checkWords: Record<CheckParity["workbook"], string> = { fine: "Fine", review: "Review", blocked: "Blocked" };

// Reconcile4 is M4: every account and month compared with the workbook to the cent, each figure that differs and each
// check whose status differs decided as class a, b or c, and the Accountant's sign-off.
function Reconcile4({ m, rec, etag }: { m: Migration; rec: Reconciliation; etag: string }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const { session } = useSignedIn();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const sign = helpText("workbook-import.sign-reconciliation");
  const accountant = (session.roles ?? []).includes("accountant");
  const differing = rec.checks.filter((c) => !c.same);
  const signOff = async () => {
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const r = await guarded(confirm, () =>
      api.POST("/v1/migrations/{migration_id}/reconciliation/sign-off", { params: { path: { migration_id: m.id }, header: { "If-Match": etag, "Idempotency-Key": key } } }),
    );
    setBusy(false);
    if (r.error) return setProblem(problemOf(r.error));
    await queries.invalidateQueries({ queryKey: ["migration"] });
  };
  const row = (d: Difference) => (
    <tr key={d.key}>
      <td data-card="title" className="whitespace-normal">
        <span className="fy-mono fy-muted me-1.5">{d.account}</span>
        {d.name}
        {d.month && <span className="fy-small fy-secondary"> · {d.month}</span>}
      </td>
      <td className="fy-num" data-card="meta">
        {d.workbook}
      </td>
      <td className="fy-num" data-card="meta">
        {d.fyniq}
      </td>
      <td className="fy-num" data-card="amount">
        {d.difference}
      </td>
      <td data-card="status">
        <Decide m={m} line={d.key} decision={d.decision} proposed={d.proposed_class} etag={etag} />
      </td>
    </tr>
  );
  return (
    <section className="fy-card" aria-labelledby="m4-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="m4-title">
          4 · Reconcile and sign
        </h2>
        {rec.sign_off?.current ? (
          <span className="fy-chip fy-chip--good">
            <Icon of={ShieldCheck} />
            Signed {dateTime(rec.sign_off.signed_at)}
          </span>
        ) : (
          <span className={`fy-chip ${rec.undecided > 0 ? "fy-chip--warning" : "fy-chip--good"}`}>
            <Icon of={rec.undecided > 0 ? TriangleAlert : CircleCheck} />
            {rec.undecided > 0 ? `${rec.undecided} to decide` : "All decided"}
          </span>
        )}
      </div>
      <div className="fy-grid fy-grid--4 gap-3 p-4">
        <div className="fy-tile">
          <span className="fy-tile__label">Figures compared</span>
          <span className="fy-tile__value fy-num">{rec.figures.toLocaleString("en-SG")}</span>
          <span className="fy-tile__meta">
            {rec.accounts} accounts, through {rec.through}
          </span>
        </div>
        <div className="fy-tile">
          <span className="fy-tile__label">Match to the cent</span>
          <span className="fy-tile__value fy-num">{(rec.figures - rec.differences.length).toLocaleString("en-SG")}</span>
        </div>
        <div className="fy-tile">
          <span className="fy-tile__label">Differ</span>
          <span className="fy-tile__value fy-num">{rec.differences.length}</span>
          <span className="fy-tile__meta">Each decided in the register</span>
        </div>
        <div className="fy-tile">
          <span className="fy-tile__label">Checks that differ</span>
          <span className="fy-tile__value fy-num">{rec.checks_differ}</span>
          <span className="fy-tile__meta">{rec.checks_run_at ? `Run ${dateTime(rec.checks_run_at)}` : "Not run yet"}</span>
        </div>
      </div>
      {rec.not_compared.includes("gst_f5") && <p className="fy-card__body fy-small fy-secondary">The GST F5 is compared once Fyniq makes the F5 return.</p>}
      {problem && <Refusal problem={problem} title="The register was not signed" />}
      {rec.differences.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-label="Figures that differ">
            <thead>
              <tr>
                <th scope="col">Account</th>
                <th scope="col" className="fy-num">
                  Workbook
                </th>
                <th scope="col" className="fy-num">
                  Fyniq
                </th>
                <th scope="col" className="fy-num">
                  Difference
                </th>
                <th scope="col">Register</th>
              </tr>
            </thead>
            <tbody>{rec.differences.map(row)}</tbody>
          </table>
        </div>
      )}
      {differing.length > 0 && (
        <ul className="fy-list" aria-label="Checks that differ">
          {differing.map((c) => (
            <li key={c.key} className="fy-list__item items-start">
              <span className="fy-list__main">
                <span className="fy-list__title whitespace-normal">
                  Check {c.number}: {c.name}
                </span>
                <span className="fy-list__meta whitespace-normal">
                  The workbook says {checkWords[c.workbook]}; Fyniq's: {c.missing ? "none matches it" : c.fyniq.map((x) => x.status).join(", ")}
                </span>
                <Decide m={m} line={c.key} decision={c.decision} etag={etag} />
              </span>
            </li>
          ))}
        </ul>
      )}
      <div className="fy-card__foot fy-row fy-row--wrap justify-end gap-3">
        {!accountant && <span className="fy-small fy-secondary">Only the Accountant signs the register</span>}
        <Button variant="primary" onClick={() => void signOff()} disabled={busy || !accountant || rec.undecided > 0 || !!rec.sign_off?.current} title={sign.tooltip}>
          <Icon of={ShieldCheck} />
          {sign.label}
        </Button>
      </div>
    </section>
  );
}

// The generic import's entities (FR-MIG-03), as the page names them.
const entities: [Entity, string][] = [
  ["contacts", "Contacts"],
  ["items", "Items"],
  ["accounts", "Accounts of the chart"],
  ["opening-balances", "Opening balances (a draft)"],
  ["journals", "Journals (drafts)"],
  ["invoices", "Invoices (drafts)"],
  ["bills", "Bills (drafts)"],
  ["fx-rates", "Monthly FX rates"],
  ["bank-rules", "Bank rules"],
  ["budgets", "Budgets (drafts)"],
];

function saveBlob(name: string, blob: Blob) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}

// OtherImports is the generic import (FR-MIG-03): a template per kind of record, as the workbook's sheet for it, and a
// file checked by a dry run before it is imported whole.
function OtherImports() {
  const queries = useQueryClient();
  const kindId = useId();
  const formatId = useId();
  const fileId = useId();
  const [entity, setEntity] = useState<Entity>("contacts");
  const [format, setFormat] = useState<"csv" | "xlsx">("csv");
  const [file, setFile] = useState<File | null>(null);
  const [checked, setChecked] = useState<ImportReport | null>(null);
  const [done, setDone] = useState<ImportReport | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const tpl = helpText("workbook-import.template");
  const dry = helpText("workbook-import.dry-run");
  // Every entity's operations take and answer the same: the contacts' stand for them all.
  const path = `/v1/imports/${entity}` as "/v1/imports/contacts";
  const template = async () => {
    setProblem(null);
    const r = await api.GET(`/v1/imports/${entity}/template` as "/v1/imports/contacts/template", { params: { query: { format } }, parseAs: "blob" });
    if (r.error) return setProblem(problemOf(r.error));
    saveBlob(`${entity}-template.${format}`, r.data as unknown as Blob);
  };
  const send = async (preview: boolean) => {
    if (!file) return;
    setBusy(true);
    setProblem(null);
    const bodySerializer = () => {
      const form = new FormData();
      form.append("file", file, file.name);
      return form;
    };
    const header = preview ? { Prefer: "return=preview" } : { "Idempotency-Key": newKey() };
    const r = await api.POST(path, { params: { header }, body: { file: file.name }, bodySerializer });
    setBusy(false);
    if (r.error) {
      setProblem(problemOf(r.error));
      return;
    }
    if (preview) {
      setChecked(r.data);
      setDone(null);
    } else {
      setDone(r.data);
      setChecked(null);
      setFile(null);
      await queries.invalidateQueries();
    }
  };
  return (
    <section className="fy-card" aria-labelledby="other-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="other-title">
          Other imports
        </h2>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <p className="fy-small fy-secondary">
          Import one kind of record from CSV or XLSX, laid out as the workbook's sheet for it. Each file is checked first and saves nothing; then it is imported
          whole, or not at all. Journals, invoices, bills, opening balances and budgets come in as drafts, approved later as usual.
        </p>
        {problem && <Refusal problem={problem} title="The file was not imported" />}
        <div className="fy-row fy-row--wrap gap-3">
          <div className="fy-field">
            <label className="fy-label" htmlFor={kindId}>
              Records
            </label>
            <select
              className="fy-select"
              id={kindId}
              value={entity}
              onChange={(e) => {
                setEntity(e.target.value as Entity);
                setChecked(null);
                setDone(null);
              }}
            >
              {entities.map(([v, label]) => (
                <option key={v} value={v}>
                  {label}
                </option>
              ))}
            </select>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={formatId}>
              Template as
            </label>
            <select className="fy-select" id={formatId} value={format} onChange={(e) => setFormat(e.target.value as "csv" | "xlsx")}>
              <option value="csv">CSV</option>
              <option value="xlsx">Excel (.xlsx)</option>
            </select>
          </div>
        </div>
        <div className="fy-row fy-row--wrap">
          <Button size="sm" onClick={() => void template()} title={tpl.tooltip}>
            <Icon of={Download} />
            {tpl.label}
          </Button>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={fileId}>
            File to import
          </label>
          <input
            className="fy-input"
            id={fileId}
            type="file"
            accept=".csv,.xlsx,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
            onChange={(e) => {
              setFile(e.target.files?.[0] ?? null);
              setChecked(null);
            }}
          />
        </div>
        <div className="fy-row fy-row--wrap justify-end gap-3">
          <Button onClick={() => void send(true)} disabled={busy || !file} title={dry.tooltip}>
            <Icon of={FileSpreadsheet} />
            {dry.label}
          </Button>
          <Button variant="primary" onClick={() => void send(false)} disabled={busy || !file || !checked || checked.errors > 0}>
            <Icon of={PlayCircle} />
            Import it
          </Button>
        </div>
        {checked && (
          <div className={`fy-banner ${checked.errors > 0 ? "fy-banner--warning" : "fy-banner--info"}`} role="status">
            <Icon of={checked.errors > 0 ? TriangleAlert : CircleCheck} size={20} />
            <div className="fy-banner__text">
              <span className="fy-strong">
                {checked.errors > 0
                  ? `${checked.errors} ${checked.errors === 1 ? "problem stops" : "problems stop"} the import`
                  : `Checked: ${checked.made} records would be made from ${checked.rows} rows`}
              </span>
              <span className="fy-small">Nothing was saved.</span>
            </div>
          </div>
        )}
        {done && (
          <div className="fy-banner fy-banner--info" role="status">
            <Icon of={CircleCheck} size={20} />
            <div className="fy-banner__text">
              <span className="fy-strong">
                Imported: {done.made} records from {done.file_name}
              </span>
            </div>
          </div>
        )}
      </div>
      {checked && <Problems problems={checked.problems ?? []} label="The file's problems" />}
    </section>
  );
}

// WorkbookImport is Settings › Import (docs/design/screens WorkbookImport; FR-MIG-01…03, §17.1 M1 to M6): the move from
// the workbook, step by step, and the import of one kind of record from CSV or XLSX.
export function WorkbookImport() {
  const queries = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const q = useQuery({ queryKey: ["migration"], queryFn: migrationNow });
  const id = q.data?.m.id;
  const report = useQuery({
    queryKey: ["migration", id, "report"],
    queryFn: () => reportOf(id ?? ""),
    enabled: !!id,
    refetchInterval: (query) => (query.state.data?.state === "validating" ? 2000 : false),
  });
  const mapping = useQuery({ queryKey: ["migration", id, "mapping"], queryFn: () => mappingOf(id ?? ""), enabled: !!id && report.data?.state === "valid" });
  const commit = useQuery({ queryKey: ["migration", id, "commit"], queryFn: () => commitOf(id ?? ""), enabled: !!id && !!mapping.data });
  const reconciliation = useQuery({
    queryKey: ["migration", id, "reconciliation"],
    queryFn: () => reconciliationOf(id ?? ""),
    enabled: !!id && commit.data?.status === "committed",
  });
  const start = helpText("workbook-import.start");
  const begin = async () => {
    setBusy(true);
    setProblem(null);
    const r = await api.POST("/v1/migrations", { params: { header: { "Idempotency-Key": newKey() } } });
    setBusy(false);
    if (r.error) return setProblem(problemOf(r.error));
    await queries.invalidateQueries({ queryKey: ["migration"] });
  };
  const m = q.data?.m;
  const done = new Set<string>();
  if (report.data?.state === "valid") done.add("M1");
  if (mapping.data?.map.signed) done.add("M2");
  if (commit.data?.status === "committed") done.add("M3");
  if (reconciliation.data?.rec.sign_off?.current) done.add("M4");
  if (m?.status === "closed") stepNames.forEach(([s]) => done.add(s));
  const current = stepNames.find(([s]) => !done.has(s))?.[0] ?? "M6";

  return (
    <SettingsPage title="Import from the workbook" screen="workbook-import" lead="Move from the finance workbook step by step, with a report at each step before anything is saved to the books.">
      {q.isError && <Refusal problem={problemOf(q.error)} title="The import could not be read" />}
      {q.isPending && <p className="fy-secondary">Reading the import…</p>}
      {q.data === null && (
        <section className="fy-card" aria-labelledby="start-title">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="start-title">
              Start the import
            </h2>
          </div>
          <div className="fy-card__body fy-stack fy-stack--3">
            <p className="fy-small fy-secondary">
              The import goes into an organisation that has posted nothing yet. You upload the frozen workbook, check it, map it and sign the mapping; an Owner
              who did not ask approves the commit; then the books are reconciled with the workbook, a month is run in both, and the workbook is archived.
            </p>
            {problem && <Refusal problem={problem} title="The import was not started" />}
            <div className="fy-row justify-end">
              <Button variant="primary" onClick={() => void begin()} disabled={busy} title={start.tooltip}>
                <Icon of={FileSpreadsheet} />
                {start.label}
              </Button>
            </div>
          </div>
        </section>
      )}
      {m && (
        <>
          <Steps done={done} current={current} />
          <Upload1 m={m} etag={q.data?.etag ?? ""} report={report.data ?? null} />
          {mapping.data && <MapAndSign m={m} mapping={mapping.data} />}
          {mapping.data && commit.data && <Commit3 m={m} mapping={mapping.data} commit={commit.data} />}
          {reconciliation.data && <Reconcile4 m={m} rec={reconciliation.data.rec} etag={reconciliation.data.etag} />}
        </>
      )}
      <OtherImports />
    </SettingsPage>
  );
}
