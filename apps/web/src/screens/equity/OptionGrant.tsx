import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { CalendarX2, Hourglass, Send } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { EvidencePanel } from "../accounting/JournalEvidence";
import { EquityTabs } from "./Holders";
import { blankGrant, GrantFields, grantBody, useCapital } from "./OptionForms";
import { count, drafters, eventWords, grantOf, RecordStatus, vestingText, type OptionEvent, type OptionEventDraft, type OptionGrantDraft } from "./optionWords";

// OptionGrantNew is Equity › Option pool › New grant (docs/design/screens OptionPool "New grant"; FR-EQ-19): the holder,
// the options and their terms. Saved as a draft, it takes the grant letter and asks for approval on its own page.
export function OptionGrantNew() {
  const navigate = useNavigate();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const capital = useCapital();
  const [form, setForm] = useState<OptionGrantDraft>(blankGrant);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const draftH = helpText("option-grant.draft");
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/option-grants", { params: { header: { "Idempotency-Key": key } }, body: grantBody(form, capital[0]?.id ?? "") }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["option-grants"] });
    await navigate({ to: "/equity/options/grants/$grantId", params: { grantId: out.data.id } });
  };
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/equity/options">Option pool</Link>
            <span aria-hidden="true">›</span>
            <span>New grant</span>
          </nav>
          <h1 className="fy-h1" title={draftH.tooltip}>
            New grant of options
          </h1>
          <p className="fy-secondary">Draft it, attach the grant letter, then ask another person to approve it. It takes at most what the pool leaves.</p>
        </div>
      </div>
      <EquityTabs current="options" />
      <form className="fy-card fy-card--pad fy-stack fy-stack--4" onSubmit={submit} aria-label="New grant of options">
        {problem && <Refusal problem={problem} title="The grant was not drafted" />}
        <GrantFields form={form} set={setForm} holderFixed={false} />
        <div className="fy-row justify-end gap-2">
          <Link className="fy-btn fy-btn--ghost" to="/equity/options">
            Cancel
          </Link>
          <Button variant="primary" type="submit" disabled={busy} title={draftH.tooltip}>
            {draftH.label}
          </Button>
        </div>
      </form>
    </div>
  );
}

