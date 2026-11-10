import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleAlert, Plus, ShieldCheck, TriangleAlert, type LucideIcon } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Incident = components["schemas"]["IncidentView"];
type Severity = Incident["severity"];
type Breach = components["schemas"]["BreachView"];

async function incidents() {
  const { data, error } = await api.GET("/v1/compliance/incidents");
  if (error) throw error;
  return data.items;
}

async function breaches() {
  const { data, error } = await api.GET("/v1/compliance/breaches");
  if (error) throw error;
  return data.items;
}

// The severities, worst first, as icon, chip and words (docs/runbooks/incident-response.md); colour never alone.
const severities: Record<Severity, { label: string; chip: string; icon: LucideIcon }> = {
  critical: { label: "Critical", chip: "fy-chip--critical", icon: CircleAlert },
  high: { label: "High", chip: "fy-chip--serious", icon: TriangleAlert },
  medium: { label: "Medium", chip: "fy-chip--warning", icon: TriangleAlert },
  low: { label: "Low", chip: "fy-chip--neutral", icon: ShieldCheck },
};

const stepWords: Record<string, string> = {
  open: "Open",
  contained: "Contained",
  assessed: "Assessed",
  notified: "Notified",
  recovered: "Recovered",
  reviewed: "Reviewed and closed",
};

// incidentNext are the steps the database accepts next (FY322): each once, nothing after the review, notifying after
// assessing, recovering after containing, the review after recovering.
export function incidentNext(i: Incident): string[] {
  const done = new Set<string>(i.steps.map((s) => s.step));
  if (done.has("reviewed")) return [];
  return ["contained", "assessed", "notified", "recovered", "reviewed"].filter(
    (s) =>
      !done.has(s) &&
      (s !== "notified" || done.has("assessed")) &&
      (s !== "recovered" || done.has("contained")) &&
      (s !== "reviewed" || done.has("recovered")),
  );
}

