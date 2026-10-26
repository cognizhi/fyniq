import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { CircleCheck, CircleX, Plus, Save, Send, Trash2, TriangleAlert, Undo2 } from "lucide-react";
import { cloneElement, useEffect, useId, useState, type ReactElement } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { typed } from "../../format/decimal";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { EvidencePanel } from "../accounting/JournalEvidence";
import { money, StageChip, useCategories, useClaimGSTCodes, type Claim, type ClaimLine } from "./claims";

type Draft = components["schemas"]["ExpenseClaimDraft"];

// ReceiptForm is a receipt line as the person types it: amounts as typed, read when it is saved.
export interface ReceiptForm {
  key: string;
  date: string;
  merchant: string;
  category_id: string;
  description: string;
  amount: string;
  currency: string;
  gst_code: string;
  gst: string;
  paid: string;
  attendees: string;
  receipt_id: string;
  duplicate_reason: string;
}

export interface ClaimForm {
  date: string;
  title: string;
  notes: string;
  late_reason: string;
  advance_id: string;
  lines: ReceiptForm[];
  trips: ClaimLine[]; // mileage lines, kept as they are (the mileage screen changes them)
}

export function newReceipt(currency: string, on: string): ReceiptForm {
  return { key: newKey(), date: on, merchant: "", category_id: "", description: "", amount: "", currency, gst_code: "", gst: "", paid: "", attendees: "", receipt_id: "", duplicate_reason: "" };
}

// formOf is a saved claim as the form shows it.
export function formOf(c: Claim, base: string): ClaimForm {
  return {
    date: c.date ?? "",
    title: c.title ?? "",
    notes: c.notes ?? "",
    late_reason: c.late_reason ?? "",
    advance_id: c.advance_id ?? "",
    trips: c.lines.filter((l) => l.kind === "mileage"),
    lines: c.lines
      .filter((l) => l.kind !== "mileage")
      .map((l) => ({
        key: newKey(),
        date: l.date ?? "",
        merchant: l.merchant ?? "",
        category_id: l.category_id,
        description: l.description ?? "",
        amount: l.amount?.amount ?? "",
        currency: l.amount?.currency ?? base,
        gst_code: l.gst_code ?? "",
        gst: l.gst?.amount ?? "",
        paid: l.paid?.amount ?? "",
        attendees: l.attendees ?? "",
        receipt_id: l.receipt_id ?? "",
        duplicate_reason: l.duplicate_reason ?? "",
      })),
  };
}

// checkClaim reads the form into the API's draft, or names each field that cannot be read, all at once (ENG-22): the
// receipts in their order, then the trips, so receipt i is the API's lines[i]. What the expense policy and the posting
// rule say comes back from the server.
export function checkClaim(f: ClaimForm, base: string): { draft?: Draft; errors: Record<string, string> } {
  const errors: Record<string, string> = {};
  const lines: Draft["lines"] = [];
  f.lines.forEach((l, i) => {
    const amount = typed(l.amount);
    const gst = typed(l.gst);
    const paid = typed(l.paid);
    if (!l.category_id) errors[`lines.${i}.category_id`] = "Choose what the expense was for.";
    if (amount === null || amount === "") errors[`lines.${i}.amount`] = "Enter the amount on the receipt, such as 27.30.";
    if (gst === null) errors[`lines.${i}.gst`] = "Enter the GST on the receipt as an amount, or leave it empty.";
    if (paid === null) errors[`lines.${i}.paid`] = "Enter what left your account in the base currency, or leave it empty.";
    if (!/^[A-Z]{3}$/.test(l.currency)) errors[`lines.${i}.currency`] = "Enter the currency's three-letter code, such as SGD or IDR.";
    lines.push({
      kind: "receipt",
      date: l.date || undefined,
      merchant: l.merchant.trim() || undefined,
      category_id: l.category_id,
      description: l.description.trim() || undefined,
      amount: { amount: amount ?? "", currency: l.currency },
      gst_code: l.gst_code || undefined,
      gst: gst ? { amount: gst, currency: l.currency } : undefined,
      paid: paid && l.currency !== base ? { amount: paid, currency: base } : undefined,
      attendees: l.attendees.trim() || undefined,
      receipt_id: l.receipt_id || undefined,
      duplicate_reason: l.duplicate_reason.trim() || undefined,
    });
  });
  for (const { claimed_km: _km, rate: _rate, amount: _amount, ...trip } of f.trips) lines.push(trip); // what RND-MILEAGE made is the server's
  if (f.lines.length + f.trips.length === 0) errors.lines = "Add a receipt line.";
  if (Object.keys(errors).length > 0) return { errors };
  return {
    errors,
    draft: {
      date: f.date || undefined,
      title: f.title.trim() || undefined,
      notes: f.notes || undefined,
      late_reason: f.late_reason.trim() || undefined,
      advance_id: f.advance_id || undefined,
      lines,
    },
  };
}

