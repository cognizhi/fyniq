import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { CircleCheck, Hourglass, PencilLine, Plus, Send, Trash2 } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { EvidencePanel } from "../accounting/JournalEvidence";
import { ControllerCard, RightsCard, statutoryTests } from "./HolderRights";
import { formWords, listInstruments } from "./instrumentWords";
import { EquityTabs, holdsText, kindText } from "./Holders";

type Detail = components["schemas"]["HolderDetail"];
type Record_ = components["schemas"]["HolderRecordView"];
type Draft = components["schemas"]["HolderRecordDraft"];

// Who drafts a holder record: C/P on the cap table, the Accountant (Appendix D); an Owner or the Controller approves.
const drafters = ["accountant"];

const idTypes: Record<string, string> = { nric: "NRIC", fin: "FIN", passport: "Passport", uen: "UEN", foreign_registration: "Foreign registration" };

async function holderOf(id: string): Promise<Detail> {
  const { data, error } = await api.GET("/v1/holders/{contact_id}", { params: { path: { contact_id: id } } });
  if (error) throw error;
  return data;
}

// identifierOf writes a record's identifier as the page shows it.
function identifierOf(d: Draft): string {
  if (d.kind === "individual" && d.id_last4) return `${idTypes[d.id_type ?? ""] ?? "ID"} ending ${d.id_last4}`;
  if (d.kind === "entity" && d.registration_number) return `${idTypes[d.id_type ?? ""] ?? "Registration"} ${d.registration_number}`;
  return "Not given";
}

function Details({ r }: { r: Record_ }) {
  const d = r.details;
  if (r.masked) return <p className="fy-small fy-muted">Shown masked: the identifier, address, people to contact and note are left out.</p>;
  return (
    <dl className="fy-dl fy-dl--3">
      <div>
        <dt>Kind</dt>
        <dd>{d.kind === "entity" ? "Entity" : "Individual"}</dd>
      </div>
      <div>
        <dt>Identifier</dt>
        <dd className="fy-mono">{identifierOf(d)}</dd>
      </div>
      <div>
        <dt>{d.kind === "entity" ? "Place of incorporation" : "Nationality"}</dt>
        <dd>{d.nationality || "Not given"}</dd>
      </div>
      <div className="fy-span-2">
        <dt>Registered address</dt>
        <dd>{d.address || "Not given"}</dd>
      </div>
      <div>
        <dt>People to contact</dt>
        <dd>{(d.contact_people ?? []).map((p) => [p.name, p.role, p.email].filter(Boolean).join(", ")).join("; ") || "None"}</dd>
      </div>
      {d.note && (
        <div className="fy-span-3">
          <dt>Note</dt>
          <dd>{d.note}</dd>
        </div>
      )}
    </dl>
  );
}

const blank: Draft = { kind: "individual", id_type: "nric", id_last4: "", nationality: "", address: "", contact_people: [], note: "" };

