import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { BadgeCheck, CalendarClock, CircleCheck, Hourglass, PencilLine, Plus, Send, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent, type ReactNode } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { EvidencePanel } from "../accounting/JournalEvidence";
import { statuses } from "../checks/data";
import { calendar, ItemChip } from "./StatutoryCalendar";

type Profile = components["schemas"]["CorporateProfile"];
type Detail = components["schemas"]["CorporateDetailView"];
type DetailDraft = components["schemas"]["CorporateDetailDraft"];
type Officer = components["schemas"]["CorporateOfficerView"];
type OfficerDraft = components["schemas"]["CorporateOfficerDraft"];
type Nominee = components["schemas"]["NomineeEntryView"];
type NomineeDraft = components["schemas"]["NomineeEntryDraft"];
type Approval = components["schemas"]["StatementsApprovalView"];
type ApprovalDraft = components["schemas"]["StatementsApprovalDraft"];
type Reconciliation = components["schemas"]["CorporateReconciliation"];
type Check = components["schemas"]["CheckOut"];
type Holder = components["schemas"]["Holder"];

// Who drafts the profile's records: C on "Corporate profile and registers", the Accountant and the Company secretary
// (Appendix D, rev 0.282, and its note).
const drafters = ["accountant", "company_secretary"];

// The profile's checks (C.2 #32 to #35), shown in both modes.
const checkKeys = ["corporate.secretary", "corporate.statements-approved", "corporate.acra-profile", "corporate.lodgements"];

const roleWords: Record<OfficerDraft["role"], string> = { director: "Director", secretary: "Company secretary", ceo: "CEO", auditor: "Auditor" };

async function profile(): Promise<Profile> {
  const { data, error } = await api.GET("/v1/corporate-profile", {});
  if (error) throw error;
  return data;
}

async function reconciliation(): Promise<Reconciliation> {
  const { data, error } = await api.GET("/v1/corporate-profile/reconciliation", {});
  if (error) throw error;
  return data;
}

async function checkOf(key: string): Promise<Check> {
  const { data, error } = await api.GET("/v1/checks/{key}", { params: { path: { key } } });
  if (error) throw error;
  return data;
}

async function shareholders(): Promise<Holder[]> {
  const { data, error } = await api.GET("/v1/holders", { params: { query: { page_size: 200 } } });
  if (error) throw error;
  return data.items ?? [];
}

// A record of the profile that may wait as a draft: its kind for the evidence panel, and how to ask for its approval.
type Kind = "corporate-detail" | "corporate-officer" | "nominee-entry" | "statements-approval";
type Draftable = { id: string; version: number; status: string; evidence: number; approval_id?: string; checks: { code: string; message: string; fix?: string }[] };

function askFor(kind: Kind, r: Draftable) {
  const header = { "If-Match": `"${r.version}-draft"`, "Idempotency-Key": newKey() };
  switch (kind) {
    case "corporate-detail":
      return api.POST("/v1/corporate-profile/details/{detail_id}/approval", { params: { path: { detail_id: r.id }, header } });
    case "corporate-officer":
      return api.POST("/v1/corporate-profile/officers/{version_id}/approval", { params: { path: { version_id: r.id }, header } });
    case "nominee-entry":
      return api.POST("/v1/corporate-profile/nominees/{nominee_id}/approval", { params: { path: { nominee_id: r.id }, header } });
    case "statements-approval":
      return api.POST("/v1/corporate-profile/statements-approvals/{statements_approval_id}/approval", {
        params: { path: { statements_approval_id: r.id }, header },
      });
  }
}

// DraftBlock is a draft of the profile: what it holds, its files, and asking another person to approve it, or the
// approval waiting.
function DraftBlock({ kind, r, mayDraft, onChange, onStatus, children }: {
  kind: Kind;
  r: Draftable;
  mayDraft: boolean;
  onChange: () => void;
  onStatus: (text: string) => void;
  children: ReactNode;
}) {
  const confirm = useConfirm();
  const queries = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const askH = helpText("corporate-profile.ask");
  const ask = async () => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () => askFor(kind, r));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onStatus("Asked for approval. The Owner or the Controller records it, never the one who asked.");
    await queries.invalidateQueries({ queryKey: ["corporate-profile"] });
  };
  return (
    <div className="fy-stack fy-stack--2" role="group" aria-label="Draft">
      {problem && <Refusal problem={problem} title="The approval was not asked for" />}
      <div className="fy-row fy-row--between fy-row--wrap gap-2">
        <span className="fy-chip fy-chip--neutral">
          <Icon of={PencilLine} />
          Draft
        </span>
        {r.approval_id ? (
          <span className="fy-chip fy-chip--neutral">
            <Icon of={Hourglass} />
            <Link to="/approvals/$approvalId" params={{ approvalId: r.approval_id }}>
              Waiting for approval
            </Link>
          </span>
        ) : (
          mayDraft && (
            <div className="fy-row gap-2">
              <Button size="sm" variant="ghost" onClick={onChange}>
                <Icon of={PencilLine} />
                Change
              </Button>
              <Button size="sm" onClick={() => void ask()} disabled={busy || r.evidence === 0} title={askH.tooltip}>
                <Icon of={Send} />
                {askH.label}
              </Button>
            </div>
          )
        )}
      </div>
      {children}
      {r.checks.map((c) => (
        <p key={c.code} className="fy-small fy-secondary">
          {c.message}.{c.fix ? ` ${c.fix}.` : ""}
        </p>
      ))}
      <EvidencePanel record={{ kind, id: r.id, etag: `"${r.version}-draft"` }} draft mayAttach={mayDraft && !r.approval_id}
        onChanged={(m) => {
          onStatus(m);
          void queries.invalidateQueries({ queryKey: ["corporate-profile"] });
        }} />
    </div>
  );
}