// OptionGrantPage is Equity › Option pool › a grant (FR-EQ-19, FR-DOC-06): its terms, its standing today, the grant
// letter as evidence, its approval, and its forfeitures and expiries, each with its notice and approval.
export function OptionGrantPage() {
  const { grantId } = useParams({ strict: false }) as { grantId: string };
  const { session } = useSignedIn();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const q = useQuery({ queryKey: ["option-grant", grantId], queryFn: () => grantOf(grantId) });
  const [status, setStatus] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [taking, setTaking] = useState(false);
  const mayDraft = (session.roles ?? []).some((r) => drafters.includes(r));
  const askH = helpText("option-grant.ask");
  const refresh = async (text: string) => {
    setStatus(text);
    await queries.invalidateQueries({ queryKey: ["option-grant", grantId] });
    await queries.invalidateQueries({ queryKey: ["option-grants"] });
    await queries.invalidateQueries({ queryKey: ["option-pool"] });
  };
  if (q.isError) return <Refusal problem={problemOf(q.error)} title="The grant could not be shown" />;
  if (!q.data) return null;
  const { g, etag } = q.data;
  const ask = async () => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/option-grants/{grant_id}/approval", { params: { path: { grant_id: g.id }, header: { "If-Match": etag, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh("Asked for approval. Another Owner or the Controller approves it, never the holder.");
  };
  const terms: [string, string][] = [
    ["Holder", g.holder_name],
    ["Options", count(g.number)],
    ["Exercise price", `${g.exercise_price} ${g.currency} a share`],
    ["Granted on", date(g.granted_on)],
    ["Vesting", vestingText(g)],
    ["Expires on", date(g.expires_on)],
    ["Over the class of", g.class_code],
  ];
  if (g.recorded_at) terms.push(["Recorded", `${date(g.recorded_at)}${g.recorded_by ? ` by ${g.recorded_by.name}` : ""}`]);
  if (g.note) terms.push(["Note", g.note]);
  const standing: [string, string][] = [
    ["Vested", count(g.vested)],
    ["Exercisable", count(g.exercisable)],
    ["Exercised, now shares", count(g.exercised)],
    ["Forfeited", count(g.forfeited)],
    ["Expired", count(g.expired)],
    ["Outstanding", count(g.outstanding)],
    ["Next vesting", g.next_vests_on ? `${count(g.next_vests)} on ${date(g.next_vests_on)}` : "Nothing more vests"],
  ];
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/equity/options">Option pool</Link>
            <span aria-hidden="true">›</span>
            <span>{g.holder_name}</span>
          </nav>
          <h1 className="fy-h1">Grant of options: {g.holder_name}</h1>
          <RecordStatus status={g.status} asked={g.approval_id} />
        </div>
        <div className="fy-pagehead__actions">
          {g.status === "draft" && mayDraft && !g.approval_id && (
            <Button onClick={() => void ask()} disabled={busy || g.evidence === 0} title={askH.tooltip}>
              <Icon of={Send} />
              {askH.label}
            </Button>
          )}
          {g.approval_id && (
            <Link className="fy-btn fy-btn--secondary" to="/approvals/$approvalId" params={{ approvalId: g.approval_id }}>
              <Icon of={Hourglass} />
              Waiting for approval
            </Link>
          )}
          {g.status === "recorded" && mayDraft && g.outstanding !== "0" && (
            <Button variant="secondary" onClick={() => setTaking(true)} title={helpText("option-event.draft").tooltip}>
              <Icon of={CalendarX2} />
              Forfeit or let lapse
            </Button>
          )}
        </div>
      </div>
      <EquityTabs current="options" />
      <p className="fy-small" role="status">
        {status}
      </p>
      {problem && <Refusal problem={problem} title="That was not done" />}
      <div className="fy-split">
        <section className="fy-card" aria-labelledby="grant-terms">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="grant-terms">
              Its terms
            </h2>
          </div>
          <dl className="fy-card__body fy-dl">
            {terms.map(([k, v]) => (
              <div key={k}>
                <dt>{k}</dt>
                <dd>{v}</dd>
              </div>
            ))}
          </dl>
        </section>
        <div className="fy-stack fy-stack--4">
          <section className="fy-card" aria-labelledby="grant-standing">
            <div className="fy-card__head">
              <h2 className="fy-h3" id="grant-standing">
                Today
              </h2>
            </div>
            <dl className="fy-card__body fy-dl">
              {standing.map(([k, v]) => (
                <div key={k}>
                  <dt>{k}</dt>
                  <dd className="fy-num">{v}</dd>
                </div>
              ))}
            </dl>
          </section>
          {g.checks.length > 0 && (
            <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-label="What stops it">
              {g.checks.map((c) => (
                <p key={`${c.code}-${c.field}`} className="fy-small fy-secondary">
                  {c.message}. {c.fix}.
                </p>
              ))}
            </section>
          )}
          <EvidencePanel record={{ kind: "option-grant", id: g.id, etag }} draft={g.status === "draft"} mayAttach={mayDraft && g.status === "draft" && !g.approval_id}
            onChanged={(m) => void refresh(m)} />
          {g.status === "recorded" && g.exercisable !== "0" && (
            <p className="fy-small fy-secondary">
              To exercise vested options, an Accountant drafts an <Link to="/equity/transactions/new">Exercise of options</Link> under Transactions.
            </p>
          )}
        </div>
      </div>
      {g.events.length > 0 && (
        <section className="fy-card" aria-labelledby="grant-events">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="grant-events">
              Forfeitures and expiries
            </h2>
          </div>
          <div className="fy-card__body fy-stack fy-stack--4">
            {g.events.map((e) => (
              <EventCard key={e.id} e={e} mayDraft={mayDraft} onChanged={(m) => void refresh(m)} />
            ))}
          </div>
        </section>
      )}
      {taking && <TakeOptions grantId={g.id} etag={etag} onClose={() => setTaking(false)} onDone={(m) => void refresh(m)} />}
    </div>
  );
}

