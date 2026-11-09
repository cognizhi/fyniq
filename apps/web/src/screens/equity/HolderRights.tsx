import { useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, ClockAlert, Hourglass, PencilLine, Plus, Send, Trash2, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { EvidencePanel } from "../accounting/JournalEvidence";

type Detail = components["schemas"]["HolderDetail"];
type Rights = components["schemas"]["InvestorRightsView"];
type RightsDraft = components["schemas"]["InvestorRightsDraft"];
type Entry = components["schemas"]["ControllerEntryView"];
type EntryDraft = components["schemas"]["ControllerEntryDraft"];
type Ground = EntryDraft["grounds"][number];
export type StatutoryTests = components["schemas"]["StatutoryTests"];
type TestLine = components["schemas"]["ControllerTestView"];

export async function statutoryTests(): Promise<StatutoryTests> {
  const { data, error } = await api.GET("/v1/cap-table/statutory-tests", {});
  if (error) throw error;
  return data;
}

const groundWords: Record<Ground, string> = {
  shares: "more than 25% of the shares",
  votes: "more than 25% of the votes",
  board: "appoints or removes a majority of the board",
  influence: "significant influence or control",
};

function yes(b: boolean): string {
  return b ? "Yes" : "No";
}

// Waiting is a draft's state: a link to the approval waiting, or its buttons to change it and ask.
function DraftActions({ approvalID, mayDraft, busy, evidence, onChange, onAsk, askLabel, askTip }: {
  approvalID?: string;
  mayDraft: boolean;
  busy: boolean;
  evidence: number;
  onChange: () => void;
  onAsk: () => void;
  askLabel: string;
  askTip: string;
}) {
  if (approvalID) {
    return (
      <span className="fy-chip fy-chip--neutral">
        <Icon of={Hourglass} />
        <Link to="/approvals/$approvalId" params={{ approvalId: approvalID }}>
          Waiting for approval
        </Link>
      </span>
    );
  }
  if (!mayDraft) return null;
  return (
    <div className="fy-row gap-2">
      <Button size="sm" variant="ghost" onClick={onChange}>
        <Icon of={PencilLine} />
        Change
      </Button>
      <Button size="sm" onClick={onAsk} disabled={busy || evidence === 0} title={askTip}>
        <Icon of={Send} />
        {askLabel}
      </Button>
    </div>
  );
}

function RightsList({ r }: { r: RightsDraft }) {
  return (
    <dl className="fy-dl fy-dl--3">
      <div>
        <dt>Board seats</dt>
        <dd>{r.board_seats === 0 ? "None" : `Appoints ${r.board_seats}`}</dd>
      </div>
      <div>
        <dt>Observer</dt>
        <dd>{yes(r.observer)}</dd>
      </div>
      <div>
        <dt>Board majority</dt>
        <dd>{r.board_majority ? "Appoints or removes a majority of the board" : "No"}</dd>
      </div>
      <div>
        <dt>Pro-rata</dt>
        <dd>{r.pro_rata ? "May take up its share of a later round" : "No"}</dd>
      </div>
      <div className="fy-span-2">
        <dt>Information rights</dt>
        <dd>{r.information || "None"}</dd>
      </div>
      <div className="fy-span-3">
        <dt>Consent matters</dt>
        <dd>{r.consents.length === 0 ? "None" : r.consents.join("; ")}</dd>
      </div>
      <div className="fy-span-3">
        <dt>From the agreements</dt>
        <dd>
          {r.agreements}, from {date(r.from)}
        </dd>
      </div>
      {r.note && (
        <div className="fy-span-3">
          <dt>Note</dt>
          <dd>{r.note}</dd>
        </div>
      )}
    </dl>
  );
}

const blankRights: RightsDraft = { from: "", board_seats: 0, observer: false, board_majority: false, pro_rata: false, information: "", consents: [], agreements: "", note: "" };

// RightsForm drafts a holder's rights from the agreements (FR-EQ-22), or changes the draft at its version.
function RightsForm({ holderID, draft, start, onClose, onDone }: { holderID: string; draft: Rights | null; start: RightsDraft; onClose: () => void; onDone: (text: string) => void }) {
  const id = useId();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const [form, setForm] = useState<RightsDraft>({ ...blankRights, ...start, consents: [...start.consents] });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const body: RightsDraft = { ...form, consents: form.consents.map((c) => c.trim()).filter(Boolean) };
    if (!body.information?.trim()) delete body.information;
    if (!body.note?.trim()) delete body.note;
    const out = await guarded(confirm, () =>
      draft
        ? api.PUT("/v1/investor-rights/{rights_id}", { params: { path: { rights_id: draft.id }, header: { "If-Match": `"${draft.version}-draft"`, "Idempotency-Key": key } }, body })
        : api.POST("/v1/investor-rights", { params: { header: { "Idempotency-Key": key } }, body: { holder_id: holderID, ...body } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["holder", holderID] });
    await queries.invalidateQueries({ queryKey: ["statutory-tests"] });
    onDone(draft ? "The draft is saved." : "The rights are drafted. Attach the agreements, then ask for approval.");
    onClose();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} wide eyebrow="Investor rights" title={draft ? "Change the draft" : "Record the rights from the agreements"}
      description="Recorded once another person approves them, with the agreements attached. They are recorded, never computed.">
      {problem && <Refusal problem={problem} title="The rights were not saved" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-form fy-form--2">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-agreements`}>
              From the agreements
            </label>
            <input className="fy-input" id={`${id}-agreements`} maxLength={500} value={form.agreements} onChange={(e) => setForm({ ...form, agreements: e.target.value })}
              aria-describedby={`${id}-agreements-help`} />
            <span className="fy-help" id={`${id}-agreements-help`}>
              Such as: Side letter of 18 Jun 2026
            </span>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-from`}>
              In force from
            </label>
            <input className="fy-input" id={`${id}-from`} type="date" value={form.from} onChange={(e) => setForm({ ...form, from: e.target.value })} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-seats`}>
              Directors they may appoint
            </label>
            <input className="fy-input fy-num" id={`${id}-seats`} type="number" min={0} max={20} value={form.board_seats}
              onChange={(e) => setForm({ ...form, board_seats: Number(e.target.value || 0) })} />
          </div>
          <div className="fy-field fy-stack fy-stack--2">
            {(
              [
                ["observer", "A board observer seat"],
                ["board_majority", "Appoints or removes a majority of the board"],
                ["pro_rata", "A pro-rata right in later rounds"],
              ] as const
            ).map(([k, label]) => (
              <label key={k} className="fy-row gap-2 fy-label">
                <input type="checkbox" checked={form[k]} onChange={(e) => setForm({ ...form, [k]: e.target.checked })} />
                {label}
              </label>
            ))}
          </div>
          <div className="fy-field fy-field--full">
            <label className="fy-label" htmlFor={`${id}-info`}>
              Information rights (optional)
            </label>
            <textarea className="fy-textarea" id={`${id}-info`} maxLength={1000} value={form.information ?? ""} onChange={(e) => setForm({ ...form, information: e.target.value })}
              aria-describedby={`${id}-info-help`} />
            <span className="fy-help" id={`${id}-info-help`}>
              What and when, such as: Quarterly management accounts within 45 days
            </span>
          </div>
          <fieldset className="fy-field fy-field--full">
            <legend className="fy-label">Matters that need their consent</legend>
            <div className="fy-stack fy-stack--2">
              {form.consents.map((c, i) => (
                <div key={i} className="fy-row gap-2">
                  <input className="fy-input" aria-label={`Consent matter ${i + 1}`} maxLength={200} value={c}
                    onChange={(e) => setForm({ ...form, consents: form.consents.map((x, j) => (j === i ? e.target.value : x)) })} />
                  <Button variant="ghost" size="sm" type="button" aria-label={`Remove consent matter ${i + 1}`}
                    onClick={() => setForm({ ...form, consents: form.consents.filter((_, j) => j !== i) })}>
                    <Icon of={Trash2} />
                  </Button>
                </div>
              ))}
              {form.consents.length < 20 && (
                <Button variant="ghost" size="sm" type="button" style={{ alignSelf: "flex-start" }} onClick={() => setForm({ ...form, consents: [...form.consents, ""] })}>
                  <Icon of={Plus} />
                  Add a consent matter
                </Button>
              )}
              <span className="fy-help">A board majority or any consent matter could make them a registrable controller: the company secretary weighs it.</span>
            </div>
          </fieldset>
          <div className="fy-field fy-field--full">
            <label className="fy-label" htmlFor={`${id}-note`}>
              Note (optional)
            </label>
            <textarea className="fy-textarea" id={`${id}-note`} maxLength={1000} value={form.note ?? ""} onChange={(e) => setForm({ ...form, note: e.target.value })} />
          </div>
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy}>
            {draft ? "Save the draft" : "Draft the rights"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// RightsCard is a holder's rights from the agreements (FR-EQ-22): those in force, a draft with its agreements and its
// approval, and whether they could confer control.
export function RightsCard({ h, mayDraft, onStatus }: { h: Detail; mayDraft: boolean; onStatus: (text: string) => void }) {
  const confirm = useConfirm();
  const queries = useQueryClient();
  const [editing, setEditing] = useState<"new" | "draft" | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const draft = h.rights_history.find((r) => r.status === "draft") ?? null;
  const draftH = helpText("investor-rights.draft");
  const askH = helpText("investor-rights.ask");
  const ask = async () => {
    if (!draft) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/investor-rights/{rights_id}/approval", { params: { path: { rights_id: draft.id }, header: { "If-Match": `"${draft.version}-draft"`, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onStatus("Asked for approval. Another Owner or the Controller records the rights, never the holder.");
    await queries.invalidateQueries({ queryKey: ["holder", h.contact_id] });
  };
  return (
    <section className="fy-card" aria-labelledby="holder-rights-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="holder-rights-title" title={helpText("holder.rights").tooltip}>
          Rights from the agreements
        </h2>
        <div className="fy-row gap-2">
          {h.rights?.confers_control && (
            <span className="fy-chip fy-chip--warning">
              <Icon of={TriangleAlert} />
              Could confer control
            </span>
          )}
          {mayDraft && !draft && (
            <Button size="sm" variant="secondary" onClick={() => setEditing("new")} title={draftH.tooltip}>
              <Icon of={PencilLine} />
              {h.rights ? "Change the rights" : draftH.label}
            </Button>
          )}
        </div>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        {problem && <Refusal problem={problem} title="The approval was not asked for" />}
        {h.rights ? (
          <RightsList r={h.rights.rights} />
        ) : (
          <p className="fy-small fy-muted">No rights are recorded. An Accountant records them from the agreements; another person approves.</p>
        )}
        {draft && (
          <div className="fy-stack fy-stack--3" aria-label="Drafted rights" role="group">
            <div className="fy-row fy-row--between">
              <h3 className="fy-strong">Draft</h3>
              <DraftActions approvalID={draft.approval_id} mayDraft={mayDraft} busy={busy} evidence={draft.evidence} onChange={() => setEditing("draft")}
                onAsk={() => void ask()} askLabel={askH.label} askTip={askH.tooltip} />
            </div>
            <RightsList r={draft.rights} />
            {draft.checks.map((c) => (
              <p key={c.code} className="fy-small fy-secondary">
                {c.message}. {c.fix}.
              </p>
            ))}
            <EvidencePanel record={{ kind: "investor-rights", id: draft.id, etag: `"${draft.version}-draft"` }} draft mayAttach={mayDraft && !draft.approval_id}
              onChanged={(m) => {
                onStatus(m);
                void queries.invalidateQueries({ queryKey: ["holder", h.contact_id] });
              }} />
          </div>
        )}
      </div>
      {editing && (
        <RightsForm holderID={h.contact_id} draft={editing === "draft" ? draft : null} start={editing === "draft" && draft ? draft.rights : (h.rights?.rights ?? blankRights)}
          onClose={() => setEditing(null)} onDone={onStatus} />
      )}
    </section>
  );
}

// standingChip is where the register stands for a holder the controller test names, with its icon and words.
function StandingChip({ t }: { t: TestLine }) {
  switch (t.standing) {
    case "to_enter":
      return (
        <span className="fy-chip fy-chip--serious">
          <Icon of={ClockAlert} />
          Enter in the register by {date(t.company_register_due)}
        </span>
      );
    case "to_lodge":
      return (
        <span className="fy-chip fy-chip--serious">
          <Icon of={ClockAlert} />
          Update ACRA's register by {date(t.central_register_due)}
        </span>
      );
    case "registered":
      return (
        <span className="fy-chip fy-chip--good">
          <Icon of={CircleCheck} />
          In both registers
        </span>
      );
    case "not_assessed":
      return (
        <span className="fy-chip fy-chip--warning">
          <Icon of={TriangleAlert} />
          Not assessed
        </span>
      );
    default:
      return <span className="fy-chip fy-chip--neutral">Not a controller</span>;
  }
}

// testWords say what the cap table's test found for a holder.
function testWords(t: TestLine | undefined): string {
  if (!t) return "Not a registrable controller on the cap table's test: 25% or less of the shares and of the votes, and no recorded rights that could confer control.";
  const over = t.shares && t.votes ? `${t.ownership}% of the shares and ${t.voting}% of the votes` : t.shares ? `${t.ownership}% of the shares` : t.votes ? `${t.voting}% of the votes` : "";
  if (over) return `Holds ${over} since ${date(t.since)}, more than 25%: a registrable controller.`;
  return "Their recorded rights (a board majority or consent matters) could confer control: the company secretary concludes whether they do.";
}

function EntryList({ e, noticeDue }: { e: EntryDraft; noticeDue?: string }) {
  return (
    <dl className="fy-dl fy-dl--3">
      <div className="fy-span-3">
        <dt>Conclusion</dt>
        <dd>{e.controller ? `A registrable controller: ${e.grounds.map((g) => groundWords[g]).join("; ")}` : `Not a registrable controller: ${e.reason}`}</dd>
      </div>
      <div>
        <dt>{e.controller ? "A controller since" : "Concluded on"}</dt>
        <dd>{date(e.since)}</dd>
      </div>
      {e.controller && (
        <>
          <div>
            <dt>Company's register</dt>
            <dd>{e.company_register_on ? date(e.company_register_on) : "Not entered yet"}</dd>
          </div>
          <div>
            <dt>ACRA's central register</dt>
            <dd>{e.central_register_on ? date(e.central_register_on) : "Not updated yet"}</dd>
          </div>
          <div className="fy-span-2">
            <dt>Yearly notices</dt>
            <dd>
              {e.notices.length === 0
                ? "None sent yet"
                : e.notices.map((n) => `Sent ${date(n.sent_on)}, ${n.replied_on ? `replied ${date(n.replied_on)}` : "no reply yet"}`).join("; ")}
            </dd>
          </div>
          {noticeDue && (
            <div>
              <dt>Next notice due</dt>
              <dd>{date(noticeDue)}</dd>
            </div>
          )}
          {e.ceased_on && (
            <div>
              <dt>Ceased</dt>
              <dd>{date(e.ceased_on)}</dd>
            </div>
          )}
        </>
      )}
      {e.note && (
        <div className="fy-span-3">
          <dt>Note</dt>
          <dd>{e.note}</dd>
        </div>
      )}
    </dl>
  );
}

// ControllerForm drafts the company's conclusion about a holder (FR-EQ-25), or changes the draft at its version.
function ControllerForm({ holderID, draft, start, onClose, onDone }: { holderID: string; draft: Entry | null; start: EntryDraft; onClose: () => void; onDone: (text: string) => void }) {
  const id = useId();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const [form, setForm] = useState<EntryDraft>({ ...start, grounds: [...start.grounds], notices: start.notices.map((n) => ({ ...n })) });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const day = (k: "since" | "company_register_on" | "central_register_on" | "ceased_on", label: string, help?: string) => (
    <div className="fy-field">
      <label className="fy-label" htmlFor={`${id}-${k}`}>
        {label}
      </label>
      <input className="fy-input" id={`${id}-${k}`} type="date" value={form[k] ?? ""} onChange={(e) => setForm({ ...form, [k]: e.target.value })}
        aria-describedby={help ? `${id}-${k}-help` : undefined} />
      {help && (
        <span className="fy-help" id={`${id}-${k}-help`}>
          {help}
        </span>
      )}
    </div>
  );
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const body: EntryDraft = form.controller
      ? { controller: true, grounds: form.grounds, since: form.since, notices: form.notices.filter((n) => n.sent_on).map((n) => (n.replied_on ? n : { sent_on: n.sent_on })) }
      : { controller: false, grounds: [], reason: form.reason?.trim() ?? "", since: form.since, notices: [] };
    if (form.controller) {
      for (const k of ["company_register_on", "central_register_on", "ceased_on"] as const) if (form[k]) body[k] = form[k];
    }
    if (form.note?.trim()) body.note = form.note.trim();
    const out = await guarded(confirm, () =>
      draft
        ? api.PUT("/v1/controller-entries/{entry_id}", { params: { path: { entry_id: draft.id }, header: { "If-Match": `"${draft.version}-draft"`, "Idempotency-Key": key } }, body })
        : api.POST("/v1/controller-entries", { params: { header: { "Idempotency-Key": key } }, body: { holder_id: holderID, ...body } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["holder", holderID] });
    await queries.invalidateQueries({ queryKey: ["statutory-tests"] });
    onDone(draft ? "The draft is saved." : "The entry is drafted. Attach the company secretary's confirmation, then ask for approval.");
    onClose();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} wide eyebrow="Registrable controller" title={draft ? "Change the draft" : "Record the company's conclusion"}
      description="As the register of registrable controllers records it. The company secretary confirms each conclusion; attach their confirmation.">
      {problem && <Refusal problem={problem} title="The entry was not saved" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <fieldset className="fy-field">
          <legend className="fy-label">Conclusion</legend>
          <div className="fy-segmented" role="radiogroup" aria-label="Conclusion">
            {[true, false].map((v) => (
              <button key={String(v)} type="button" role="radio" aria-checked={form.controller === v} onClick={() => setForm({ ...form, controller: v })}>
                {v ? "A registrable controller" : "Not a controller"}
              </button>
            ))}
          </div>
        </fieldset>
        {form.controller ? (
          <div className="fy-form fy-form--2">
            <fieldset className="fy-field fy-field--full">
              <legend className="fy-label">Grounds</legend>
              <div className="fy-stack fy-stack--2">
                {(Object.keys(groundWords) as Ground[]).map((g) => (
                  <label key={g} className="fy-row gap-2 fy-label">
                    <input type="checkbox" checked={form.grounds.includes(g)}
                      onChange={(e) => setForm({ ...form, grounds: e.target.checked ? [...form.grounds, g] : form.grounds.filter((x) => x !== g) })} />
                    {groundWords[g].charAt(0).toUpperCase() + groundWords[g].slice(1)}
                  </label>
                ))}
              </div>
            </fieldset>
            {day("since", "A controller since")}
            {day("company_register_on", "Entered in the company's register", "Within 7 days")}
            {day("central_register_on", "ACRA's central register updated", "Within 2 business days after the company's")}
            {day("ceased_on", "Ceased (optional)")}
            <fieldset className="fy-field fy-field--full">
              <legend className="fy-label">Yearly notices</legend>
              <div className="fy-stack fy-stack--2">
                {form.notices.map((n, i) => (
                  <div key={i} className="fy-row fy-row--wrap gap-2">
                    <input className="fy-input" type="date" aria-label={`Notice ${i + 1}: sent on`} value={n.sent_on}
                      onChange={(e) => setForm({ ...form, notices: form.notices.map((x, j) => (j === i ? { ...x, sent_on: e.target.value } : x)) })} />
                    <input className="fy-input" type="date" aria-label={`Notice ${i + 1}: replied on`} value={n.replied_on ?? ""}
                      onChange={(e) => setForm({ ...form, notices: form.notices.map((x, j) => (j === i ? { ...x, replied_on: e.target.value } : x)) })} />
                    <Button variant="ghost" size="sm" type="button" aria-label={`Remove notice ${i + 1}`} onClick={() => setForm({ ...form, notices: form.notices.filter((_, j) => j !== i) })}>
                      <Icon of={Trash2} />
                    </Button>
                  </div>
                ))}
                <Button variant="ghost" size="sm" type="button" style={{ alignSelf: "flex-start" }} onClick={() => setForm({ ...form, notices: [...form.notices, { sent_on: "" }] })}>
                  <Icon of={Plus} />
                  Add a notice
                </Button>
                <span className="fy-help">At least once a year; the reply is due within 30 days.</span>
              </div>
            </fieldset>
          </div>
        ) : (
          <div className="fy-form fy-form--2">
            <div className="fy-field fy-field--full">
              <label className="fy-label" htmlFor={`${id}-reason`}>
                Why they are not a controller
              </label>
              <textarea className="fy-textarea" id={`${id}-reason`} maxLength={1000} value={form.reason ?? ""} onChange={(e) => setForm({ ...form, reason: e.target.value })}
                aria-describedby={`${id}-reason-help`} />
              <span className="fy-help" id={`${id}-reason-help`}>
                As the company secretary concluded, such as: the consent matters protect the investment and do not direct the company
              </span>
            </div>
            {day("since", "Concluded on")}
          </div>
        )}
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
            {draft ? "Save the draft" : "Draft the entry"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// ControllerCard is whether a holder is a registrable controller (FR-EQ-25): the cap table's test, where the registers
// stand, the entry in force with its notices, and a draft with the company secretary's confirmation and its approval.
export function ControllerCard({ h, tests, mayDraft, onStatus }: { h: Detail; tests?: StatutoryTests; mayDraft: boolean; onStatus: (text: string) => void }) {
  const confirm = useConfirm();
  const queries = useQueryClient();
  const [editing, setEditing] = useState<"new" | "draft" | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const t = tests?.controllers.find((x) => x.holder_id === h.contact_id);
  const lapsed = tests?.lapsed.some((x) => x.holder_id === h.contact_id);
  const draft = h.controller_history.find((x) => x.status === "draft") ?? null;
  const draftH = helpText("controller-entry.draft");
  const askH = helpText("controller-entry.ask");
  const fresh: EntryDraft = {
    controller: Boolean(t?.shares || t?.votes),
    grounds: [...(t?.shares ? (["shares"] as const) : []), ...(t?.votes ? (["votes"] as const) : [])],
    since: t?.since ?? "",
    notices: [],
  };
  const ask = async () => {
    if (!draft) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/controller-entries/{entry_id}/approval", { params: { path: { entry_id: draft.id }, header: { "If-Match": `"${draft.version}-draft"`, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onStatus("Asked for approval. Another Owner or the Controller records the entry, never the holder.");
    await queries.invalidateQueries({ queryKey: ["holder", h.contact_id] });
  };
  if (!t && !h.controller && !draft && !lapsed) {
    return (
      <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-labelledby="holder-controller-title">
        <h2 className="fy-h3" id="holder-controller-title" title={helpText("holder.controller").tooltip}>
          Registrable controller
        </h2>
        <p className="fy-small fy-muted">{testWords(undefined)}</p>
      </section>
    );
  }
  return (
    <section className="fy-card" aria-labelledby="holder-controller-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="holder-controller-title" title={helpText("holder.controller").tooltip}>
          Registrable controller
        </h2>
        <div className="fy-row gap-2">
          {t && <StandingChip t={t} />}
          {mayDraft && !draft && (
            <Button size="sm" variant="secondary" onClick={() => setEditing("new")} title={draftH.tooltip}>
              <Icon of={PencilLine} />
              {h.controller ? "Update the entry" : draftH.label}
            </Button>
          )}
        </div>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        {problem && <Refusal problem={problem} title="The approval was not asked for" />}
        <p className="fy-small">{testWords(t)}</p>
        {lapsed && (
          <p className="fy-small">
            <Icon of={TriangleAlert} /> No longer more than 25% of the shares or votes: record the day they ceased, unless other grounds keep them a controller.
          </p>
        )}
        {h.controller ? <EntryList e={h.controller.entry} noticeDue={t?.notice_due} /> : <p className="fy-small fy-muted">No conclusion is recorded yet.</p>}
        {draft && (
          <div className="fy-stack fy-stack--3" aria-label="Drafted entry" role="group">
            <div className="fy-row fy-row--between">
              <h3 className="fy-strong">Draft</h3>
              <DraftActions approvalID={draft.approval_id} mayDraft={mayDraft} busy={busy} evidence={draft.evidence} onChange={() => setEditing("draft")}
                onAsk={() => void ask()} askLabel={askH.label} askTip={askH.tooltip} />
            </div>
            <EntryList e={draft.entry} />
            {draft.checks.map((c) => (
              <p key={c.code} className="fy-small fy-secondary">
                {c.message}. {c.fix}.
              </p>
            ))}
            <EvidencePanel record={{ kind: "controller-entry", id: draft.id, etag: `"${draft.version}-draft"` }} draft mayAttach={mayDraft && !draft.approval_id}
              onChanged={(m) => {
                onStatus(m);
                void queries.invalidateQueries({ queryKey: ["holder", h.contact_id] });
              }} />
          </div>
        )}
      </div>
      {editing && (
        <ControllerForm holderID={h.contact_id} draft={editing === "draft" ? draft : null}
          start={editing === "draft" && draft ? draft.entry : (h.controller?.entry ?? fresh)} onClose={() => setEditing(null)} onDone={onStatus} />
      )}
    </section>
  );
}