// The fields every form shares: a labelled input with its help, a date, a note.
function Field({ id, label, help, children }: { id: string; label: string; help?: string; children: ReactNode }) {
  return (
    <div className="fy-field">
      <label className="fy-label" htmlFor={id}>
        {label}
      </label>
      {children}
      {help && (
        <span className="fy-help" id={`${id}-help`}>
          {help}
        </span>
      )}
    </div>
  );
}

// useSave runs a form's write, stepped up, and closes it with its words once the profile is read again.
function useSave(onClose: () => void, onDone: (text: string) => void) {
  const confirm = useConfirm();
  const queries = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const save = async (call: () => Promise<{ data?: unknown; error?: unknown }>, words: string) => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, call);
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["corporate-profile"] });
    onDone(words);
    onClose();
  };
  return { busy, problem, save };
}

function FormButtons({ busy, onClose, label }: { busy: boolean; onClose: () => void; label: string }) {
  return (
    <div className="fy-row justify-end gap-2">
      <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
        Cancel
      </Button>
      <Button variant="primary" type="submit" disabled={busy}>
        {label}
      </Button>
    </div>
  );
}

// DetailForm drafts the company's name, registered office and SSIC code from a day (FR-CS-01).
function DetailForm({ draft, start, onClose, onDone }: { draft: Detail | null; start: DetailDraft; onClose: () => void; onDone: (text: string) => void }) {
  const id = useId();
  const [key] = useState(newKey);
  const [form, setForm] = useState<DetailDraft>({ ...start });
  const { busy, problem, save } = useSave(onClose, onDone);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const body: DetailDraft = { from: form.from, name: form.name.trim(), registered_office: form.registered_office.trim(), ssic: form.ssic.trim() };
    if (form.lodged_on) body.lodged_on = form.lodged_on;
    if (form.note?.trim()) body.note = form.note.trim();
    void save(
      () =>
        draft
          ? api.PUT("/v1/corporate-profile/details/{detail_id}", { params: { path: { detail_id: draft.id }, header: { "If-Match": `"${draft.version}-draft"`, "Idempotency-Key": key } }, body })
          : api.POST("/v1/corporate-profile/details", { params: { header: { "Idempotency-Key": key } }, body }),
      draft ? "The draft is saved." : "The details are drafted. Attach the BizFile+ acknowledgement or the business profile, then ask for approval.",
    );
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} wide eyebrow="Corporate profile" title={draft ? "Change the draft" : "Record the company's details"}
      description="As ACRA's business profile shows them, from the day they take effect. A change is a new record; the old one stays in the history.">
      {problem && <Refusal problem={problem} title="The details were not saved" />}
      <form className="fy-form fy-form--2" onSubmit={submit} noValidate>
        <Field id={`${id}-from`} label="From" help="Incorporation, or the day the name, office or SSIC code changed">
          <input className="fy-input" id={`${id}-from`} type="date" value={form.from} onChange={(e) => setForm({ ...form, from: e.target.value })} aria-describedby={`${id}-from-help`} />
        </Field>
        <Field id={`${id}-name`} label="Company name">
          <input className="fy-input" id={`${id}-name`} maxLength={200} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
        </Field>
        <Field id={`${id}-office`} label="Registered office">
          <input className="fy-input" id={`${id}-office`} maxLength={500} value={form.registered_office} onChange={(e) => setForm({ ...form, registered_office: e.target.value })} />
        </Field>
        <Field id={`${id}-ssic`} label="SSIC code" help="The primary activity's five digits, such as 62011">
          <input className="fy-input" id={`${id}-ssic`} inputMode="numeric" maxLength={5} value={form.ssic} onChange={(e) => setForm({ ...form, ssic: e.target.value })} aria-describedby={`${id}-ssic-help`} />
        </Field>
        <Field id={`${id}-lodged`} label="Lodged on (optional)" help="The day BizFile+ acknowledged the change: within 14 days for the registered office">
          <input className="fy-input" id={`${id}-lodged`} type="date" value={form.lodged_on ?? ""} onChange={(e) => setForm({ ...form, lodged_on: e.target.value })} aria-describedby={`${id}-lodged-help`} />
        </Field>
        <Field id={`${id}-note`} label="Note (optional)">
          <textarea className="fy-textarea" id={`${id}-note`} maxLength={1000} value={form.note ?? ""} onChange={(e) => setForm({ ...form, note: e.target.value })} />
        </Field>
        <div className="fy-field--full">
          <FormButtons busy={busy} onClose={onClose} label={draft ? "Save the draft" : "Draft the details"} />
        </div>
      </form>
    </Dialog>
  );
}

