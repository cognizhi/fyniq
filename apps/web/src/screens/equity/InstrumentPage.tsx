import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { Hourglass, PencilLine, Send } from "lucide-react";
import { useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listAccounts } from "../accounting/data";
import { EvidencePanel } from "../accounting/JournalEvidence";
import { EquityTabs } from "./Holders";
import { InstrumentFields, instrumentBody } from "./InstrumentForm";
import {
  classificationText,
  denominatorWords,
  drafters,
  formWords,
  instrumentOf,
  InstrumentStatus,
  termsText,
  triggerWords,
  type Instrument,
  type InstrumentDraft,
} from "./instrumentWords";

// draftOf is an instrument's terms as a draft takes them: its own fields only.
function draftOf(i: Instrument): InstrumentDraft {
  const {
    id: _id, status: _s, version: _v, holder_name: _h, currency: _c, class_code: _cc, template_enabled: _t, held: _held, evidence: _e,
    approval_id: _a, recorded_by: _rb, recorded_at: _ra, checks: _ch, conversion: _cv, ...draft
  } = i;
  return draft;
}

// InstrumentPage is Equity › SAFEs and notes › an instrument (docs/design/screens Instruments; FR-EQ-18, FR-DOC-06): its
// terms, the signed instrument as evidence, its approval, what 2700 holds for it from the ledger, and its conversion.
export function InstrumentPage() {
  const { instrumentId } = useParams({ strict: false }) as { instrumentId: string };
  const { session } = useSignedIn();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const q = useQuery({ queryKey: ["instrument", instrumentId], queryFn: () => instrumentOf(instrumentId) });
  const [editing, setEditing] = useState(false);
  const [status, setStatus] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const mayDraft = (session.roles ?? []).some((r) => drafters.includes(r));
  const askH = helpText("instrument.ask");
  const refresh = async (text: string) => {
    setStatus(text);
    await queries.invalidateQueries({ queryKey: ["instrument", instrumentId] });
    await queries.invalidateQueries({ queryKey: ["instruments"] });
  };
  if (q.isError) return <Refusal problem={problemOf(q.error)} title="The instrument could not be shown" />;
  if (!q.data) return null;
  const { i, etag } = q.data;
  const ask = async () => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/instruments/{instrument_id}/approval", { params: { path: { instrument_id: i.id }, header: { "If-Match": etag, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh("Asked for approval. Another Owner or the Controller approves it.");
  };
  const rows: [string, string][] = [
    ["Holder", i.holder_name],
    ["Form", formWords[i.form]],
    ["Amount paid", `${formatAmount(i.amount)} ${i.currency}`],
    ["Signed on", date(i.signed_on)],
    ["Terms", termsText(i)],
    ["Converts at", triggerWords[i.trigger]],
    ["Converts into", i.class_code],
    ["Its price divides by", i.denominator === "as_terms" && i.denominator_note ? i.denominator_note : denominatorWords[i.denominator]],
    ["Price decimals", String(i.price_decimals)],
    ["Classification", classificationText(i)],
    ["Template", i.template ? `${i.template}${i.template_enabled ? ", enabled" : ", not enabled"}` : "None"],
  ];
  if (i.held) rows.push(["Held on 2700 today", `${formatAmount(i.held.amount)} ${i.held.currency}`]);
  if (i.recorded_at) rows.push(["Recorded", `${date(i.recorded_at)}${i.recorded_by ? ` by ${i.recorded_by.name}` : ""}`]);
  if (i.note) rows.push(["Note", i.note]);
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/equity/instruments">SAFEs and notes</Link>
            <span aria-hidden="true">›</span>
            <span>{i.holder_name}</span>
          </nav>
          <h1 className="fy-h1">
            {formWords[i.form]}: {i.holder_name}
          </h1>
          <InstrumentStatus i={i} />
        </div>
        <div className="fy-pagehead__actions">
          {i.status === "draft" && mayDraft && !i.approval_id && (
            <>
              <Button variant="secondary" onClick={() => setEditing(true)}>
                <Icon of={PencilLine} />
                Change
              </Button>
              <Button onClick={() => void ask()} disabled={busy || i.evidence === 0} title={askH.tooltip}>
                <Icon of={Send} />
                {askH.label}
              </Button>
            </>
          )}
          {i.approval_id && (
            <Link className="fy-btn fy-btn--secondary" to="/approvals/$approvalId" params={{ approvalId: i.approval_id }}>
              <Icon of={Hourglass} />
              Waiting for approval
            </Link>
          )}
          {i.conversion && (
            <Link className="fy-btn fy-btn--secondary" to="/equity/transactions/$transactionId" params={{ transactionId: i.conversion.transaction_id }}>
              {i.conversion.number ? `Its conversion, ${i.conversion.number}` : "Its conversion"}
            </Link>
          )}
        </div>
      </div>
      <EquityTabs current="instruments" />
      <p className="fy-small" role="status">
        {status}
      </p>
      {problem && <Refusal problem={problem} title="That was not done" />}
      <div className="fy-split">
        <section className="fy-card" aria-labelledby="instrument-terms">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="instrument-terms">
              Its terms
            </h2>
          </div>
          <dl className="fy-card__body fy-dl">
            {rows.map(([k, v]) => (
              <div key={k}>
                <dt>{k}</dt>
                <dd>{v}</dd>
              </div>
            ))}
          </dl>
        </section>
        <div className="fy-stack fy-stack--4">
          {i.checks.length > 0 && (
            <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-label="What stops it">
              {i.checks.map((c) => (
                <p key={`${c.code}-${c.field}`} className="fy-small fy-secondary">
                  {c.message}. {c.fix}.
                </p>
              ))}
            </section>
          )}
          <EvidencePanel record={{ kind: "instrument", id: i.id, etag }} draft={i.status === "draft"} mayAttach={mayDraft && i.status === "draft" && !i.approval_id}
            onChanged={(m) => void refresh(m)} />
          {i.status === "recorded" && !i.conversion && (
            <p className="fy-small fy-secondary">
              Its money comes in on a bank line categorised to 2700 that names it. At a priced round, the round's page drafts its conversion.
            </p>
          )}
        </div>
      </div>
      {editing && <ChangeDraft i={i} etag={etag} onClose={() => setEditing(false)} onDone={(m) => void refresh(m)} />}
    </div>
  );
}

// ChangeDraft saves a new version of a drafted instrument; its holder stays.
function ChangeDraft({ i, etag, onClose, onDone }: { i: Instrument; etag: string; onClose: () => void; onDone: (text: string) => void }) {
  const confirm = useConfirm();
  const chart = useQuery({ queryKey: ["accounts", "all"], queryFn: listAccounts });
  const capital = (chart.data ?? []).filter((a) => a.role === "share-capital" && a.active && !a.group);
  const [form, setForm] = useState<InstrumentDraft>(draftOf(i));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.PUT("/v1/instruments/{instrument_id}", {
        params: { path: { instrument_id: i.id }, header: { "If-Match": etag, "Idempotency-Key": key } },
        body: instrumentBody(form, capital[0]?.id ?? ""),
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone("The draft is saved.");
    onClose();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} wide eyebrow="SAFE or note" title="Change the draft"
      description="The holder stays. Recorded once another person approves it, with the signed instrument attached.">
      {problem && <Refusal problem={problem} title="The draft was not saved" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <InstrumentFields form={form} set={setForm} holderFixed />
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy}>
            Save the draft
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
