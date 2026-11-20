import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Lock, LockOpen, Plus } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Hold = components["schemas"]["RetentionHoldView"];
type HoldInput = components["schemas"]["RetentionHoldInput"];

async function holds(all: boolean) {
  const { data, error } = await api.GET("/v1/compliance/holds", { params: { query: { state: all ? "all" : "in_force" } } });
  if (error) throw error;
  return data;
}

async function contactChoices() {
  const { data, error } = await api.GET("/v1/contacts", { params: { query: { page_size: 200 } } });
  if (error) throw error;
  return (data.items ?? []).map((c) => ({ id: c.id, name: c.legal_name }));
}

// whatItNames is a hold's subject in words.
export function whatItNames(h: Hold): string {
  if (h.kind === "record") return `${h.document_type ? `${h.document_type} ` : "Record "}${h.document_number || h.document_id}`;
  if (h.kind === "contact") return `Contact ${h.contact_name || h.contact_id}`;
  return `Financial year ${h.year_number}, ${date(h.year_starts_on ?? "")} to ${date(h.year_ends_on ?? "")}`;
}

// HoldsTab is Compliance › Data protection › Holds (AUD-11, rev 0.617): the legal holds on a record, a contact or a financial
// year. While one stands the purge after retention skips what it covers, the database refuses to delete it, and the evidence files
// it covers keep a storage hold. The Owner and the Compliance officer place and lift them, each with a reason.
export function HoldsTab() {
  const id = useId();
  const { session } = useSignedIn();
  const roles = session.roles ?? [];
  const mayChange = roles.includes("dpo") || roles.includes("owner");
  const [showLifted, setShowLifted] = useState(false);
  const list = useQuery({ queryKey: ["retention-holds", showLifted], queryFn: () => holds(showLifted) });
  const [placing, setPlacing] = useState(false);
  const [lifting, setLifting] = useState<Hold | null>(null);
  if (list.isError) return <Refusal problem={problemOf(list.error)} title="The holds could not be shown" />;
  if (!list.data) return <p className="fy-small">Loading the holds…</p>;
  return (
    <>
      <section className="fy-card" aria-labelledby={`${id}-holds`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-holds`}>
            Legal holds
          </h2>
          <label className="fy-row gap-2 fy-small">
            <input type="checkbox" checked={showLifted} onChange={(e) => setShowLifted(e.target.checked)} />
            Show the holds that were lifted
          </label>
          {mayChange && (
            <Button size="sm" onClick={() => setPlacing(true)}>
              <Icon of={Plus} />
              Place a hold
            </Button>
          )}
        </div>
        <p className="fy-card__body fy-small fy-secondary">
          While a hold stands, the purge after retention skips what it covers, the database refuses to delete it, and its evidence files stay in the store. Placing and lifting a hold each need a reason and are kept in the audit trail.
        </p>
        {list.data.items.length === 0 ? (
          <p className="fy-card__body fy-small fy-secondary">No hold stands. Place one on a record, a contact or a financial year when a dispute, claim, audit or inquiry needs the records kept.</p>
        ) : (
          <ul className="fy-list" aria-labelledby={`${id}-holds`}>
            {list.data.items.map((h) => (
              <li key={h.hold_id} className="fy-list__item fy-list__item--wrap">
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">{whatItNames(h)}</span>
                  <span className="fy-list__meta whitespace-normal">
                    {h.reason} · placed {date(h.placed_at.slice(0, 10))}
                    {!h.in_force && ` · lifted ${date((h.lifted_at ?? "").slice(0, 10))}: ${h.lift_reason}`}
                  </span>
                </span>
                {h.in_force ? (
                  <span className="fy-chip fy-chip--warning">
                    <Icon of={Lock} />
                    Held
                  </span>
                ) : (
                  <span className="fy-chip">
                    <Icon of={LockOpen} />
                    Lifted
                  </span>
                )}
                {mayChange && h.in_force && (
                  <Button size="sm" variant="secondary" onClick={() => setLifting(h)} aria-label={`Lift the hold on ${whatItNames(h)}`}>
                    <Icon of={LockOpen} />
                    Lift
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>
      {placing && <PlaceHold onClose={() => setPlacing(false)} />}
      {lifting && <LiftHold hold={lifting} onClose={() => setLifting(null)} />}
    </>
  );
}

function PlaceHold({ onClose }: { onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const contacts = useQuery({ queryKey: ["hold-contacts"], queryFn: contactChoices });
  const [form, setForm] = useState<{ kind: HoldInput["kind"]; document_id: string; contact_id: string; financial_year: string; reason: string }>({
    kind: "financial_year",
    document_id: "",
    contact_id: "",
    financial_year: "1",
    reason: "",
  });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const place = useMutation({
    mutationFn: async () => {
      const body: HoldInput = { kind: form.kind, reason: form.reason.trim() };
      if (form.kind === "record") body.document_id = form.document_id.trim();
      if (form.kind === "contact") body.contact_id = form.contact_id;
      if (form.kind === "financial_year") body.financial_year = Number(form.financial_year);
      const out = await api.POST("/v1/compliance/holds", { params: { header: { "Idempotency-Key": newKey() } }, body });
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await queries.invalidateQueries({ queryKey: ["retention-holds"] });
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    place.mutate();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !place.isPending && onClose()} dismissible={!place.isPending} eyebrow="Legal holds" title="Place a hold"
      description="Say what to keep and why. Nothing is deleted from what a hold covers, and the purge after retention skips it, until the hold is lifted.">
      {problem && <Refusal problem={problem} title="The hold was not placed" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-kind`}>
            What to hold
          </label>
          <select className="fy-select" id={`${id}-kind`} value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value as HoldInput["kind"] })}>
            <option value="financial_year">A financial year</option>
            <option value="contact">A contact and the documents that name them</option>
            <option value="record">One record</option>
          </select>
        </div>
        {form.kind === "financial_year" && (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-year`}>
              Financial year number (1 is the first)
            </label>
            <input className="fy-input" id={`${id}-year`} type="number" min={1} max={200} value={form.financial_year} onChange={(e) => setForm({ ...form, financial_year: e.target.value })} />
          </div>
        )}
        {form.kind === "contact" && (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-contact`}>
              Contact
            </label>
            <select className="fy-select" id={`${id}-contact`} value={form.contact_id} onChange={(e) => setForm({ ...form, contact_id: e.target.value })}>
              <option value="">Choose a contact</option>
              {(contacts.data ?? []).map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          </div>
        )}
        {form.kind === "record" && (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-document`}>
              The record's ID
            </label>
            <input className="fy-input" id={`${id}-document`} value={form.document_id} onChange={(e) => setForm({ ...form, document_id: e.target.value })} aria-describedby={`${id}-document-hint`} />
            <span className="fy-hint" id={`${id}-document-hint`}>
              Copy it from the address of the invoice, bill or journal's page.
            </span>
          </div>
        )}
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-reason`}>
            Why
          </label>
          <textarea className="fy-input" id={`${id}-reason`} rows={3} maxLength={500} value={form.reason} onChange={(e) => setForm({ ...form, reason: e.target.value })} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={place.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={place.isPending}>
            Place the hold
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function LiftHold({ hold, onClose }: { hold: Hold; onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [reason, setReason] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const lift = useMutation({
    mutationFn: async () => {
      const out = await guarded(confirm, () =>
        api.POST("/v1/compliance/holds/{hold_id}/lift", { params: { path: { hold_id: hold.hold_id }, header: { "Idempotency-Key": newKey() } }, body: { reason: reason.trim() } }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await queries.invalidateQueries({ queryKey: ["retention-holds"] });
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    lift.mutate();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !lift.isPending && onClose()} dismissible={!lift.isPending} eyebrow="Legal holds" title="Lift the hold"
      description={`${whatItNames(hold)}. Once it is lifted, the purge after retention may reach what it covered, and a lifted hold never stands again: place a new one to hold again.`}>
      {problem && <Refusal problem={problem} title="The hold was not lifted" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-reason`}>
            Why it is lifted
          </label>
          <textarea className="fy-input" id={`${id}-reason`} rows={3} maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={lift.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={lift.isPending}>
            Lift the hold
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