// OfficerForm drafts an appointment, or a later version of one: its cessation, its lodgement or a correction (FR-CS-01,
// FR-CS-06). An individual's identifier is its last 4 characters only (SEC-15).
function OfficerForm({ follows, draft, start, onClose, onDone }: { follows?: string; draft: Officer | null; start: OfficerDraft; onClose: () => void; onDone: (text: string) => void }) {
  const id = useId();
  const [key] = useState(newKey);
  const [form, setForm] = useState<OfficerDraft>({ ...start });
  const { busy, problem, save } = useSave(onClose, onDone);
  const firm = form.role === "auditor" && form.kind === "entity";
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const body: OfficerDraft = { role: form.role, name: form.name.trim(), kind: form.role === "auditor" ? form.kind : "individual", appointed_on: form.appointed_on, nominee: form.role === "director" && form.nominee };
    for (const k of ["member_user_id", "id_type", "id_last4", "registration_number", "ceased_on", "appointment_lodged_on", "cessation_lodged_on", "note"] as const) {
      const v = form[k]?.trim();
      if (v) body[k] = v as never;
    }
    if (body.nominee && form.nominator?.trim()) body.nominator = form.nominator.trim();
    void save(
      () =>
        draft
          ? api.PUT("/v1/corporate-profile/officers/{version_id}", { params: { path: { version_id: draft.id }, header: { "If-Match": `"${draft.version}-draft"`, "Idempotency-Key": key } }, body })
          : api.POST("/v1/corporate-profile/officers", { params: { header: { "Idempotency-Key": key } }, body: follows ? { officer_id: follows, ...body } : body }),
      draft ? "The draft is saved." : "The officer is drafted. Attach the consent to act, the resolution or the BizFile+ acknowledgement, then ask for approval.",
    );
  };
  const day = (k: "appointed_on" | "ceased_on" | "appointment_lodged_on" | "cessation_lodged_on", label: string, help?: string) => (
    <Field id={`${id}-${k}`} label={label} help={help}>
      <input className="fy-input" id={`${id}-${k}`} type="date" value={form[k] ?? ""} onChange={(e) => setForm({ ...form, [k]: e.target.value })}
        aria-describedby={help ? `${id}-${k}-help` : undefined} />
    </Field>
  );
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} wide eyebrow="Officers" title={draft ? "Change the draft" : follows ? "Record a change of this officer" : "Record an officer"}
      description="As ACRA's business profile lists them. Only the last 4 characters of an identifier are kept; the full number stays with ACRA and the identity document.">
      {problem && <Refusal problem={problem} title="The officer was not saved" />}
      <form className="fy-form fy-form--2" onSubmit={submit} noValidate>
        <Field id={`${id}-role`} label="Office">
          <select className="fy-select" id={`${id}-role`} value={form.role} disabled={Boolean(follows || draft)} onChange={(e) => setForm({ ...form, role: e.target.value as OfficerDraft["role"] })}>
            {(Object.keys(roleWords) as OfficerDraft["role"][]).map((r) => (
              <option key={r} value={r}>
                {roleWords[r]}
              </option>
            ))}
          </select>
        </Field>
        <Field id={`${id}-name`} label="Name">
          <input className="fy-input" id={`${id}-name`} maxLength={200} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
        </Field>
        {form.role === "auditor" && (
          <Field id={`${id}-kind`} label="The auditor is">
            <select className="fy-select" id={`${id}-kind`} value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value as OfficerDraft["kind"], id_type: undefined, id_last4: undefined, registration_number: undefined })}>
              <option value="individual">A person</option>
              <option value="entity">A firm</option>
            </select>
          </Field>
        )}
        {firm ? (
          <Field id={`${id}-reg`} label="The firm's UEN" help="Its 9 or 10 characters, such as T08LL1234A">
            <input className="fy-input" id={`${id}-reg`} maxLength={30} value={form.registration_number ?? ""} aria-describedby={`${id}-reg-help`}
              onChange={(e) => setForm({ ...form, id_type: e.target.value ? "uen" : undefined, registration_number: e.target.value })} />
          </Field>
        ) : (
          <>
            <Field id={`${id}-idtype`} label="Identifier (optional)">
              <select className="fy-select" id={`${id}-idtype`} value={form.id_type ?? ""} onChange={(e) => setForm({ ...form, id_type: (e.target.value || undefined) as OfficerDraft["id_type"] })}>
                <option value="">None</option>
                <option value="nric">NRIC</option>
                <option value="fin">FIN</option>
                <option value="passport">Passport</option>
              </select>
            </Field>
            <Field id={`${id}-last4`} label="Its last 4 characters" help="Such as 567A">
              <input className="fy-input" id={`${id}-last4`} maxLength={4} value={form.id_last4 ?? ""} onChange={(e) => setForm({ ...form, id_last4: e.target.value })} aria-describedby={`${id}-last4-help`} />
            </Field>
          </>
        )}
        {day("appointed_on", "Appointed on")}
        {day("appointment_lodged_on", "Appointment lodged on (optional)", "The day BizFile+ acknowledged it: within 14 days")}
        {day("ceased_on", "Ceased on (optional)")}
        {day("cessation_lodged_on", "Cessation lodged on (optional)", "Within 14 days of the cessation")}
        {form.role === "director" && (
          <fieldset className="fy-field fy-field--full">
            <legend className="fy-label">Nominee director</legend>
            <label className="fy-row gap-2 fy-label">
              <input type="checkbox" checked={form.nominee} onChange={(e) => setForm({ ...form, nominee: e.target.checked })} />
              Acts on another person's directions
            </label>
            {form.nominee && (
              <input className="fy-input" aria-label="Whom they act for" maxLength={200} value={form.nominator ?? ""} onChange={(e) => setForm({ ...form, nominator: e.target.value })} />
            )}
          </fieldset>
        )}
        <Field id={`${id}-note`} label="Note (optional)">
          <textarea className="fy-textarea" id={`${id}-note`} maxLength={1000} value={form.note ?? ""} onChange={(e) => setForm({ ...form, note: e.target.value })} />
        </Field>
        <div className="fy-field--full">
          <FormButtons busy={busy} onClose={onClose} label={draft ? "Save the draft" : "Draft the officer"} />
        </div>
      </form>
    </Dialog>
  );
}

