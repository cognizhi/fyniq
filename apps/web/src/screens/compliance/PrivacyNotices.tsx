import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronRight, CircleCheck, CircleMinus, Plus, Stamp } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
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
import { PolicyLibrary } from "./Policies";

type Purpose = components["schemas"]["ConsentPurposeView"];
type Consent = components["schemas"]["ConsentView"];

async function purposes() {
  const { data, error } = await api.GET("/v1/compliance/consent-purposes");
  if (error) throw error;
  return data.items;
}

async function consents() {
  const { data, error } = await api.GET("/v1/compliance/consents");
  if (error) throw error;
  return data.items;
}

// people are those a purpose may be for: the members, or the contacts, as "member:<user id>" or "contact:<contact id>".
async function people(audience: Purpose["audience"]) {
  if (audience === "members") {
    const { data, error } = await api.GET("/v1/members", { params: { query: { page_size: 200 } } });
    if (error) throw error;
    return (data.items ?? []).map((m) => ({ ref: `member:${m.user_id}`, name: m.display_name }));
  }
  const { data, error } = await api.GET("/v1/contacts", { params: { query: { page_size: 500 } } });
  if (error) throw error;
  return (data.items ?? []).map((c) => ({ ref: `contact:${c.id}`, name: c.legal_name }));
}

// PrivacyNotices is Compliance › Privacy notices and consent (CMP-PD-03, CMP-PD-01; rev 0.244): the members' and the
// contacts' notices, published from Fyniq's drafts as policies are, and consent for the purposes that need it, which
// the Compliance officer records with its withdrawal. None needs it by default.
export function PrivacyNotices() {
  const id = useId();
  const h = helpText("compliance.privacy-notices");
  const { session } = useSignedIn();
  const officer = (session.roles ?? []).includes("dpo");
  const list = useQuery({ queryKey: ["consent-purposes"], queryFn: purposes });
  const records = useQuery({ queryKey: ["consents"], queryFn: consents });
  const [adding, setAdding] = useState(false);
  const [recording, setRecording] = useState<Purpose | null>(null);
  const names = new Map((list.data ?? []).map((p) => [p.id, p.name] as const));
  return (
    <div className="fy-page fy-enter">
      <nav className="fy-breadcrumb" aria-label="Breadcrumb">
        <Link to="/compliance">Compliance</Link>
        <Icon of={ChevronRight} />
        <span>{h.label}</span>
      </nav>
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">{h.label}</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
      </div>
      <PolicyLibrary kind="notice" title="Privacy notices" />
      <section className="fy-card" aria-labelledby={`${id}-purposes`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-purposes`} title={helpText("consent.purpose").tooltip}>
            Purposes and consent
          </h2>
          {officer && (
            <Button size="sm" onClick={() => setAdding(true)}>
              <Icon of={Plus} />
              Add a purpose
            </Button>
          )}
        </div>
        {list.isError && <Refusal problem={problemOf(list.error)} title="The purposes could not be shown" />}
        {list.data && list.data.length === 0 && (
          <p className="fy-card__body fy-small fy-secondary">
            No purpose is defined. Fyniq uses members' and contacts' data for the work, the business relationship and the records the law requires, which need no consent.
            Add a purpose only for something more, such as a newsletter.
          </p>
        )}
        {list.data && list.data.length > 0 && (
          <ul className="fy-list" aria-labelledby={`${id}-purposes`}>
            {list.data.map((p) => (
              <li key={p.id} className="fy-list__item">
                <span className="fy-list__main">
                  <span className="fy-list__title">{p.name}</span>
                  <span className="fy-list__meta whitespace-normal">
                    For {p.audience} · {p.description}
                  </span>
                </span>
                {p.needs_consent ? (
                  <span className="fy-chip fy-chip--neutral">
                    <Icon of={CircleCheck} />
                    {p.given} consent{p.given === 1 ? "" : "s"} standing
                  </span>
                ) : (
                  <span className="fy-chip fy-chip--neutral">
                    <Icon of={CircleMinus} />
                    No consent needed
                  </span>
                )}
                {officer && p.needs_consent && (
                  <Button size="sm" variant="secondary" onClick={() => setRecording(p)} aria-label={`Record consent for ${p.name}`} title={helpText("consent.record").tooltip}>
                    <Icon of={Stamp} />
                    Record
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>
      {records.data && records.data.length > 0 && (
        <section className="fy-card" aria-labelledby={`${id}-records`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-records`}>
              Consents given and withdrawn
            </h2>
          </div>
          <ul className="fy-list" aria-labelledby={`${id}-records`}>
            {records.data
              .slice()
              .reverse()
              .map((c: Consent) => (
                <li key={c.id} className="fy-list__item">
                  <span className="fy-list__main">
                    <span className="fy-list__title">
                      {c.event === "given" ? "Given" : "Withdrawn"}: {names.get(c.purpose_id) ?? "a purpose"}
                    </span>
                    <span className="fy-list__meta whitespace-normal">
                      {c.person} · {date(c.at)}
                      {c.note && ` · ${c.note}`}
                    </span>
                  </span>
                </li>
              ))}
          </ul>
        </section>
      )}
      {adding && <PurposeForm onClose={() => setAdding(false)} />}
      {recording && <ConsentForm purpose={recording} onClose={() => setRecording(null)} />}
    </div>
  );
}