// EventCard is one forfeiture or expiry: its options and day, and while a draft its notice and approval.
function EventCard({ e, mayDraft, onChanged }: { e: OptionEvent; mayDraft: boolean; onChanged: (text: string) => void }) {
  const confirm = useConfirm();
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const etag = `"${e.version}-${e.status === "draft" ? "draft" : "posted"}"`;
  const askH = helpText("option-event.ask");
  const ask = async () => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/option-events/{event_id}/approval", { params: { path: { event_id: e.id }, header: { "If-Match": etag, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onChanged("Asked for approval. Another Owner or the Controller approves it, never the holder.");
  };
  return (
    <article className="fy-stack fy-stack--2" aria-label={`${eventWords[e.kind]} of ${count(e.number)} on ${date(e.on)}`}>
      <div className="fy-row justify-between gap-2">
        <span>
          {eventWords[e.kind]} of <span className="fy-num">{count(e.number)}</span> options on {date(e.on)}
          {e.note ? `: ${e.note}` : ""}
        </span>
        <span className="fy-row gap-2">
          <RecordStatus status={e.status} asked={e.approval_id} />
          {e.status === "draft" && mayDraft && !e.approval_id && (
            <Button size="sm" onClick={() => void ask()} disabled={busy || e.evidence === 0} title={askH.tooltip}>
              <Icon of={Send} />
              {askH.label}
            </Button>
          )}
        </span>
      </div>
      {problem && <Refusal problem={problem} title="That was not done" />}
      {e.status === "draft" && (
        <EvidencePanel record={{ kind: "option-event", id: e.id, etag }} draft mayAttach={mayDraft && !e.approval_id} onChanged={onChanged} />
      )}
    </article>
  );
}

// TakeOptions drafts a forfeiture or expiry of the grant's options: how many and when.
function TakeOptions({ grantId, etag, onClose, onDone }: { grantId: string; etag: string; onClose: () => void; onDone: (text: string) => void }) {
  const id = useId();
  const confirm = useConfirm();
  const [form, setForm] = useState<OptionEventDraft>({ kind: "forfeiture", number: "", on: "" });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const draftH = helpText("option-event.draft");
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const body = { ...form, number: form.number.trim().replace(/,/g, "") };
    if (!body.note?.trim()) delete body.note;
    const out = await guarded(confirm, () =>
      api.POST("/v1/option-grants/{grant_id}/events", { params: { path: { grant_id: grantId }, header: { "If-Match": etag, "Idempotency-Key": key } }, body }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone("Drafted. Attach the notice, then ask for approval.");
    onClose();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} eyebrow="Grant of options" title="Forfeit or let lapse"
      description="A forfeiture gives up options not vested when the holder leaves; an expiry lets options lapse. Either way they are back in the pool from that day.">
      {problem && <Refusal problem={problem} title="It was not drafted" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-kind`}>
            What happens
          </label>
          <select className="fy-select" id={`${id}-kind`} value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value as OptionEventDraft["kind"] })}>
            <option value="forfeiture">Forfeiture: options not vested</option>
            <option value="expiry">Expiry: options lapse</option>
          </select>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-number`}>
            Options
          </label>
          <input className="fy-input fy-num" id={`${id}-number`} inputMode="numeric" value={form.number} onChange={(e) => setForm({ ...form, number: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-on`}>
            On
          </label>
          <input className="fy-input" id={`${id}-on`} type="date" value={form.on} onChange={(e) => setForm({ ...form, on: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-note`}>
            Note (optional)
          </label>
          <input className="fy-input" id={`${id}-note`} maxLength={1000} value={form.note ?? ""} onChange={(e) => setForm({ ...form, note: e.target.value })} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy} title={draftH.tooltip}>
            {draftH.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