// NomineeForm drafts an entry of the registers of nominee directors or shareholders, or a later version (s386AL).
function NomineeForm({ follows, draft, start, directors, onClose, onDone }: {
  follows?: string;
  draft: Nominee | null;
  start: NomineeDraft;
  directors: Officer[];
  onClose: () => void;
  onDone: (text: string) => void;
}) {
  const id = useId();
  const [key] = useState(newKey);
  const [form, setForm] = useState<NomineeDraft>({ ...start });
  const holders = useQuery({ queryKey: ["holders"], queryFn: shareholders });
  const { busy, problem, save } = useSave(onClose, onDone);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const body: NomineeDraft = { kind: form.kind, nominator: form.nominator.trim(), since: form.since };
    if (form.kind === "director" && form.officer_id) body.officer_id = form.officer_id;
    if (form.kind === "shareholder" && form.holder_id) body.holder_id = form.holder_id;
    for (const k of ["informed_on", "register_on", "central_on", "ceased_on", "note"] as const) {
      const v = form[k]?.trim();
      if (v) body[k] = v;
    }
    void save(
      () =>
        draft
          ? api.PUT("/v1/corporate-profile/nominees/{nominee_id}", { params: { path: { nominee_id: draft.id }, header: { "If-Match": `"${draft.version}-draft"`, "Idempotency-Key": key } }, body })
          : api.POST("/v1/corporate-profile/nominees", { params: { header: { "Idempotency-Key": key } }, body: follows ? { entry_id: follows, ...body } : body }),
      draft ? "The draft is saved." : "The entry is drafted. Attach the nominee's notice or the register's extract, then ask for approval.",
    );
  };
  const day = (k: "since" | "informed_on" | "register_on" | "central_on" | "ceased_on", label: string, help?: string) => (
    <Field id={`${id}-${k}`} label={label} help={help}>
      <input className="fy-input" id={`${id}-${k}`} type="date" value={form[k] ?? ""} onChange={(e) => setForm({ ...form, [k]: e.target.value })}
        aria-describedby={help ? `${id}-${k}-help` : undefined} />
    </Field>
  );
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} wide eyebrow="Nominee registers" title={draft ? "Change the draft" : "Record a nominee"}
      description="As the company's register of nominee directors or nominee shareholders holds it. ACRA's central register is told within 2 business days after.">
      {problem && <Refusal problem={problem} title="The entry was not saved" />}
      <form className="fy-form fy-form--2" onSubmit={submit} noValidate>
        <Field id={`${id}-kind`} label="Register">
          <select className="fy-select" id={`${id}-kind`} value={form.kind} disabled={Boolean(follows || draft)} onChange={(e) => setForm({ ...form, kind: e.target.value as NomineeDraft["kind"] })}>
            <option value="shareholder">Nominee shareholders</option>
            <option value="director">Nominee directors</option>
          </select>
        </Field>
        {form.kind === "director" ? (
          <Field id={`${id}-who`} label="Director">
            <select className="fy-select" id={`${id}-who`} value={form.officer_id ?? ""} disabled={Boolean(follows || draft)} onChange={(e) => setForm({ ...form, officer_id: e.target.value })}>
              <option value="">Choose a director</option>
              {directors.map((o) => (
                <option key={o.officer_id} value={o.officer_id}>
                  {o.officer.name}
                </option>
              ))}
            </select>
          </Field>
        ) : (
          <Field id={`${id}-who`} label="Shareholder">
            <select className="fy-select" id={`${id}-who`} value={form.holder_id ?? ""} disabled={Boolean(follows || draft)} onChange={(e) => setForm({ ...form, holder_id: e.target.value })}>
              <option value="">Choose a shareholder</option>
              {(holders.data ?? []).map((h) => (
                <option key={h.contact_id} value={h.contact_id}>
                  {h.name}
                </option>
              ))}
            </select>
          </Field>
        )}
        <Field id={`${id}-for`} label="Acts or holds for">
          <input className="fy-input" id={`${id}-for`} maxLength={200} value={form.nominator} onChange={(e) => setForm({ ...form, nominator: e.target.value })} />
        </Field>
        {day("since", "A nominee since")}
        {day("informed_on", "Told the company on (optional)", "Within 30 days of becoming one")}
        {day("register_on", "Entered in the register on (optional)", "Within 7 days")}
        {day("central_on", "Filed with ACRA on (optional)", "Within 2 business days after the register")}
        {day("ceased_on", "Ceased on (optional)")}
        <Field id={`${id}-note`} label="Note (optional)">
          <textarea className="fy-textarea" id={`${id}-note`} maxLength={1000} value={form.note ?? ""} onChange={(e) => setForm({ ...form, note: e.target.value })} />
        </Field>
        <div className="fy-field--full">
          <FormButtons busy={busy} onClose={onClose} label={draft ? "Save the draft" : "Draft the entry"} />
        </div>
      </form>
    </Dialog>
  );
}

