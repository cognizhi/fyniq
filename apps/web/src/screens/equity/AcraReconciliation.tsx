import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, ClipboardList, PencilLine, Plus, Stamp, Trash2, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { EvidencePanel } from "../accounting/JournalEvidence";
import { statuses } from "../checks/data";
import { EquityTabs, listHolders } from "./Holders";
import { useCapital } from "./OptionForms";
import { count } from "./optionWords";

type Extract = components["schemas"]["AcraExtractView"];
type ExtractDraft = components["schemas"]["AcraExtractDraft"];
type Reconciliation = components["schemas"]["AcraReconciliation"];
type Check = components["schemas"]["CheckOut"];

// Who types in an extract and records it: C and P on the cap table, the Accountant (Appendix D).
const drafters = ["accountant"];

async function reconciliation(): Promise<Reconciliation> {
  const { data, error } = await api.GET("/v1/cap-table/acra-reconciliation", {});
  if (error) throw error;
  return data;
}

async function extracts(): Promise<Extract[]> {
  const { data, error } = await api.GET("/v1/acra-extracts", { params: { query: { page_size: 50 } } });
  if (error) throw error;
  return data.items ?? [];
}

async function checkOf(key: string): Promise<Check> {
  const { data, error } = await api.GET("/v1/checks/{key}", { params: { path: { key } } });
  if (error) throw error;
  return data;
}

// CheckLine is a check's status as a chip with its words, as the checks page shows it.
function CheckLine({ c }: { c: Check }) {
  const s = statuses[c.status];
  return (
    <div className="fy-stack fy-stack--2">
      <span className={`fy-chip ${s.tone}`} style={{ alignSelf: "flex-start" }}>
        <Icon of={s.icon} />
        {s.label}
      </span>
      <p className="fy-small fy-secondary">{c.detail}</p>
    </div>
  );
}