// IncidentsTab is the incident register of Compliance › Controls (SEC-32; p1-security-hardening design D10): each
// security incident with its severity and its steps in the runbook's order. The DPO keeps it (Appendix D, Compliance
// Center: C/A); the Compliance Center's other readers read it.
export function IncidentsTab() {
  const id = useId();
  const { session } = useSignedIn();
  const officer = (session.roles ?? []).includes("dpo");
  const list = useQuery({ queryKey: ["incidents"], queryFn: incidents });
  const [opening, setOpening] = useState(false);
  const [stepping, setStepping] = useState<Incident | null>(null);
  if (list.isError) return <Refusal problem={problemOf(list.error)} title="The incident register could not be shown" />;
  if (!list.data) return <p className="fy-small">Loading the incident register…</p>;
  const open = list.data.filter((i) => i.status !== "reviewed").length;
  return (
    <>
      <section className="fy-card" aria-labelledby={`${id}-incidents`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-incidents`} title={helpText("compliance.incidents").tooltip}>
            Incident register
          </h2>
          <span className="fy-small fy-muted">{open === 1 ? "1 open" : `${open} open`}</span>
          {officer && (
            <Button size="sm" onClick={() => setOpening(true)}>
              <Icon of={Plus} />
              Open an incident
            </Button>
          )}
        </div>
        {list.data.length === 0 ? (
          <p className="fy-card__body fy-small fy-secondary">
            No incident recorded. Open one as soon as something is suspected: its steps follow the incident runbook, and personal data involved goes to the breach register too.
          </p>
        ) : (
          <ul className="fy-list" aria-labelledby={`${id}-incidents`}>
            {list.data
              .slice()
              .reverse()
              .map((i) => {
                const s = severities[i.severity];
                return (
                  <li key={i.id} className="fy-list__item">
                    <span className="fy-list__main">
                      <span className="fy-list__title whitespace-normal">{i.title}</span>
                      <span className="fy-list__meta whitespace-normal">
                        Seen {dateTime(i.detected_at)} · {stepWords[i.status]}
                        {i.breach_id ? " · linked to the breach register" : ""}
                      </span>
                      {i.steps.length > 0 && <span className="fy-list__meta whitespace-normal">{i.steps.at(-1)!.note}</span>}
                    </span>
                    <span className={`fy-chip ${s.chip}`}>
                      <Icon of={s.icon} />
                      {s.label}
                    </span>
                    {officer && incidentNext(i).length > 0 && (
                      <Button size="sm" variant="secondary" onClick={() => setStepping(i)} aria-label={`Next step for ${i.title}`}>
                        <Icon of={ShieldCheck} />
                        Next step
                      </Button>
                    )}
                  </li>
                );
              })}
          </ul>
        )}
      </section>
      {opening && <IncidentForm onClose={() => setOpening(false)} />}
      {stepping && <IncidentStepForm incident={stepping} onClose={() => setStepping(null)} />}
    </>
  );
}

// IncidentForm opens an incident, or a suspected one, as soon as it is seen (SEC-32).
function IncidentForm({ onClose }: { onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [form, setForm] = useState({ title: "", summary: "", severity: "high" as Severity });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const save = useMutation({
    mutationFn: async () => {
      const out = await guarded(confirm, () =>
        api.POST("/v1/compliance/incidents", {
          params: { header: { "Idempotency-Key": newKey() } },
          body: { title: form.title.trim(), summary: form.summary.trim(), severity: form.severity },
        }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await queries.invalidateQueries({ queryKey: ["incidents"] });
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && !save.isPending && onClose()}
      dismissible={!save.isPending}
      eyebrow="Incident register"
      title="Open an incident"
      description="Open it as soon as something is suspected, then follow the incident runbook. Never paste a secret, a token, a full NRIC or a bank number."
    >
      {problem && <Refusal problem={problem} title="The incident was not opened" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-title`}>
            What happened, in a few words
          </label>
          <input className="fy-input" id={`${id}-title`} value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-summary`}>
            What is known
          </label>
          <textarea className="fy-textarea" id={`${id}-summary`} value={form.summary} onChange={(e) => setForm({ ...form, summary: e.target.value })} />
        </div>
        <fieldset className="fy-field" title={helpText("incidents.severity").tooltip}>
          <legend className="fy-label">{helpText("incidents.severity").label}</legend>
          {(Object.keys(severities) as Severity[]).map((s) => (
            <label key={s} className="fy-check">
              <input type="radio" name={`${id}-severity`} checked={form.severity === s} onChange={() => setForm({ ...form, severity: s })} />
              {severities[s].label}
            </label>
          ))}
        </fieldset>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={save.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={save.isPending || form.title.trim() === "" || form.summary.trim() === ""}>
            Open it
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// IncidentStepForm adds an incident's next step (SEC-32): contained, assessed (with the breach register's breach when
// personal data was involved), notified, recovered, reviewed.
function IncidentStepForm({ incident, onClose }: { incident: Incident; onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const steps = incidentNext(incident);
  const register = useQuery({ queryKey: ["breaches"], queryFn: breaches, retry: false });
  const [form, setForm] = useState({ step: steps[0]!, note: "", breach: "" });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const save = useMutation({
    mutationFn: async () => {
      const body = { step: form.step as "contained", note: form.note.trim(), breach_id: form.step === "assessed" && form.breach ? form.breach : undefined };
      const out = await guarded(confirm, () =>
        api.POST("/v1/compliance/incidents/{incident_id}/steps", {
          params: { path: { incident_id: incident.id }, header: { "Idempotency-Key": newKey(), "If-Match": `"s${incident.steps.length}"` } },
          body,
        }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await queries.invalidateQueries({ queryKey: ["incidents"] });
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()} dismissible={!save.isPending} eyebrow={incident.title} title="Next step">
      {problem && <Refusal problem={problem} title="The step was not recorded" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-step`}>
            Step
          </label>
          <select className="fy-select" id={`${id}-step`} value={form.step} onChange={(e) => setForm({ ...form, step: e.target.value })}>
            {steps.map((s) => (
              <option key={s} value={s}>
                {stepWords[s]}
              </option>
            ))}
          </select>
        </div>
        {form.step === "assessed" && (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-breach`}>
              The breach, if personal data was involved
            </label>
            <select className="fy-select" id={`${id}-breach`} aria-describedby={`${id}-breach-help`} value={form.breach} onChange={(e) => setForm({ ...form, breach: e.target.value })}>
              <option value="">No personal data was involved</option>
              {(register.data ?? []).map((b: Breach) => (
                <option key={b.id} value={b.id}>
                  {b.title}
                </option>
              ))}
            </select>
            <span className="fy-help" id={`${id}-breach-help`}>
              Record the breach first in Compliance › Data protection › Breaches: its 30 days run from when the company became aware of it.
            </span>
          </div>
        )}
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-note`}>
            What was found or done
          </label>
          <textarea className="fy-textarea" id={`${id}-note`} value={form.note} onChange={(e) => setForm({ ...form, note: e.target.value })} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={save.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={save.isPending || form.note.trim() === ""}>
            Record the step
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
