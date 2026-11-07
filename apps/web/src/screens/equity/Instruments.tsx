import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Calculator, CircleCheck, FileSymlink, Hourglass, Plus, Send, ShieldCheck, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { EvidencePanel } from "../accounting/JournalEvidence";
import { EquityTabs } from "./Holders";
import {
  classificationText,
  drafters,
  formWords,
  InstrumentStatus,
  listInstruments,
  listTemplates,
  termsText,
  type Preview,
  type Template,
  type Working,
} from "./instrumentWords";
import { listTransactions, reviewText, sharesText, transactionOf } from "./transactionWords";

// Instruments is Equity › SAFEs and notes (docs/design/screens Instruments; FR-EQ-18, FR-EQ-20 Expert): every SAFE and
// convertible note with its terms, its status and what 2700 holds for it; the forms Fyniq converts by itself; and a
// preview of the conversions at a round, with the working of each price, before any is drafted.
export function Instruments() {
  const { session } = useSignedIn();
  const q = useQuery({ queryKey: ["instruments"], queryFn: listInstruments });
  const mayDraft = (session.roles ?? []).some((r) => drafters.includes(r));
  const addH = helpText("instrument.draft");
  const all = q.data ?? [];
  const recorded = all.filter((i) => i.status === "recorded" && !i.conversion).length;
  const converted = all.filter((i) => i.conversion).length;
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Equity</span>
          <h1 className="fy-h1" title={helpText("equity.instruments").tooltip}>
            SAFEs and convertible notes
          </h1>
          <p className="fy-secondary">Money in today that becomes shares at a priced round. Fyniq converts it with the instrument's own terms and whole shares only.</p>
        </div>
        {mayDraft && (
          <div className="fy-pagehead__actions">
            <Link className="fy-btn fy-btn--secondary" to="/equity/instruments/new" title={addH.tooltip}>
              <Icon of={Plus} />
              Add instrument
            </Link>
          </div>
        )}
      </div>
      <EquityTabs current="instruments" />
      {q.isError && <Refusal problem={problemOf(q.error)} title="The instruments could not be shown" />}
      <div className="fy-grid fy-grid--tiles">
        <div className="fy-tile">
          <span className="fy-tile__label">Outstanding</span>
          <span className="fy-tile__value fy-num">{recorded}</span>
          <span className="fy-tile__meta">recorded, not converted</span>
        </div>
        <div className="fy-tile">
          <span className="fy-tile__label">Drafts</span>
          <span className="fy-tile__value fy-num">{all.filter((i) => i.status === "draft").length}</span>
          <span className="fy-tile__meta">waiting for the signed copy or approval</span>
        </div>
        <div className="fy-tile">
          <span className="fy-tile__label">Converting or converted</span>
          <span className="fy-tile__value fy-num">{converted}</span>
          <span className="fy-tile__meta">into shares at a round</span>
        </div>
      </div>
      {q.data && all.length === 0 && (
        <p className="fy-small fy-muted">No SAFE or note is recorded yet. Add one when it is signed; its money comes in on a bank line to 2700 that names it.</p>
      )}
      {all.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table" aria-label="SAFEs and convertible notes">
            <thead>
              <tr>
                <th scope="col">Holder</th>
                <th scope="col">Form</th>
                <th scope="col" className="fy-num">
                  Amount
                </th>
                <th scope="col">Signed</th>
                <th scope="col">Terms</th>
                <th scope="col">Classification</th>
                <th scope="col" className="fy-num">
                  On 2700 today
                </th>
                <th scope="col">Status</th>
              </tr>
            </thead>
            <tbody>
              {all.map((i) => (
                <tr key={i.id}>
                  <td>
                    <Link to="/equity/instruments/$instrumentId" params={{ instrumentId: i.id }}>
                      {i.holder_name}
                    </Link>
                  </td>
                  <td>{formWords[i.form]}</td>
                  <td className="fy-num">
                    {formatAmount(i.amount)} {i.currency}
                  </td>
                  <td>{date(i.signed_on)}</td>
                  <td>{termsText(i)}</td>
                  <td>{classificationText(i)}</td>
                  <td className="fy-num">{i.held ? `${formatAmount(i.held.amount)} ${i.held.currency}` : "–"}</td>
                  <td>
                    <InstrumentStatus i={i} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <div className="fy-split">
        <ConversionPreview mayDraft={mayDraft} />
        <Templates mayDraft={mayDraft} />
      </div>
    </div>
  );
}

// ConversionPreview works out, on the server, what every recorded SAFE and note not converted yet would convert into at
// a round: an approved issue for cash, or a price typed in (a sandbox: nothing posts). At an approved round the
// Accountant drafts the conversions from it.
function ConversionPreview({ mayDraft }: { mayDraft: boolean }) {
  const id = useId();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const txs = useQuery({ queryKey: ["equity-transactions"], queryFn: listTransactions });
  const rounds = (txs.data ?? []).filter((t) => t.details.kind === "issue_cash" && (t.status === "approved" || t.status === "effective"));
  const [round, setRound] = useState("");
  const [price, setPrice] = useState("");
  const [preview, setPreview] = useState<Preview | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [status, setStatus] = useState("");
  const [busy, setBusy] = useState(false);
  const h = helpText("instrument.preview");
  const convertH = helpText("equity-transaction.convert-safes");
  const run = async (e: FormEvent) => {
    e.preventDefault();
    setProblem(null);
    setStatus("");
    const query = round ? { round_id: round } : { price: price.trim() };
    const out = await api.GET("/v1/instruments/conversion-preview", { params: { query } });
    if (!out.data) {
      setPreview(null);
      return setProblem(problemOf(out.error));
    }
    setPreview(out.data);
  };
  const draft = async () => {
    if (!preview?.round_id) return;
    setBusy(true);
    setProblem(null);
    const { etag } = await transactionOf(preview.round_id);
    const out = await guarded(confirm, () =>
      api.POST("/v1/equity-transactions/{transaction_id}/conversions", {
        params: { path: { transaction_id: preview.round_id as string }, header: { "If-Match": etag, "Idempotency-Key": newKey() } },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    const n = out.data.items.length;
    setStatus(n === 0 ? "No SAFE or note waits to convert at this round." : `Drafted ${n} conversion${n === 1 ? "" : "s"}: open each in Transactions, attach the resolutions and ask for approval.`);
    setPreview(null);
    await queries.invalidateQueries({ queryKey: ["instruments"] });
    await queries.invalidateQueries({ queryKey: ["equity-transactions"] });
  };
  return (
    <section className="fy-card" aria-labelledby={`${id}-title`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-title`} title={h.tooltip}>
          <Icon of={Calculator} /> Conversion preview
        </h2>
        <span className="fy-small fy-muted">Sandbox, nothing posts</span>
      </div>
      <form className="fy-card__body fy-stack fy-stack--3" onSubmit={run} aria-label="Preview the conversions">
        <div className="fy-form fy-form--2">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-round`}>
              At the round
            </label>
            <select className="fy-select" id={`${id}-round`} value={round} onChange={(e) => setRound(e.target.value)}>
              <option value="">A price I type in</option>
              {rounds.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.number} · {t.parties.map((p) => p.name).join(", ")} · {t.details.price}
                </option>
              ))}
            </select>
          </div>
          {!round && (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-price`}>
                Round price per share
              </label>
              <input className="fy-input fy-num" id={`${id}-price`} inputMode="decimal" value={price} onChange={(e) => setPrice(e.target.value)} placeholder="Such as 3.00" />
            </div>
          )}
        </div>
        <div className="fy-row justify-end">
          <Button type="submit" variant="secondary" title={h.tooltip}>
            {h.label}
          </Button>
        </div>
        <p className="fy-small" role="status">
          {status}
        </p>
        {problem && <Refusal problem={problem} title="The preview could not be worked out" />}
        {preview && <PreviewResult preview={preview} />}
        {preview?.round_id && mayDraft && preview.conversions.length > 0 && (
          <div className="fy-row justify-end">
            <Button onClick={() => void draft()} disabled={busy} title={convertH.tooltip}>
              <Icon of={FileSymlink} />
              {convertH.label}
            </Button>
          </div>
        )}
      </form>
    </section>
  );
}

// PreviewResult is a preview's working, step by step, as the server worked it out.
function PreviewResult({ preview }: { preview: Preview }) {
  if (preview.conversions.length === 0) return <p className="fy-small fy-muted">No recorded SAFE or note waits to convert.</p>;
  return (
    <div className="fy-stack fy-stack--3" aria-label="The working">
      <p className="fy-small">
        At {preview.round_price} {preview.currency} a share, over {sharesText(preview.shares_before)} shares before the round
        {preview.post_money_part
          ? `. The post-money SAFEs take ${preview.post_money_part}% of the company, so its capitalisation once they convert is ${formatAmount(preview.capitalisation ?? "0")}.`
          : "."}
      </p>
      {preview.conversions.map((w) => (
        <WorkingCard key={w.instrument_id} w={w} currency={preview.currency} />
      ))}
    </div>
  );
}

function WorkingCard({ w, currency }: { w: Working; currency: string }) {
  const steps: string[] = [];
  if (w.ownership_at_cap) steps.push(`Ownership at the cap: ${w.ownership_at_cap}% of the capitalisation, counting this SAFE.`);
  if (w.cap_price) steps.push(`Cap price: ${w.cap_price}.`);
  if (w.discount_price) steps.push(`Discounted round price: ${w.discount_price}.`);
  steps.push(`Conversion price: ${w.price}, the lower, cut down to ${w.price_decimals} decimals.`);
  steps.push(`Whole shares (RND-SHARE): ${sharesText(w.shares)}, for ${formatAmount(w.converted.amount)} ${currency}.`);
  steps.push(`Remainder: ${formatAmount(w.left.amount)} ${currency} stays on 2700 until it is settled under the terms.`);
  return (
    <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-label={`${w.holder_name}'s conversion`}>
      <div className="fy-row justify-between gap-2">
        <strong>
          {w.holder_name} · {formWords[w.form as keyof typeof formWords] ?? w.form}
        </strong>
        {w.covered ? (
          <span className="fy-chip fy-chip--good">
            <Icon of={CircleCheck} />
            Automatic
          </span>
        ) : (
          <span className="fy-chip fy-chip--warning">
            <Icon of={TriangleAlert} />
            Accountant
          </span>
        )}
      </div>
      <ol className="fy-small fy-stack fy-stack--1">
        {steps.map((s) => (
          <li key={s}>{s}</li>
        ))}
      </ol>
      {w.review && (
        <p className="fy-small" role="note">
          <Icon of={TriangleAlert} /> {reviewText(w.review)}
        </p>
      )}
    </section>
  );
}

// Templates are the forms Fyniq converts by itself (FR-EQ-18): each enabled only by an approved enablement with its
// lawyer's confirmation attached, once. Any other form or term converts by a draft the accountant completes (⚠).
function Templates({ mayDraft }: { mayDraft: boolean }) {
  const confirm = useConfirm();
  const queries = useQueryClient();
  const q = useQuery({ queryKey: ["instrument-templates"], queryFn: listTemplates });
  const [status, setStatus] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const enableH = helpText("instrument-template.enable");
  const askH = helpText("instrument-template.ask");
  const refresh = async (text: string) => {
    setStatus(text);
    await queries.invalidateQueries({ queryKey: ["instrument-templates"] });
    await queries.invalidateQueries({ queryKey: ["instruments"] });
  };
  const enable = async (t: Template) => {
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/instrument-templates/{template_key}/enablement", { params: { path: { template_key: t.key }, header: { "Idempotency-Key": newKey() } }, body: {} }),
    );
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh("The enablement is drafted. Attach the lawyer's confirmation, then ask for approval.");
  };
  const ask = async (t: Template) => {
    if (!t.enablement_id) return;
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/instrument-template-enablements/{enablement_id}/approval", {
        params: { path: { enablement_id: t.enablement_id as string }, header: { "If-Match": `"${t.version}-draft"`, "Idempotency-Key": newKey() } },
      }),
    );
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh("Asked for approval. Another Owner or the Controller approves it.");
  };
  return (
    <section className="fy-card" aria-labelledby="templates-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="templates-title">
          <Icon of={ShieldCheck} /> Forms Fyniq converts
        </h2>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <p className="fy-small" role="status">
          {status}
        </p>
        {problem && <Refusal problem={problem} title="That was not done" />}
        {(q.data ?? []).map((t) => (
          <div key={t.key} className="fy-stack fy-stack--2">
            <div className="fy-row justify-between gap-2">
              <strong>{t.name}</strong>
              {t.enabled ? (
                <span className="fy-chip fy-chip--good">
                  <Icon of={CircleCheck} />
                  Automatic
                </span>
              ) : t.approval_id ? (
                <span className="fy-chip fy-chip--neutral">
                  <Icon of={Hourglass} />
                  <Link to="/approvals/$approvalId" params={{ approvalId: t.approval_id }}>
                    Waiting for approval
                  </Link>
                </span>
              ) : (
                <span className="fy-chip fy-chip--neutral">Not enabled</span>
              )}
            </div>
            <p className="fy-small fy-secondary">{t.covers}</p>
            <p className="fy-small fy-muted">{t.source}</p>
            {mayDraft && !t.enabled && !t.enablement_id && (
              <div>
                <Button size="sm" variant="secondary" onClick={() => void enable(t)} title={enableH.tooltip}>
                  {enableH.label}
                </Button>
              </div>
            )}
            {t.enablement_id && t.enablement_status === "draft" && (
              <>
                <EvidencePanel record={{ kind: "instrument-enablement", id: t.enablement_id, etag: `"${t.version}-draft"` }} draft
                  mayAttach={mayDraft && !t.approval_id} onChanged={(m) => void refresh(m)} />
                {mayDraft && !t.approval_id && (
                  <div>
                    <Button size="sm" onClick={() => void ask(t)} disabled={!t.evidence} title={askH.tooltip}>
                      <Icon of={Send} />
                      {askH.label}
                    </Button>
                  </div>
                )}
              </>
            )}
          </div>
        ))}
        <p className="fy-small fy-secondary">
          Any other form, and any SAFE with a discount, a most-favoured-nation clause or interest, converts by a draft the accountant completes (⚠).
        </p>
      </div>
    </section>
  );
}