// RecordForm drafts a holder record, or changes the draft at its version (FR-EQ-15). An individual's identifier is its
// last 4 characters only.
function RecordForm({ contactId, draft, start, onClose, onDone }: { contactId: string; draft: Record_ | null; start: Draft; onClose: () => void; onDone: (text: string) => void }) {
  const id = useId();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const [form, setForm] = useState<Draft>({ ...blank, ...start, contact_people: start.contact_people?.length ? start.contact_people : [{ name: "" }] });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const set = (k: keyof Draft, v: string) => setForm({ ...form, [k]: v });
  const individual = form.kind === "individual";
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const body: Draft = {
      kind: form.kind,
      ...(form.id_type ? { id_type: form.id_type } : {}),
      ...(individual && form.id_last4 ? { id_last4: form.id_last4.trim() } : {}),
      ...(!individual && form.registration_number ? { registration_number: form.registration_number.trim() } : {}),
      ...(form.nationality ? { nationality: form.nationality.trim() } : {}),
      ...(form.address ? { address: form.address.trim() } : {}),
      ...(form.note ? { note: form.note.trim() } : {}),
      contact_people: (form.contact_people ?? []).filter((p) => p.name.trim()),
    };
    if (individual && !form.id_last4) delete body.id_type;
    if (!individual && !form.registration_number) delete body.id_type;
    const out = await guarded(confirm, () =>
      draft
        ? api.PUT("/v1/holder-records/{record_id}", {
            params: { path: { record_id: draft.id }, header: { "If-Match": `"${draft.version}-draft"`, "Idempotency-Key": key } },
            body,
          })
        : api.POST("/v1/holder-records", { params: { header: { "Idempotency-Key": key } }, body: { contact_id: contactId, ...body } }),
    );
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    await queries.invalidateQueries({ queryKey: ["holder", contactId] });
    await queries.invalidateQueries({ queryKey: ["holders"] });
    onDone(draft ? "The draft is saved." : "The record is drafted. Attach the identity document, then ask for approval.");
    onClose();
  };
  const people = form.contact_people ?? [];
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} wide eyebrow="Holder record" title={draft ? "Change the draft" : "Draft the holder's details"}
      description="Recorded once another person approves it. Fyniq keeps only the last 4 characters of an individual's identifier.">
      {problem && <Refusal problem={problem} title="The record was not saved" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-form fy-form--2">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-kind`}>
              Kind
            </label>
            <select className="fy-select" id={`${id}-kind`} value={form.kind}
              onChange={(e) => setForm({ ...form, kind: e.target.value as Draft["kind"], id_type: e.target.value === "entity" ? "uen" : "nric" })}>
              <option value="individual">Individual</option>
              <option value="entity">Entity</option>
            </select>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-type`}>
              Identifier
            </label>
            <select className="fy-select" id={`${id}-type`} value={form.id_type} onChange={(e) => set("id_type", e.target.value)}>
              {(individual ? ["nric", "fin", "passport"] : ["uen", "foreign_registration"]).map((t) => (
                <option key={t} value={t}>
                  {idTypes[t]}
                </option>
              ))}
            </select>
          </div>
          {individual ? (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-last4`}>
                Last 4 characters
              </label>
              <input className="fy-input fy-mono" id={`${id}-last4`} maxLength={4} value={form.id_last4 ?? ""} onChange={(e) => set("id_last4", e.target.value.toUpperCase())}
                aria-describedby={`${id}-last4-help`} autoComplete="off" />
              <span className="fy-help" id={`${id}-last4-help`}>
                Such as 567D. The full number stays in ACRA's register and the identity document.
              </span>
            </div>
          ) : (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-reg`}>
                Registration number
              </label>
              <input className="fy-input fy-mono" id={`${id}-reg`} maxLength={30} value={form.registration_number ?? ""} onChange={(e) => set("registration_number", e.target.value.toUpperCase())} />
            </div>
          )}
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-nat`}>
              {individual ? "Nationality" : "Place of incorporation"}
            </label>
            <input className="fy-input" id={`${id}-nat`} maxLength={60} value={form.nationality ?? ""} onChange={(e) => set("nationality", e.target.value)} />
          </div>
          <div className="fy-field fy-field--full">
            <label className="fy-label" htmlFor={`${id}-addr`}>
              Registered address
            </label>
            <textarea className="fy-textarea" id={`${id}-addr`} maxLength={500} value={form.address ?? ""} onChange={(e) => set("address", e.target.value)} />
          </div>
          {people.map((p, i) => (
            <fieldset key={i} className="fy-field fy-field--full">
              <legend className="fy-label">Person to contact {people.length > 1 ? i + 1 : ""}</legend>
              <div className="fy-row fy-row--wrap gap-2">
                {(["name", "role", "email"] as const).map((k) => (
                  <input key={k} className="fy-input" aria-label={`Person to contact ${i + 1}: ${k}`} placeholder={{ name: "Name", role: "Role", email: "Email" }[k]} value={p[k] ?? ""}
                    onChange={(e) => setForm({ ...form, contact_people: people.map((x, j) => (j === i ? { ...x, [k]: e.target.value } : x)) })} />
                ))}
                {people.length > 1 && (
                  <Button variant="ghost" size="sm" type="button" aria-label={`Remove person to contact ${i + 1}`}
                    onClick={() => setForm({ ...form, contact_people: people.filter((_, j) => j !== i) })}>
                    <Icon of={Trash2} />
                  </Button>
                )}
              </div>
            </fieldset>
          ))}
          {people.length < 10 && (
            <div className="fy-field--full">
              <Button variant="ghost" size="sm" type="button" onClick={() => setForm({ ...form, contact_people: [...people, { name: "" }] })}>
                <Icon of={Plus} />
                Add a person to contact
              </Button>
            </div>
          )}
          <div className="fy-field fy-field--full">
            <label className="fy-label" htmlFor={`${id}-note`}>
              Note (optional)
            </label>
            <textarea className="fy-textarea" id={`${id}-note`} maxLength={1000} value={form.note ?? ""} onChange={(e) => set("note", e.target.value)} />
          </div>
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy}>
            {draft ? "Save the draft" : "Draft the record"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

const considerationWords: Record<Detail["issues"][number]["consideration"], string> = {
  cash: "Shares for cash",
  debt_capitalised: "Shares for a balance owed to them",
  services: "Shares for services",
  safe_or_note: "Shares from a SAFE or note",
};

// Investments are what a holder put in (FR-EQ-22): each share issue with its shares, price and amount, and each SAFE or
// note they hold, from the register of share issues and the instruments recorded.
function Investments({ h }: { h: Detail }) {
  const instruments = useQuery({ queryKey: ["instruments"], queryFn: listInstruments, retry: false });
  const held = (instruments.data ?? []).filter((i) => i.holder_id === h.contact_id);
  const none = h.issues.length === 0 && held.length === 0;
  return (
    <section className="fy-card" aria-labelledby="holder-issues-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="holder-issues-title" title={helpText("holder.investments").tooltip}>
          Investments
        </h2>
      </div>
      {none ? (
        <p className="fy-card__body fy-small fy-muted">No shares, SAFE or note is recorded for this holder.</p>
      ) : (
        <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
          <table className="fy-table fy-table--cards" aria-label="Investments">
            <thead>
              <tr>
                <th scope="col">Investment</th>
                <th scope="col">Date</th>
                <th scope="col" className="fy-num">
                  Shares
                </th>
                <th scope="col" className="fy-num">
                  Price
                </th>
                <th scope="col" className="fy-num">
                  Amount
                </th>
              </tr>
            </thead>
            <tbody>
              {h.issues.map((s) => (
                <tr key={s.id}>
                  <td data-card="title">
                    {considerationWords[s.consideration]} on {s.class_code}
                    <div className="fy-small fy-muted">{s.resolution_refs}</div>
                  </td>
                  <td data-card="meta">{date(s.effective_on)}</td>
                  <td className="fy-num">{Number(s.shares).toLocaleString("en-SG")}</td>
                  <td className="fy-num">{s.price}</td>
                  <td data-card="amount" className="fy-num">
                    {formatAmount(s.amount.amount)}
                  </td>
                </tr>
              ))}
              {held.map((i) => (
                <tr key={i.id}>
                  <td data-card="title">
                    <Link to="/equity/instruments/$instrumentId" params={{ instrumentId: i.id }}>
                      {formWords[i.form]}
                    </Link>
                    <div className="fy-small fy-muted">
                      {i.conversion ? "Converted" : i.status === "recorded" ? "Recorded" : "Draft"}
                      {i.valuation_cap ? ` · cap S$${formatAmount(i.valuation_cap)}` : ""}
                      {i.discount ? ` · ${i.discount}% discount` : ""}
                    </div>
                  </td>
                  <td data-card="meta">{date(i.signed_on)}</td>
                  <td className="fy-num">–</td>
                  <td className="fy-num">–</td>
                  <td data-card="amount" className="fy-num">
                    {formatAmount(i.amount)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

// HolderPage is Equity › Holders › a holder (docs/design/screens HolderRecord; FR-EQ-15, FR-EQ-22, FR-EQ-25): the record
// in force, a draft with its identity document and its approval, every record before, the rights from the agreements,
// whether they are a registrable controller, and the holder's share issues.
export function HolderPage() {
  const { contactId } = useParams({ strict: false }) as { contactId: string };
  const { session } = useSignedIn();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const q = useQuery({ queryKey: ["holder", contactId], queryFn: () => holderOf(contactId) });
  // The cap table's statutory tests read the whole register: a reader of their own holding only gets none (FR-EQ-25).
  const tests = useQuery({ queryKey: ["statutory-tests"], queryFn: statutoryTests, retry: false });
  const [editing, setEditing] = useState<"new" | "draft" | null>(null);
  const [status, setStatus] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const mayDraft = (session.roles ?? []).some((r) => drafters.includes(r));
  const draftH = helpText("holder-record.draft");
  const askH = helpText("holder-record.ask");
  const h = q.data;
  const draft = h?.history.find((r) => r.status === "draft") ?? null;
  const ask = async () => {
    if (!draft) return;
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/holder-records/{record_id}/approval", { params: { path: { record_id: draft.id }, header: { "If-Match": `"${draft.version}-draft"`, "Idempotency-Key": key } } }),
    );
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    setStatus("Asked for approval. Another Owner or the Controller approves it.");
    await queries.invalidateQueries({ queryKey: ["holder", contactId] });
  };
  return (
    <div className="fy-page fy-enter">
      <nav className="fy-breadcrumb" aria-label="Breadcrumb">
        <Link to="/equity/holders">Holders</Link>
        <span aria-hidden="true">›</span>
        <span>{h?.name ?? "Holder"}</span>
      </nav>
      {q.isError && <Refusal problem={problemOf(q.error)} title="The holder could not be shown" />}
      {h && (
        <>
          <div className="fy-pagehead">
            <div className="fy-pagehead__text">
              <h1 className="fy-h1">{h.name}</h1>
              <span className="fy-small fy-secondary">
                {kindText(h)}
                {h.identifier ? ` · ${h.identifier}` : ""}
              </span>
              <p className="fy-small">{holdsText(h)}</p>
            </div>
            {mayDraft && !draft && (
              <div className="fy-pagehead__actions">
                <Button variant="secondary" onClick={() => setEditing("new")} title={draftH.tooltip}>
                  <Icon of={PencilLine} />
                  {h.record ? "Change the details" : draftH.label}
                </Button>
              </div>
            )}
          </div>
          <EquityTabs current="holders" />
          <p className="fy-small" role="status">
            {status}
          </p>
          {problem && <Refusal problem={problem} title="The approval was not asked for" />}
          <section className="fy-card" aria-labelledby="holder-record-title">
            <div className="fy-card__head">
              <h2 className="fy-h3" id="holder-record-title">
                Details in force
              </h2>
              {h.record && (
                <span className="fy-chip fy-chip--good">
                  <Icon of={CircleCheck} />
                  Recorded {h.record.recorded_at ? date(h.record.recorded_at.slice(0, 10)) : ""}
                </span>
              )}
            </div>
            <div className="fy-card__body">
              {h.record ? <Details r={h.record} /> : <p className="fy-small fy-muted">No details are recorded yet. An Accountant drafts them; another person approves.</p>}
            </div>
          </section>
          {draft && (
            <section className="fy-card" aria-labelledby="holder-draft-title">
              <div className="fy-card__head">
                <h2 className="fy-h3" id="holder-draft-title">
                  Draft
                </h2>
                {draft.approval_id ? (
                  <span className="fy-chip fy-chip--neutral">
                    <Icon of={Hourglass} />
                    <Link to="/approvals/$approvalId" params={{ approvalId: draft.approval_id }}>
                      Waiting for approval
                    </Link>
                  </span>
                ) : (
                  mayDraft && (
                    <div className="fy-row gap-2">
                      <Button size="sm" variant="ghost" onClick={() => setEditing("draft")}>
                        <Icon of={PencilLine} />
                        Change
                      </Button>
                      <Button size="sm" onClick={() => void ask()} disabled={busy || draft.evidence === 0} title={askH.tooltip}>
                        <Icon of={Send} />
                        {askH.label}
                      </Button>
                    </div>
                  )
                )}
              </div>
              <div className="fy-card__body fy-stack fy-stack--3">
                <Details r={draft} />
                {draft.checks.map((c) => (
                  <p key={c.code} className="fy-small fy-secondary">
                    {c.message}. {c.fix}.
                  </p>
                ))}
                <EvidencePanel record={{ kind: "holder-record", id: draft.id, etag: `"${draft.version}-draft"` }} draft mayAttach={mayDraft && !draft.approval_id}
                  onChanged={(m) => {
                    setStatus(m);
                    void queries.invalidateQueries({ queryKey: ["holder", contactId] });
                  }} />
              </div>
            </section>
          )}
          {h.history.filter((r) => r.status === "superseded").length > 0 && (
            <section className="fy-card" aria-labelledby="holder-history-title">
              <div className="fy-card__head">
                <h2 className="fy-h3" id="holder-history-title">
                  Earlier records
                </h2>
              </div>
              <ul className="fy-list" aria-labelledby="holder-history-title">
                {h.history
                  .filter((r) => r.status === "superseded")
                  .map((r) => (
                    <li key={r.id} className="fy-list__item">
                      <span className="fy-list__main">
                        <span className="fy-list__title">{identifierOf(r.details)}</span>
                        <span className="fy-list__meta">Recorded {r.recorded_at ? dateTime(r.recorded_at) : ""}, superseded</span>
                      </span>
                    </li>
                  ))}
              </ul>
            </section>
          )}
          <RightsCard h={h} mayDraft={mayDraft} onStatus={setStatus} />
          <ControllerCard h={h} tests={tests.data} mayDraft={mayDraft} onStatus={setStatus} />
          <Investments h={h} />
          {editing && (
            <RecordForm contactId={contactId} draft={editing === "draft" ? draft : null}
              start={editing === "draft" && draft ? draft.details : h.record ? { ...h.record.details } : blank} onClose={() => setEditing(null)} onDone={setStatus} />
          )}
        </>
      )}
    </div>
  );
}