// ApprovalForm drafts the directors' approval of a year's financial statements (FR-CS-03, C.2 #33).
function ApprovalForm({ draft, start, onClose, onDone }: { draft: Approval | null; start: ApprovalDraft; onClose: () => void; onDone: (text: string) => void }) {
  const id = useId();
  const [key] = useState(newKey);
  const [form, setForm] = useState<ApprovalDraft>({ ...start });
  const { busy, problem, save } = useSave(onClose, onDone);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const body: ApprovalDraft = { fy_end: form.fy_end, approved_on: form.approved_on };
    if (form.sent_on) body.sent_on = form.sent_on;
    if (form.note?.trim()) body.note = form.note.trim();
    void save(
      () =>
        draft
          ? api.PUT("/v1/corporate-profile/statements-approvals/{statements_approval_id}", {
              params: { path: { statements_approval_id: draft.id }, header: { "If-Match": `"${draft.version}-draft"`, "Idempotency-Key": key } },
              body,
            })
          : api.POST("/v1/corporate-profile/statements-approvals", { params: { header: { "Idempotency-Key": key } }, body }),
      draft ? "The draft is saved." : "The approval is drafted. Attach the directors' resolution, then ask for approval.",
    );
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} eyebrow="Financial statements" title={draft ? "Change the draft" : "Record the directors' approval"}
      description="The day the directors approved a year's financial statements, before the AGM or before they are sent to the members.">
      {problem && <Refusal problem={problem} title="The approval was not saved" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <Field id={`${id}-end`} label="For the year ended">
          <input className="fy-input" id={`${id}-end`} type="date" value={form.fy_end} onChange={(e) => setForm({ ...form, fy_end: e.target.value })} />
        </Field>
        <Field id={`${id}-on`} label="Approved on">
          <input className="fy-input" id={`${id}-on`} type="date" value={form.approved_on} onChange={(e) => setForm({ ...form, approved_on: e.target.value })} />
        </Field>
        <Field id={`${id}-sent`} label="Sent to the members on (optional)" help="When the company sends them instead of holding an AGM">
          <input className="fy-input" id={`${id}-sent`} type="date" value={form.sent_on ?? ""} onChange={(e) => setForm({ ...form, sent_on: e.target.value })} aria-describedby={`${id}-sent-help`} />
        </Field>
        <FormButtons busy={busy} onClose={onClose} label={draft ? "Save the draft" : "Draft the approval"} />
      </form>
    </Dialog>
  );
}

function Card({ id, help, title, action, children }: { id: string; help: string; title: string; action?: ReactNode; children: ReactNode }) {
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id} title={helpText(help).tooltip}>
          {title}
        </h2>
        {action}
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">{children}</div>
    </section>
  );
}

function AddButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <Button size="sm" variant="secondary" onClick={onClick}>
      <Icon of={Plus} />
      {label}
    </Button>
  );
}