// PurposeForm adds a purpose for members' or contacts' data, saying whether it needs consent (CMP-PD-03).
function PurposeForm({ onClose }: { onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [form, setForm] = useState({ name: "", description: "", audience: "contacts" as Purpose["audience"], needs_consent: true });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const save = useMutation({
    mutationFn: async () => {
      const out = await guarded(confirm, () => api.POST("/v1/compliance/consent-purposes", { params: { header: { "Idempotency-Key": newKey() } }, body: form }));
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await queries.invalidateQueries({ queryKey: ["consent-purposes"] });
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()} dismissible={!save.isPending} eyebrow="Privacy notices and consent" title="Add a purpose"
      description="A purpose never changes once added: add another instead.">
      {problem && <Refusal problem={problem} title="The purpose was not added" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-name`}>
            Purpose
          </label>
          <input className="fy-input" id={`${id}-name`} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-description`}>
            What it means for the person
          </label>
          <textarea className="fy-textarea" id={`${id}-description`} value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-audience`}>
            Whose data
          </label>
          <select className="fy-select" id={`${id}-audience`} value={form.audience} onChange={(e) => setForm({ ...form, audience: e.target.value as Purpose["audience"] })}>
            <option value="contacts">Contacts</option>
            <option value="members">Members</option>
          </select>
        </div>
        <label className="fy-check">
          <input type="checkbox" checked={form.needs_consent} onChange={(e) => setForm({ ...form, needs_consent: e.target.checked })} />
          It needs each person's consent
        </label>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={save.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={save.isPending}>
            Add the purpose
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// ConsentForm records a consent given or withdrawn for a purpose that needs it (CMP-PD-03).
function ConsentForm({ purpose, onClose }: { purpose: Purpose; onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const who = useQuery({ queryKey: ["consent-people", purpose.audience], queryFn: () => people(purpose.audience) });
  const [form, setForm] = useState({ person: "", event: "given" as Consent["event"], note: "" });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const save = useMutation({
    mutationFn: async () => {
      const out = await guarded(confirm, () =>
        api.POST("/v1/compliance/consents", { params: { header: { "Idempotency-Key": newKey() } }, body: { purpose_id: purpose.id, ...form, note: form.note || undefined } }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await Promise.all(["consents", "consent-purposes"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()} dismissible={!save.isPending} eyebrow={purpose.name} title="Record consent"
      description="A consent is given only when none stands, and withdrawn only when one does. Withdrawal stops further use.">
      {problem && <Refusal problem={problem} title="The consent was not recorded" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-person`}>
            {purpose.audience === "members" ? "Member" : "Contact"}
          </label>
          <select className="fy-select" id={`${id}-person`} value={form.person} onChange={(e) => setForm({ ...form, person: e.target.value })}>
            <option value="">Choose…</option>
            {(who.data ?? []).map((p) => (
              <option key={p.ref} value={p.ref}>
                {p.name}
              </option>
            ))}
          </select>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-event`}>
            What happened
          </label>
          <select className="fy-select" id={`${id}-event`} value={form.event} onChange={(e) => setForm({ ...form, event: e.target.value as Consent["event"] })}>
            <option value="given">They gave their consent</option>
            <option value="withdrawn">They withdrew it</option>
          </select>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-note`}>
            How (optional)
          </label>
          <input className="fy-input" id={`${id}-note`} value={form.note} onChange={(e) => setForm({ ...form, note: e.target.value })} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={save.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={save.isPending || form.person === ""}>
            Record it
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