// ClaimEditor is a claim: a new one, or one already saved (docs/design/screens ClaimEditor; FR-AP-14, FR-AP-15,
// FR-AP-17, FR-AP-20). Its claimant enters the receipts, attaches each one's file and submits it, confirming it's them;
// the expense policy's checks show as it would be submitted: ✖ stops it, ⚠ goes to the approver, and a line above its
// limit or past the deadline goes to finance too. A returned claim shows the approver's comment, to fix and submit
// again; a submitted one is withdrawn to change it. Anyone else who may see it sees it read-only.
export function ClaimEditor() {
  const { claimId } = useParams({ strict: false }) as { claimId?: string };
  const { session, organisation } = useSignedIn();
  const base = organisation?.base_currency ?? "SGD";
  const navigate = useNavigate();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const today = new Date().toISOString().slice(0, 10);
  const [form, setForm] = useState<ClaimForm | null>(claimId ? null : { date: today, title: "", notes: "", late_reason: "", advance_id: "", lines: [newReceipt(base, today)], trips: [] });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [said, setSaid] = useState("");
  const [submitKey, setSubmitKey] = useState(newKey);
  const loaded = useQuery({
    queryKey: ["expense-claim", claimId],
    enabled: Boolean(claimId),
    queryFn: async () => {
      const { data, error, response } = await api.GET("/v1/expense-claims/{claim_id}", { params: { path: { claim_id: claimId ?? "" } } });
      if (error) throw error;
      return { claim: data, etag: etagOf(response) };
    },
  });
  const claim = loaded.data?.claim;
  const categories = useCategories();
  const codes = useClaimGSTCodes(form?.date ?? today);
  const advances = useQuery({
    queryKey: ["cash-advances", "open", session.user_id],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/cash-advances", { params: { query: { open: true, person_id: session.user_id, page_size: 100 } } });
      if (error) throw error;
      return data.items.filter((a) => a.kind === "advance");
    },
  });
  const mine = !claim || claim.claimant.id === session.user_id;
  const editable = mine && (!claim || (claim.status === "draft" && claim.stage !== "submitted"));
  useEffect(() => {
    if (claim && form === null) setForm(formOf(claim, base));
  }, [claim, form, base]);

  const set = (patch: Partial<ClaimForm>) => form && setForm({ ...form, ...patch });
  const setLine = (i: number, patch: Partial<ReceiptForm>) => form && setForm({ ...form, lines: form.lines.map((l, j) => (j === i ? { ...l, ...patch } : l)) });

  const save = async (): Promise<{ claim: Claim; etag: string } | null> => {
    if (!form) return null;
    const { draft, errors } = checkClaim(form, base);
    setErrors(errors);
    if (!draft) return null;
    setBusy(true);
    setProblem(null);
    const out = claim
      ? await guarded(confirm, () =>
          api.PUT("/v1/expense-claims/{claim_id}", { params: { path: { claim_id: claim.id }, header: { "If-Match": loaded.data?.etag ?? "" } }, body: draft }),
        )
      : await guarded(confirm, () => api.POST("/v1/expense-claims", { params: { header: { "Idempotency-Key": newKey() } }, body: draft }));
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return null;
    }
    const saved = { claim: out.data, etag: etagOf(out.response) };
    queries.setQueryData(["expense-claim", out.data.id], saved);
    await queries.invalidateQueries({ queryKey: ["expense-claims"] });
    setForm(formOf(out.data, base));
    if (!claim) await navigate({ to: "/me/claims/$claimId", params: { claimId: out.data.id } });
    return saved;
  };

  const submit = async () => {
    const saved = await save();
    if (!saved) return;
    setBusy(true);
    const out = await guarded(confirm, () =>
      api.POST("/v1/expense-claims/{claim_id}/submit", { params: { path: { claim_id: saved.claim.id }, header: { "Idempotency-Key": submitKey, "If-Match": saved.etag } } }),
    );
    setBusy(false);
    setSubmitKey(newKey());
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["expense-claim", saved.claim.id] });
    await queries.invalidateQueries({ queryKey: ["expense-claims"] });
    setSaid("Submitted. It waits for approval; you can withdraw it to change it.");
  };

  const withdraw = async () => {
    if (!claim) return;
    setBusy(true);
    const out = await guarded(confirm, () =>
      api.POST("/v1/expense-claims/{claim_id}/withdraw", { params: { path: { claim_id: claim.id }, header: { "Idempotency-Key": newKey(), "If-Match": loaded.data?.etag ?? "" } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["expense-claim", claim.id] });
    await queries.invalidateQueries({ queryKey: ["expense-claims"] });
    setForm(formOf(out.data, base));
    setSaid("Withdrawn: it is a draft again. Change it and submit it again.");
  };

  const usable = (categories.data ?? []).filter((c) => (c.active && !c.mileage) || form?.lines.some((l) => l.category_id === c.id));
  const errorsOf = (claim?.checks ?? []).filter((c) => c.severity === "error");
  const warnings = (claim?.checks ?? []).filter((c) => c.severity === "warning");
  const saveText = helpText("claim.save");
  const submitText = helpText("claim.submit");

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/me/claims">My claims</Link>
          </nav>
          <h1 className="fy-h1">{claim ? claim.title || claim.number || "Claim" : "New claim"}</h1>
          {claim && (
            <p className="fy-row fy-row--wrap gap-2">
              <StageChip stage={claim.stage} />
              {claim.number && <span className="fy-mono fy-small">{claim.number}</span>}
              {claim.total && <span className="fy-num">{money(claim.total)}</span>}
            </p>
          )}
        </div>
      </div>
      {loaded.isError && <Refusal problem={problemOf(loaded.error)} title="The claim could not be opened" />}
      {problem && <Refusal problem={problem} title="The claim was not saved" />}
      {said && (
        <p className="fy-small" role="status">
          {said}
        </p>
      )}
      {claim?.returned && (
        <div className="fy-banner fy-banner--warning">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <strong>{claim.returned.by?.name ? `${claim.returned.by.name} returned it` : "It was returned"}</strong>
            <span className="fy-small">“{claim.returned.comment}” Fix it, then submit it again.</span>
          </div>
        </div>
      )}
      {claim && (errorsOf.length > 0 || warnings.length > 0) && (
        <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-label="Checks">
          <h2 className="fy-h3">{errorsOf.length > 0 ? `${errorsOf.length} to fix before it can be submitted` : "Submitted with these for the approver"}</h2>
          <ul className="fy-stack fy-stack--2">
            {[...errorsOf, ...warnings].map((c, i) => (
              <li key={`${c.code}-${i}`} className="fy-row gap-2">
                <span className={`fy-chip ${c.severity === "error" ? "fy-chip--critical" : "fy-chip--warning"}`}>
                  <Icon of={c.severity === "error" ? CircleX : TriangleAlert} />
                  {c.severity === "error" ? "Fix" : "Review"}
                </span>
                <span className="fy-small">
                  {c.message}. {c.fix}
                </span>
              </li>
            ))}
          </ul>
        </section>
      )}
      {claim && errorsOf.length === 0 && warnings.length === 0 && claim.status === "draft" && claim.total && (
        <p className="fy-small fy-row gap-2">
          <Icon of={CircleCheck} />
          It meets the expense policy as it stands.
        </p>
      )}

      {form && (
        <form
          className="fy-stack fy-stack--4"
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <fieldset className="fy-card fy-card--pad fy-stack fy-stack--3" disabled={!editable || busy}>
            <legend className="fy-h3">The claim</legend>
            <div className="fy-grid fy-grid--2">
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-title`}>
                  What it is for
                </label>
                <input className="fy-input" id={`${id}-title`} value={form.title} maxLength={120} onChange={(e) => set({ title: e.target.value })} />
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-date`}>
                  Date of the claim
                </label>
                <input className="fy-input" id={`${id}-date`} type="date" value={form.date} onChange={(e) => set({ date: e.target.value })} />
              </div>
            </div>
            {(advances.data ?? []).length > 0 || form.advance_id ? (
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-advance`}>
                  Settles a cash advance
                </label>
                <select className="fy-select" id={`${id}-advance`} value={form.advance_id} onChange={(e) => set({ advance_id: e.target.value })} aria-describedby={`${id}-advance-help`}>
                  <option value="">None: the company pays it back</option>
                  {(advances.data ?? []).map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.number} · {money(a.open)} open
                    </option>
                  ))}
                  {form.advance_id && !(advances.data ?? []).some((a) => a.id === form.advance_id) && <option value={form.advance_id}>The advance it names</option>}
                </select>
                <span className="fy-help" id={`${id}-advance-help`}>
                  {claim?.settles ? `It settles ${money(claim.settles)} of the advance; the rest is paid back to you.` : "The claim settles what is still open on the advance; the rest is paid back to you."}
                </span>
              </div>
            ) : null}
          </fieldset>

          {form.lines.map((l, i) => (
            <fieldset key={l.key} className="fy-card fy-card--pad fy-stack fy-stack--3" disabled={!editable || busy} aria-label={`Receipt ${i + 1}`}>
              <legend className="fy-h3">Receipt {i + 1}</legend>
              <div className="fy-grid fy-grid--2">
                <Field id={`${id}-${i}-merchant`} label="Paid to" error={errors[`lines.${i}.merchant`]}>
                  <input className="fy-input" value={l.merchant} maxLength={120} onChange={(e) => setLine(i, { merchant: e.target.value })} />
                </Field>
                <Field id={`${id}-${i}-date`} label="Date on the receipt">
                  <input className="fy-input" type="date" value={l.date} onChange={(e) => setLine(i, { date: e.target.value })} />
                </Field>
                <Field id={`${id}-${i}-category`} label="Category" error={errors[`lines.${i}.category_id`]}>
                  <select className="fy-select" value={l.category_id} onChange={(e) => setLine(i, { category_id: e.target.value })}>
                    <option value="">Choose what it was for</option>
                    {usable.map((c) => (
                      <option key={c.id} value={c.id}>
                        {c.name}
                        {c.limit ? ` (up to ${money(c.limit)} a line)` : ""}
                      </option>
                    ))}
                  </select>
                </Field>
                <Field id={`${id}-${i}-description`} label="What it was for (optional)">
                  <input className="fy-input" value={l.description} maxLength={500} onChange={(e) => setLine(i, { description: e.target.value })} />
                </Field>
                <Field id={`${id}-${i}-amount`} label={`Amount on the receipt (${l.currency})`} error={errors[`lines.${i}.amount`]}>
                  <input className="fy-input fy-num" inputMode="decimal" value={l.amount} onChange={(e) => setLine(i, { amount: e.target.value })} />
                </Field>
                <Field id={`${id}-${i}-currency`} label="Currency" error={errors[`lines.${i}.currency`]}>
                  <input className="fy-input" value={l.currency} maxLength={3} onChange={(e) => setLine(i, { currency: e.target.value.toUpperCase() })} />
                </Field>
                <Field id={`${id}-${i}-gst-code`} label="GST on the receipt">
                  <select className="fy-select" value={l.gst_code} onChange={(e) => setLine(i, { gst_code: e.target.value })}>
                    <option value="">Choose the GST code</option>
                    {(codes.data?.codes ?? []).map((c) => (
                      <option key={c.code} value={c.code}>
                        {c.code}: {c.description}
                      </option>
                    ))}
                  </select>
                </Field>
                <Field id={`${id}-${i}-gst`} label={`GST shown (${l.currency}, optional)`} error={errors[`lines.${i}.gst`]}>
                  <input className="fy-input fy-num" inputMode="decimal" value={l.gst} onChange={(e) => setLine(i, { gst: e.target.value })} />
                </Field>
                {l.currency !== base && (
                  <Field id={`${id}-${i}-paid`} label={`What left your account (${base}, optional)`} error={errors[`lines.${i}.paid`]}>
                    <input className="fy-input fy-num" inputMode="decimal" value={l.paid} onChange={(e) => setLine(i, { paid: e.target.value })} />
                  </Field>
                )}
                <Field id={`${id}-${i}-attendees`} label="Who attended (meals and entertainment)">
                  <input className="fy-input" value={l.attendees} maxLength={500} onChange={(e) => setLine(i, { attendees: e.target.value })} />
                </Field>
                {claim && (
                  <ReceiptPick claimId={claim.id} id={`${id}-${i}-receipt`} value={l.receipt_id} onChange={(v) => setLine(i, { receipt_id: v })} />
                )}
                {claim?.checks.some((c) => c.code === "CLAIM-26" && (c.field ?? "").startsWith(`lines[${i}].`)) || l.duplicate_reason ? (
                  <Field id={`${id}-${i}-duplicate`} label="Why it is not a duplicate">
                    <input className="fy-input" value={l.duplicate_reason} maxLength={500} onChange={(e) => setLine(i, { duplicate_reason: e.target.value })} />
                  </Field>
                ) : null}
              </div>
              {editable && form.lines.length > 1 && (
                <Button variant="ghost" size="sm" className="self-start" onClick={() => setForm({ ...form, lines: form.lines.filter((_, j) => j !== i) })} aria-label={`Remove receipt ${i + 1}`}>
                  <Icon of={Trash2} />
                  Remove this receipt
                </Button>
              )}
            </fieldset>
          ))}
          {form.trips.length > 0 && (
            <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-label="Trips">
              <h2 className="fy-h3">Trips in your own vehicle</h2>
              <ul className="fy-stack fy-stack--2">
                {form.trips.map((t, i) => (
                  <li key={i} className="fy-small">
                    {date(t.date)} · {t.from} to {t.to}
                    {t.return ? " and back" : ""} · {t.claimed_km ?? t.distance} km{t.amount ? ` · ${money(t.amount)}` : ""}
                  </li>
                ))}
              </ul>
            </section>
          )}
          {errors.lines && <p className="fy-error">{errors.lines}</p>}
          {editable && (
            <Button variant="ghost" className="self-start" onClick={() => setForm({ ...form, lines: [...form.lines, newReceipt(base, form.date)] })}>
              <Icon of={Plus} />
              Add a receipt
            </Button>
          )}
          {editable && (warnings.some((w) => w.code === "CLAIM-PAST-DEADLINE") || form.late_reason) && (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-late`}>
                Why it is claimed late
              </label>
              <input className="fy-input" id={`${id}-late`} value={form.late_reason} maxLength={500} onChange={(e) => set({ late_reason: e.target.value })} />
            </div>
          )}
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-notes`}>
              Notes for the approver (optional)
            </label>
            <textarea className="fy-input" id={`${id}-notes`} value={form.notes} maxLength={2000} disabled={!editable || busy} onChange={(e) => set({ notes: e.target.value })} />
          </div>
          {editable && (
            <div className="fy-row fy-row--wrap gap-2">
              <Button type="submit" variant="secondary" disabled={busy} title={saveText.tooltip}>
                <Icon of={Save} />
                {saveText.label}
              </Button>
              <Button onClick={() => void submit()} disabled={busy} title={submitText.tooltip}>
                <Icon of={Send} />
                {submitText.label}
              </Button>
            </div>
          )}
          {mine && claim?.stage === "submitted" && (
            <Button variant="secondary" className="self-start" onClick={() => void withdraw()} disabled={busy} title={helpText("claim.withdraw").tooltip}>
              <Icon of={Undo2} />
              {helpText("claim.withdraw").label}
            </Button>
          )}
        </form>
      )}
      {claim && <EvidencePanel record={{ kind: "claim", id: claim.id, etag: loaded.data?.etag ?? "" }} draft={claim.status === "draft"} mayAttach={editable} onChanged={setSaid} />}
      {claim && claim.ledger_impact.length > 0 && (
        <section className="fy-card fy-expert-only" aria-label="Ledger impact">
          <div className="fy-card__head">
            <h2 className="fy-h3">Ledger impact</h2>
            <span className="fy-small fy-muted">{claim.status === "draft" ? "What approving it posts" : "As posted"}</span>
          </div>
          <table className="fy-table fy-table--compact fy-ledger">
            <thead>
              <tr>
                <th scope="col">Line</th>
                <th scope="col" className="fy-num">
                  Debit ({base})
                </th>
                <th scope="col" className="fy-num">
                  Credit ({base})
                </th>
              </tr>
            </thead>
            <tbody>
              {claim.ledger_impact.map((l) => {
                const credit = l.base_amount.amount.startsWith("-");
                const amount = formatAmount(l.base_amount.amount.replace(/^-/, ""));
                return (
                  <tr key={l.line_no}>
                    <td>{l.description || "Line"}</td>
                    <td className="fy-num">{credit ? "" : amount}</td>
                    <td className="fy-num">{credit ? amount : ""}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </section>
      )}
    </div>
  );
}

// Field is a labelled control with its error under it, tied by aria-describedby (MASTER.md "Forms").
function Field({ id, label, error, children }: { id: string; label: string; error?: string; children: ReactElement<{ id?: string; "aria-invalid"?: boolean; "aria-describedby"?: string }> }) {
  const child = cloneElement(children, { id, "aria-invalid": error ? true : undefined, "aria-describedby": error ? `${id}-error` : undefined });
  return (
    <div className="fy-field">
      <label className="fy-label" htmlFor={id}>
        {label}
      </label>
      {child}
      {error && (
        <span className="fy-error" id={`${id}-error`}>
          {error}
        </span>
      )}
    </div>
  );
}

// ReceiptPick names a line's receipt among the files attached to the claim.
function ReceiptPick({ claimId, id, value, onChange }: { claimId: string; id: string; value: string; onChange: (v: string) => void }) {
  const files = useQuery({
    queryKey: ["claim-evidence", claimId],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/expense-claims/{claim_id}/evidence", { params: { path: { claim_id: claimId }, query: { page_size: 100 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const live = (files.data ?? []).filter((e) => e.state !== "removed");
  return (
    <Field id={id} label="Its receipt">
      <select className="fy-select" value={value} onChange={(e) => onChange(e.target.value)}>
        <option value="">{live.length === 0 ? "Attach the receipt below first" : "Choose the receipt's file"}</option>
        {live.map((e) => (
          <option key={e.id} value={e.id}>
            {e.file_name}
          </option>
        ))}
      </select>
    </Field>
  );
}