function recorded(r: { status: string }): boolean {
  return r.status === "recorded";
}

// CorporateProfile is Compliance › Corporate profile and registers (FR-CS-01, FR-CS-06, FR-CS-03; p1-company-secretary
// design D8, Expert): the profile's checks in both modes; the details in force and their history; the officers in office
// with the named secretary; the nominee registers; ACRA's business profile and its differences; each lodgement due; the
// directors' approvals of the statements. The Accountant and the Company secretary draft each record with its evidence,
// and another person approves it.
export function CorporateProfile() {
  const { session } = useSignedIn();
  const mayDraft = (session.roles ?? []).some((r) => drafters.includes(r));
  const h = helpText("compliance.corporate-profile");
  const p = useQuery({ queryKey: ["corporate-profile"], queryFn: profile });
  const rec = useQuery({ queryKey: ["corporate-profile", "reconciliation"], queryFn: reconciliation });
  const lodgements = useQuery({ queryKey: ["calendar"], queryFn: () => calendar() });
  const checks = useQuery({ queryKey: ["checks", "corporate"], queryFn: () => Promise.all(checkKeys.map((k) => checkOf(k))), retry: false });
  const [status, setStatus] = useState("");
  const [editing, setEditing] = useState<ReactNode>(null);
  if (p.isError) return <Refusal problem={problemOf(p.error)} title="The corporate profile could not be shown" />;
  if (!p.data) return <p className="fy-small">Loading the corporate profile…</p>;
  const pr = p.data;
  const close = () => setEditing(null);
  const draftOf = <T extends { status: string }>(xs: T[], pick: (x: T) => boolean = () => true) => xs.find((x) => x.status === "draft" && pick(x)) ?? null;
  const detailDraft = draftOf(pr.details);
  const officerDrafts = pr.officer_history.filter((o) => o.status === "draft");
  const nomineeDrafts = pr.nominee_history.filter((n) => n.status === "draft");
  const approvalDrafts = pr.statements_approvals.filter((a) => a.status === "draft");
  const directors = pr.officers.filter((o) => o.officer.role === "director");
  const due = (lodgements.data?.deadlines ?? []).filter((it) => it.key.startsWith("register_officer") || it.key.startsWith("register_detail") || it.key.startsWith("register_nominee") || it.key.startsWith("sg-acra"));
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/compliance">Compliance</Link>
          </nav>
          <h1 className="fy-h1" title={h.tooltip}>
            {h.label}
          </h1>
          <p className="fy-secondary">
            The company as ACRA's registers hold it, on {date(pr.on)}: each change recorded with its evidence, lodged within its deadline, and checked against the latest business profile.
          </p>
        </div>
      </div>
      <p role="status" className="fy-small">
        {status}
      </p>

      <Card id="corporate-checks" help="corporate-profile.checks" title="Checks">
        {checks.data ? (
          <ul className="fy-stack fy-stack--2" aria-label="The profile's checks">
            {checks.data.map((c) => {
              const s = statuses[c.status];
              return (
                <li key={c.key} className="fy-row fy-row--wrap gap-2">
                  <span className={`fy-chip ${s.tone}`}>
                    <Icon of={s.icon} />
                    {s.label}
                  </span>
                  <span className="fy-strong">{c.title}</span>
                  <span className="fy-small fy-secondary">{c.detail}</span>
                </li>
              );
            })}
          </ul>
        ) : (
          <p className="fy-small fy-muted">The checks run each night, and on a change; they show here once they have run.</p>
        )}
      </Card>

      <Card id="corporate-details" help="corporate-profile.details" title="The company"
        action={mayDraft && !detailDraft && <AddButton label={pr.detail ? "Record a change" : "Record the details"} onClick={() => setEditing(
          <DetailForm draft={null} start={pr.detail ? { ...pr.detail.detail, from: "", lodged_on: undefined, note: undefined } : { from: "", name: "", registered_office: "", ssic: "" }} onClose={close} onDone={setStatus} />,
        )} />}>
        {pr.detail ? (
          <dl className="fy-dl">
            <dt>Name</dt>
            <dd>{pr.detail.detail.name}</dd>
            <dt>Registered office</dt>
            <dd>{pr.detail.detail.registered_office}</dd>
            <dt>SSIC code</dt>
            <dd className="fy-mono">{pr.detail.detail.ssic}</dd>
            <dt>Since</dt>
            <dd>
              {date(pr.detail.detail.from)}
              {pr.detail.detail.lodged_on && <span className="fy-small fy-muted"> · lodged {date(pr.detail.detail.lodged_on)}</span>}
            </dd>
          </dl>
        ) : (
          <p className="fy-small fy-muted">No details are recorded. Record them from incorporation, as the business profile shows them: the checks start from that day.</p>
        )}
        {detailDraft && (
          <DraftBlock kind="corporate-detail" r={detailDraft} mayDraft={mayDraft} onStatus={setStatus}
            onChange={() => setEditing(<DetailForm draft={detailDraft} start={detailDraft.detail} onClose={close} onDone={setStatus} />)}>
            <p className="fy-small">
              {detailDraft.detail.name}, {detailDraft.detail.registered_office}, SSIC {detailDraft.detail.ssic}, from {date(detailDraft.detail.from)}
            </p>
          </DraftBlock>
        )}
        {pr.details.filter((d) => d.status === "superseded").length > 0 && (
          <details>
            <summary className="fy-small">History</summary>
            <ul className="fy-stack fy-stack--1">
              {pr.details
                .filter((d) => d.status === "superseded")
                .map((d) => (
                  <li key={d.id} className="fy-small">
                    From {date(d.detail.from)}: {d.detail.name}, {d.detail.registered_office}, SSIC {d.detail.ssic}
                  </li>
                ))}
            </ul>
          </details>
        )}
      </Card>

      <Card id="corporate-officers" help="corporate-profile.officers" title="Officers"
        action={mayDraft && <AddButton label="Record an officer" onClick={() => setEditing(
          <OfficerForm draft={null} start={{ role: "director", name: "", kind: "individual", appointed_on: "", nominee: false }} onClose={close} onDone={setStatus} />,
        )} />}>
        {pr.officers.length === 0 ? (
          <p className="fy-small fy-muted">No officer is in office on this day.</p>
        ) : (
          <table className="fy-table fy-table--cards" aria-label="Officers in office">
            <thead>
              <tr>
                <th scope="col">Name</th>
                <th scope="col">Office</th>
                <th scope="col">Appointed</th>
                <th scope="col">Lodged</th>
                <th scope="col">
                  <span className="sr-only">Change</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {pr.officers.map((o) => (
                <tr key={o.id}>
                  <td data-card="title">
                    {o.officer.name}
                    {o.officer.role === "secretary" && (
                      <span className="fy-chip fy-chip--good" style={{ marginInlineStart: "0.5rem" }}>
                        <Icon of={BadgeCheck} />
                        Named secretary
                      </span>
                    )}
                    {o.officer.nominee && <div className="fy-small fy-muted">Nominee for {o.officer.nominator}</div>}
                    {!o.masked && o.officer.id_last4 && <div className="fy-small fy-muted fy-mono">{(o.officer.id_type ?? "").toUpperCase()} ending {o.officer.id_last4}</div>}
                  </td>
                  <td data-card="meta">{roleWords[o.officer.role]}</td>
                  <td data-card="meta">{date(o.officer.appointed_on)}</td>
                  <td data-card="status">{o.officer.appointment_lodged_on ? date(o.officer.appointment_lodged_on) : <span className="fy-muted">Not recorded</span>}</td>
                  <td>
                    {mayDraft && !officerDrafts.some((d) => d.officer_id === o.officer_id) && (
                      <Button size="sm" variant="ghost" aria-label={`Record a change of ${o.officer.name}`} onClick={() => setEditing(
                        <OfficerForm follows={o.officer_id} draft={null} start={{ ...o.officer, note: undefined }} onClose={close} onDone={setStatus} />,
                      )}>
                        <Icon of={PencilLine} />
                        Change
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {officerDrafts.map((d) => (
          <DraftBlock key={d.id} kind="corporate-officer" r={d} mayDraft={mayDraft} onStatus={setStatus}
            onChange={() => setEditing(<OfficerForm draft={d} start={d.officer} onClose={close} onDone={setStatus} />)}>
            <p className="fy-small">
              {d.officer.name}, {roleWords[d.officer.role].toLowerCase()} from {date(d.officer.appointed_on)}
              {d.officer.ceased_on && `, ceased ${date(d.officer.ceased_on)}`}
            </p>
          </DraftBlock>
        ))}
      </Card>

      <Card id="corporate-nominees" help="corporate-profile.nominees" title="Nominee directors and shareholders"
        action={mayDraft && <AddButton label="Record a nominee" onClick={() => setEditing(
          <NomineeForm draft={null} directors={directors} start={{ kind: "shareholder", nominator: "", since: "" }} onClose={close} onDone={setStatus} />,
        )} />}>
        <p className="fy-small fy-secondary">
          Registrable controllers are kept in the equity register: <Link to="/equity/reconciliation">Equity › Reconciliation</Link>.
        </p>
        {pr.nominees.length === 0 ? (
          <p className="fy-small fy-muted">No nominee is recorded.</p>
        ) : (
          <ul className="fy-stack fy-stack--2" aria-label="Nominees">
            {pr.nominees.map((n) => (
              <li key={n.id} className="fy-row fy-row--between fy-row--wrap gap-2">
                <span>
                  <span className="fy-strong">{n.name}</span>, nominee {n.entry.kind} for {n.entry.nominator} since {date(n.entry.since)}
                  <span className="fy-small fy-muted">
                    {" "}
                    · register {n.entry.register_on ? date(n.entry.register_on) : "not recorded"} · ACRA {n.entry.central_on ? date(n.entry.central_on) : "not recorded"}
                  </span>
                </span>
                {mayDraft && !nomineeDrafts.some((d) => d.entry_id === n.entry_id) && (
                  <Button size="sm" variant="ghost" aria-label={`Record a change of ${n.name}'s entry`} onClick={() => setEditing(
                    <NomineeForm follows={n.entry_id} draft={null} directors={directors} start={{ ...n.entry, note: undefined }} onClose={close} onDone={setStatus} />,
                  )}>
                    <Icon of={PencilLine} />
                    Change
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
        {nomineeDrafts.map((d) => (
          <DraftBlock key={d.id} kind="nominee-entry" r={d} mayDraft={mayDraft} onStatus={setStatus}
            onChange={() => setEditing(<NomineeForm draft={d} directors={directors} start={d.entry} onClose={close} onDone={setStatus} />)}>
            <p className="fy-small">
              {d.name}, nominee {d.entry.kind} for {d.entry.nominator} since {date(d.entry.since)}
            </p>
          </DraftBlock>
        ))}
      </Card>

      <Card id="corporate-acra" help="corporate-profile.acra" title="Against ACRA's business profile">
        {rec.data && rec.data.status === "not_assessed" && (
          <p className="fy-small">
            <Icon of={TriangleAlert} /> Not assessed: {rec.data.reason}
          </p>
        )}
        {rec.data && rec.data.status === "reconciled" && (
          <p className="fy-small">
            <Icon of={CircleCheck} /> The officers and the registered office agree with the business profile of {date(rec.data.profile_on)}.
          </p>
        )}
        {rec.data && rec.data.status === "differs" && (
          <ul className="fy-stack fy-stack--2" aria-label="Differences from ACRA">
            {rec.data.differences.map((d, i) => (
              <li key={i} className="fy-small">
                <Icon of={TriangleAlert} />{" "}
                {d.kind === "office_differs"
                  ? `The registered office: ${d.profile} here, ${d.acra} at ACRA`
                  : d.kind === "missing_in_fyniq"
                    ? `${d.name}: ACRA lists them as ${d.role} from ${date(d.acra)}, not in office here`
                    : d.kind === "missing_at_acra"
                      ? `${d.name}: in office here as ${d.role}, not listed by ACRA`
                      : `${d.name}: appointed ${date(d.profile)} here and ${date(d.acra)} at ACRA`}
              </li>
            ))}
          </ul>
        )}
        <p className="fy-small fy-secondary">
          Type in a business profile with its officers under <Link to="/equity/reconciliation">Equity › Reconciliation</Link>.
        </p>
      </Card>

      <Card id="corporate-lodgements" help="corporate-profile.lodgements" title="Lodgements">
        {due.length === 0 ? (
          <p className="fy-small fy-muted">No change waits to be lodged with ACRA.</p>
        ) : (
          <ul className="fy-stack fy-stack--2" aria-label="Lodgements due">
            {due.map((it) => (
              <li key={it.key} className="fy-row fy-row--between fy-row--wrap gap-2">
                <span className="fy-small">
                  <Icon of={CalendarClock} /> {it.title}
                  {it.due_on && <span className="fy-muted"> · due {date(it.due_on)}</span>}
                </span>
                <ItemChip it={it} />
              </li>
            ))}
          </ul>
        )}
      </Card>

      <Card id="corporate-approvals" help="corporate-profile.approvals" title="Directors' approval of the financial statements"
        action={mayDraft && <AddButton label="Record an approval" onClick={() => setEditing(<ApprovalForm draft={null} start={{ fy_end: "", approved_on: "" }} onClose={close} onDone={setStatus} />)} />}>
        {pr.statements_approvals.filter(recorded).length === 0 ? (
          <p className="fy-small fy-muted">No approval is recorded.</p>
        ) : (
          <ul className="fy-stack fy-stack--2" aria-label="Approvals">
            {pr.statements_approvals.filter(recorded).map((a) => (
              <li key={a.id} className="fy-small">
                The year ended {date(a.approval.fy_end)}: approved {date(a.approval.approved_on)}
                {a.approval.sent_on && `, sent to the members ${date(a.approval.sent_on)}`}
              </li>
            ))}
          </ul>
        )}
        {approvalDrafts.map((d) => (
          <DraftBlock key={d.id} kind="statements-approval" r={d} mayDraft={mayDraft} onStatus={setStatus}
            onChange={() => setEditing(<ApprovalForm draft={d} start={d.approval} onClose={close} onDone={setStatus} />)}>
            <p className="fy-small">
              The year ended {date(d.approval.fy_end)}: approved {date(d.approval.approved_on)}
            </p>
          </DraftBlock>
        ))}
      </Card>
      {editing}
    </div>
  );
}