// ExtractForm types in a business profile's holdings (FR-EQ-26; the owner's decision of 7 Nov 2026), or changes the
// draft at its version. A new one starts with a row for each holding the register has, the shares left for the person
// to type from the PDF, so the register is never copied into it.
function ExtractForm({ draft, start, onClose, onDone }: { draft: Extract | null; start: ExtractDraft; onClose: () => void; onDone: (text: string) => void }) {
  const id = useId();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const holders = useQuery({ queryKey: ["holders"], queryFn: listHolders });
  const capital = useCapital();
  const [form, setForm] = useState<ExtractDraft>({ ...start, holdings: start.holdings.map((h) => ({ ...h })) });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const set = (i: number, x: Partial<ExtractDraft["holdings"][number]>) => setForm({ ...form, holdings: form.holdings.map((h, j) => (j === i ? { ...h, ...x } : h)) });
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const body: ExtractDraft = {
      profile_on: form.profile_on,
      holdings: form.holdings.filter((h) => h.holder_id || h.shares).map((h) => ({ ...h, shares: h.shares.replace(/[,\s]/g, "") })),
    };
    if (form.note?.trim()) body.note = form.note.trim();
    const out = await guarded(confirm, () =>
      draft
        ? api.PUT("/v1/acra-extracts/{extract_id}", { params: { path: { extract_id: draft.id }, header: { "If-Match": `"${draft.version}-draft"`, "Idempotency-Key": key } }, body })
        : api.POST("/v1/acra-extracts", { params: { header: { "Idempotency-Key": key } }, body }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["acra-extracts"] });
    onDone(draft ? "The draft is saved." : "The extract is typed in. Attach the business profile's PDF, then record it.");
    onClose();
  };
  const people = (holders.data ?? []).filter((h) => h.shareholder || h.investor);
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} wide eyebrow="ACRA register of members" title={draft ? "Change the draft" : "Type in a business profile"}
      description="Type each holder's shares as the business profile lists them. Recorded with the PDF, it is compared with the register at its day.">
      {problem && <Refusal problem={problem} title="The extract was not saved" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-on`}>
            Business profile of
          </label>
          <input className="fy-input" id={`${id}-on`} type="date" value={form.profile_on} onChange={(e) => setForm({ ...form, profile_on: e.target.value })}
            aria-describedby={`${id}-on-help`} />
          <span className="fy-help" id={`${id}-on-help`}>
            The day printed on the business profile
          </span>
        </div>
        <fieldset className="fy-field">
          <legend className="fy-label">Holdings</legend>
          <div className="fy-stack fy-stack--2">
            {form.holdings.map((h, i) => (
              <div key={i} className="fy-row fy-row--wrap gap-2">
                <select className="fy-select" aria-label={`Holding ${i + 1}: holder`} value={h.holder_id} onChange={(e) => set(i, { holder_id: e.target.value })}>
                  <option value="">Choose the holder</option>
                  {people.map((p) => (
                    <option key={p.contact_id} value={p.contact_id}>
                      {p.name}
                    </option>
                  ))}
                </select>
                <select className="fy-select" aria-label={`Holding ${i + 1}: class`} value={h.class_account_id || capital[0]?.id || ""}
                  onChange={(e) => set(i, { class_account_id: e.target.value })}>
                  {capital.map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.code} {a.name}
                    </option>
                  ))}
                </select>
                <input className="fy-input fy-num" aria-label={`Holding ${i + 1}: shares`} inputMode="numeric" value={h.shares} placeholder="Shares"
                  onChange={(e) => set(i, { shares: e.target.value })} />
                <Button variant="ghost" size="sm" type="button" aria-label={`Remove holding ${i + 1}`} onClick={() => setForm({ ...form, holdings: form.holdings.filter((_, j) => j !== i) })}>
                  <Icon of={Trash2} />
                </Button>
              </div>
            ))}
            <Button variant="ghost" size="sm" type="button" style={{ alignSelf: "flex-start" }}
              onClick={() => setForm({ ...form, holdings: [...form.holdings, { holder_id: "", class_account_id: capital[0]?.id ?? "", shares: "" }] })}>
              <Icon of={Plus} />
              Add a holding
            </Button>
          </div>
        </fieldset>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-note`}>
            Note (optional)
          </label>
          <textarea className="fy-textarea" id={`${id}-note`} maxLength={1000} value={form.note ?? ""} onChange={(e) => setForm({ ...form, note: e.target.value })} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy}>
            {draft ? "Save the draft" : "Save the extract"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function HoldingsTable({ x, label }: { x: Extract; label: string }) {
  return (
    <div className="fy-tablewrap">
      <table className="fy-table fy-table--compact" aria-label={label}>
        <thead>
          <tr>
            <th scope="col">Holder</th>
            <th scope="col">Class</th>
            <th scope="col" className="fy-num">
              Shares
            </th>
          </tr>
        </thead>
        <tbody>
          {x.holdings.map((h) => (
            <tr key={`${h.holder_id}-${h.class_account_id}`}>
              <td>{h.holder_name}</td>
              <td>{h.class_code}</td>
              <td className="fy-num">{count(h.shares)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// AcraReconciliation is Equity › Reconciliation (docs/design/screens CapTable "Reconciliation"; FR-EQ-26, FR-EQ-27,
// design D11, Expert): paid-up capital against the ledger (C.2 #25), and the register of members against the latest
// ACRA business profile typed in, with every difference (C.2 #26); a draft extract with its PDF, recorded by the
// Accountant; the extracts before.
export function AcraReconciliation() {
  const { session } = useSignedIn();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const mayDraft = (session.roles ?? []).some((r) => drafters.includes(r));
  const rec = useQuery({ queryKey: ["acra-extracts", "reconciliation"], queryFn: reconciliation });
  const all = useQuery({ queryKey: ["acra-extracts", "list"], queryFn: extracts });
  const paid = useQuery({ queryKey: ["check", "founders.share-capital-registered"], queryFn: () => checkOf("founders.share-capital-registered"), retry: false });
  const holders = useQuery({ queryKey: ["holders"], queryFn: listHolders, enabled: mayDraft });
  const capital = useCapital();
  const [editing, setEditing] = useState<"new" | "draft" | null>(null);
  const [status, setStatus] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const draft = all.data?.find((x) => x.status === "draft") ?? null;
  const earlier = (all.data ?? []).filter((x) => x.status === "superseded");
  const draftH = helpText("acra-extract.draft");
  const recordH = helpText("acra-extract.record");
  // A new extract starts with a row for each holder and class the register holds, the shares left empty.
  const start: ExtractDraft = {
    profile_on: "",
    holdings: (holders.data ?? []).flatMap((h) =>
      h.holdings.map((x) => ({ holder_id: h.contact_id, class_account_id: capital.find((a) => a.code === x.account_code)?.id ?? "", shares: "" })),
    ),
  };
  const record = async () => {
    if (!draft) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/acra-extracts/{extract_id}/record", { params: { path: { extract_id: draft.id }, header: { "If-Match": `"${draft.version}-draft"`, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["acra-extracts"] });
    setStatus(`Recorded the business profile of ${date(out.data.profile_on)}. The register is compared with it at its day.`);
  };
  const r = rec.data;
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Equity</span>
          <h1 className="fy-h1" title={helpText("equity.reconciliation").tooltip}>
            Reconciliation
          </h1>
          <p className="fy-secondary">The register of members against the ledger and against ACRA's register, as the latest business profile shows it.</p>
        </div>
        {mayDraft && !draft && (
          <div className="fy-pagehead__actions">
            <Button variant="primary" onClick={() => setEditing("new")} title={draftH.tooltip}>
              <Icon of={ClipboardList} />
              {draftH.label}
            </Button>
          </div>
        )}
      </div>
      <EquityTabs current="reconciliation" />
      <p className="fy-small" role="status">
        {status}
      </p>
      {problem && <Refusal problem={problem} title="The extract was not recorded" />}
      {rec.isError && <Refusal problem={problemOf(rec.error)} title="The reconciliation could not be shown" />}
      <div className="fy-split">
        <section className="fy-card" aria-labelledby="acra-title">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="acra-title" title={helpText("acra.differences").tooltip}>
              Against ACRA's register of members
            </h2>
            {r?.extract &&
              (r.differences.length === 0 ? (
                <span className="fy-chip fy-chip--good">
                  <Icon of={CircleCheck} />
                  Agrees
                </span>
              ) : (
                <span className="fy-chip fy-chip--warning">
                  <Icon of={TriangleAlert} />
                  {r.differences.length} difference{r.differences.length === 1 ? "" : "s"}
                </span>
              ))}
          </div>
          <div className="fy-card__body fy-stack fy-stack--3">
            {r && !r.extract && (
              <p className="fy-small fy-muted">
                No business profile is recorded yet. Buy the business profile from ACRA's Bizfile, type in its holdings and attach its PDF: the register is then
                compared with it.
              </p>
            )}
            {r?.extract && (
              <p className="fy-small">
                Business profile of {date(r.extract.profile_on)}
                {r.extract.recorded_by ? `, recorded by ${r.extract.recorded_by.name}` : ""}
                {r.extract.recorded_at ? ` on ${dateTime(r.extract.recorded_at)}` : ""}.
              </p>
            )}
            {r?.extract && r.differences.length > 0 && (
              <>
                <div className="fy-tablewrap">
                  <table className="fy-table" aria-label="Differences from ACRA">
                    <thead>
                      <tr>
                        <th scope="col">Holder</th>
                        <th scope="col">Class</th>
                        <th scope="col" className="fy-num">
                          Register
                        </th>
                        <th scope="col" className="fy-num">
                          ACRA
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      {r.differences.map((d) => (
                        <tr key={`${d.holder_id}-${d.class_account_id}`}>
                          <td>{d.holder_name}</td>
                          <td>{d.class_code}</td>
                          <td className="fy-num">{count(d.register)}</td>
                          <td className="fy-num">{count(d.acra)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <p className="fy-small fy-secondary">
                  Find which is right. Where ACRA is right, correct the register by a reversal citing the extract; where the typing is wrong, type the profile in
                  again.
                </p>
              </>
            )}
            {r?.extract && <HoldingsTable x={r.extract} label="Holdings in the business profile" />}
          </div>
        </section>
        <div className="fy-stack fy-stack--6">
          <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby="paid-title">
            <h2 className="fy-h3" id="paid-title" title={helpText("acra.paid-up").tooltip}>
              Paid-up capital against the ledger
            </h2>
            {paid.data ? <CheckLine c={paid.data} /> : <p className="fy-small fy-muted">The checks have not run yet.</p>}
          </section>
          {draft && (
            <section className="fy-card" aria-labelledby="acra-draft-title">
              <div className="fy-card__head">
                <h2 className="fy-h3" id="acra-draft-title">
                  Draft: business profile of {date(draft.profile_on)}
                </h2>
                {mayDraft && (
                  <div className="fy-row gap-2">
                    <Button size="sm" variant="ghost" onClick={() => setEditing("draft")}>
                      <Icon of={PencilLine} />
                      Change
                    </Button>
                    <Button size="sm" onClick={() => void record()} disabled={busy || draft.evidence === 0} title={recordH.tooltip}>
                      <Icon of={Stamp} />
                      {recordH.label}
                    </Button>
                  </div>
                )}
              </div>
              <div className="fy-card__body fy-stack fy-stack--3">
                <HoldingsTable x={draft} label="Holdings typed in" />
                {draft.checks.map((c) => (
                  <p key={c.code} className="fy-small fy-secondary">
                    {c.message}. {c.fix}.
                  </p>
                ))}
                <EvidencePanel record={{ kind: "acra-extract", id: draft.id, etag: `"${draft.version}-draft"` }} draft mayAttach={mayDraft}
                  onChanged={(m) => {
                    setStatus(m);
                    void queries.invalidateQueries({ queryKey: ["acra-extracts"] });
                  }} />
              </div>
            </section>
          )}
          {earlier.length > 0 && (
            <section className="fy-card" aria-labelledby="acra-earlier-title">
              <div className="fy-card__head">
                <h2 className="fy-h3" id="acra-earlier-title">
                  Earlier business profiles
                </h2>
              </div>
              <ul className="fy-list" aria-labelledby="acra-earlier-title">
                {earlier.map((x) => (
                  <li key={x.id} className="fy-list__item">
                    <span className="fy-list__main">
                      <span className="fy-list__title">Business profile of {date(x.profile_on)}</span>
                      <span className="fy-list__meta">
                        {x.holdings.length} holding{x.holdings.length === 1 ? "" : "s"}, superseded
                      </span>
                    </span>
                  </li>
                ))}
              </ul>
            </section>
          )}
        </div>
      </div>
      {editing && (
        <ExtractForm draft={editing === "draft" ? draft : null}
          start={editing === "draft" && draft ? { profile_on: draft.profile_on, holdings: draft.holdings.map((h) => ({ holder_id: h.holder_id, class_account_id: h.class_account_id, shares: h.shares })), note: draft.note } : start}
          onClose={() => setEditing(null)} onDone={setStatus} />
      )}
    </div>
  );
}
