import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, ClockAlert, Plus, ShieldAlert } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { useSignedIn } from "../../session";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Register = components["schemas"]["SecurityAssessmentRegister"];
type Assessment = components["schemas"]["SecurityAssessmentView"];
type Finding = components["schemas"]["SecurityFindingView"];
type Severity = Finding["severity"];

async function register() {
  const { data, error } = await api.GET("/v1/compliance/security-assessments");
  if (error) throw error;
  return data;
}

const kinds: Record<Assessment["kind"], string> = { penetration_test: "Penetration test", vulnerability_assessment: "Vulnerability assessment", other: "Other assessment" };
const severities: Severity[] = ["critical", "high", "medium", "low", "info"];
const stateWords: Record<Finding["state"], string> = { open: "Open", fixed: "Fixed, to retest", retested: "Retested", accepted: "Accepted" };

// SecurityAssessmentsTab is the register of security assessments of Compliance › Controls (SEC-25, CTL-CHG-12; rev 0.617): each penetration test
// or other assessment with its tester, scope, dates, the report's SHA-256 and where it is kept, and each finding with its due day, fix and retest.
// The Owner keeps it; the Administrator, Controller, Auditor and DPO read it.
export function SecurityAssessmentsTab() {
  const id = useId();
  const { session } = useSignedIn();
  const owner = (session.roles ?? []).includes("owner");
  const reg = useQuery({ queryKey: ["security-assessments"], queryFn: register });
  const [recording, setRecording] = useState(false);
  const [adding, setAdding] = useState<Assessment | null>(null);
  const [stepping, setStepping] = useState<Finding | null>(null);
  if (reg.isError) return <Refusal problem={problemOf(reg.error)} title="The security assessments could not be shown" />;
  if (!reg.data) return <p className="fy-small">Loading the security assessments…</p>;
  const r: Register = reg.data;
  return (
    <>
      <section className="fy-card" aria-labelledby={`${id}-assessments`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-assessments`}>
            Security assessments
          </h2>
          {r.overdue_findings > 0 && (
            <span className="fy-chip fy-chip--serious">
              <Icon of={ShieldAlert} />
              {r.overdue_findings} overdue {r.overdue_findings === 1 ? "finding" : "findings"}
            </span>
          )}
          {owner && (
            <Button size="sm" onClick={() => setRecording(true)}>
              <Icon of={Plus} />
              Record an assessment
            </Button>
          )}
        </div>
        <p className="fy-card__body fy-small fy-secondary">
          {r.last_independent_on
            ? `The last independent test ended on ${date(r.last_independent_on)}; the next is due by ${date(r.next_due_on ?? "")}.`
            : "No independent penetration test is recorded yet. One is needed every year, and before the first external customer is onboarded."}{" "}
          A Critical finding is due in 7 days, a High one in 30, a Medium one in 90 and a Low one in 180, from the day it was found. The report stays outside Fyniq: record its SHA-256 and where it is kept.
        </p>
        {r.items.length === 0 ? (
          <p className="fy-card__body fy-small fy-secondary">Nothing recorded. Record each penetration test and other assessment, with what it found.</p>
        ) : (
          <ul className="fy-list" aria-labelledby={`${id}-assessments`}>
            {r.items.map((a) => (
              <li key={a.assessment_id} className="fy-list__item fy-list__item--wrap">
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">
                    {kinds[a.kind]} by {a.tester}
                    {a.independent ? ", independent" : ", not independent"}
                  </span>
                  <span className="fy-list__meta whitespace-normal">
                    {date(a.started_on)} to {date(a.ended_on)} · {a.scope} · {a.summary}
                    {a.report_sha256 && ` · Report SHA-256 ${a.report_sha256.slice(0, 12)}…${a.report_location ? `, kept: ${a.report_location}` : ""}`}
                  </span>
                  <ul className="fy-list" aria-label={`Findings of the ${kinds[a.kind].toLowerCase()} ending ${date(a.ended_on)}`}>
                    {a.findings.length === 0 && <li className="fy-small fy-secondary">No finding.</li>}
                    {a.findings.map((f) => (
                      <li key={f.finding_id} className="fy-list__item fy-list__item--wrap">
                        <span className="fy-list__main">
                          <span className="fy-list__title whitespace-normal">
                            {f.ref}: {f.title}
                          </span>
                          <span className="fy-list__meta whitespace-normal">
                            {f.severity} · found {date(f.found_on)}
                            {f.due_on && ` · due ${date(f.due_on)}`}
                            {f.steps.map((s) => ` · ${s.step} ${date(s.on)}${s.reason ? ` (${s.reason})` : ""}`).join("")}
                          </span>
                        </span>
                        {f.overdue ? (
                          <span className="fy-chip fy-chip--serious">
                            <Icon of={ClockAlert} />
                            Overdue
                          </span>
                        ) : (
                          <span className={f.state === "retested" || f.state === "accepted" ? "fy-chip fy-chip--good" : "fy-chip"}>
                            {(f.state === "retested" || f.state === "accepted") && <Icon of={CircleCheck} />}
                            {stateWords[f.state]}
                          </span>
                        )}
                        {owner && f.state !== "retested" && f.state !== "accepted" && (
                          <Button size="sm" variant="secondary" onClick={() => setStepping(f)} aria-label={`Record a step of ${f.ref}`}>
                            Record a step
                          </Button>
                        )}
                      </li>
                    ))}
                  </ul>
                </span>
                {owner && (
                  <Button size="sm" variant="secondary" onClick={() => setAdding(a)} aria-label={`Add a finding to the assessment ending ${date(a.ended_on)}`}>
                    <Icon of={Plus} />
                    Add a finding
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>
      {recording && <RecordAssessment onClose={() => setRecording(false)} />}
      {adding && <AddFinding assessment={adding} onClose={() => setAdding(null)} />}
      {stepping && <StepFinding finding={stepping} onClose={() => setStepping(null)} />}
    </>
  );
}

type FindingDraft = { ref: string; title: string; severity: Severity };

function RecordAssessment({ onClose }: { onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const [form, setForm] = useState({ kind: "penetration_test" as Assessment["kind"], tester: "", independent: true, scope: "", started_on: "", ended_on: "", summary: "", report_sha256: "", report_location: "" });
  const [findings, setFindings] = useState<FindingDraft[]>([]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const save = useMutation({
    mutationFn: async () => {
      const body: components["schemas"]["SecurityAssessmentInput"] = {
        ...form,
        tester: form.tester.trim(),
        scope: form.scope.trim(),
        summary: form.summary.trim(),
        report_sha256: form.report_sha256.trim() || undefined,
        report_location: form.report_location.trim() || undefined,
        findings: findings.filter((f) => f.ref.trim() || f.title.trim()).map((f) => ({ ref: f.ref.trim(), title: f.title.trim(), severity: f.severity })),
      };
      const out = await api.POST("/v1/compliance/security-assessments", { params: { header: { "Idempotency-Key": newKey() } }, body });
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await Promise.all(["security-assessments", "compliance-reviews"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  const text = (key: "tester" | "scope" | "summary" | "report_sha256" | "report_location", label: string) => (
    <div className="fy-field">
      <label className="fy-label" htmlFor={`${id}-${key}`}>
        {label}
      </label>
      <input className="fy-input" id={`${id}-${key}`} value={form[key]} onChange={(e) => setForm({ ...form, [key]: e.target.value })} />
    </div>
  );
  const day = (key: "started_on" | "ended_on", label: string) => (
    <div className="fy-field">
      <label className="fy-label" htmlFor={`${id}-${key}`}>
        {label}
      </label>
      <input className="fy-input" type="date" id={`${id}-${key}`} value={form[key]} onChange={(e) => setForm({ ...form, [key]: e.target.value })} />
    </div>
  );
  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()} dismissible={!save.isPending} eyebrow="Security assessments" title="Record an assessment"
      description="Record who tested, what was covered and when, what it found, and the report's SHA-256 and where it is kept. It is kept as it is: a correction is a new entry.">
      {problem && <Refusal problem={problem} title="The assessment was not saved" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-kind`}>
            What it was
          </label>
          <select className="fy-select" id={`${id}-kind`} value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value as Assessment["kind"] })}>
            {(Object.keys(kinds) as Assessment["kind"][]).map((k) => (
              <option key={k} value={k}>
                {kinds[k]}
              </option>
            ))}
          </select>
        </div>
        {text("tester", "Who tested")}
        <label className="fy-row gap-2 fy-small">
          <input type="checkbox" checked={form.independent} onChange={(e) => setForm({ ...form, independent: e.target.checked })} />
          They are independent: not the people who build and run Fyniq
        </label>
        {text("scope", "What it covered")}
        {day("started_on", "First day")}
        {day("ended_on", "Last day")}
        {text("summary", "What it concluded")}
        {text("report_sha256", "The report's SHA-256 (64 characters, optional)")}
        {text("report_location", "Where the report is kept (optional)")}
        <fieldset className="fy-stack fy-stack--2">
          <legend className="fy-label">Findings</legend>
          {findings.map((f, i) => (
            <div key={i} className="fy-row gap-2">
              <input className="fy-input" aria-label={`Finding ${i + 1} reference`} placeholder="Reference" value={f.ref} onChange={(e) => setFindings(findings.map((x, j) => (j === i ? { ...x, ref: e.target.value } : x)))} />
              <input className="fy-input" aria-label={`Finding ${i + 1} title`} placeholder="What it is" value={f.title} onChange={(e) => setFindings(findings.map((x, j) => (j === i ? { ...x, title: e.target.value } : x)))} />
              <select className="fy-select" aria-label={`Finding ${i + 1} severity`} value={f.severity} onChange={(e) => setFindings(findings.map((x, j) => (j === i ? { ...x, severity: e.target.value as Severity } : x)))}>
                {severities.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
            </div>
          ))}
          <Button type="button" variant="ghost" size="sm" onClick={() => setFindings([...findings, { ref: "", title: "", severity: "medium" }])}>
            <Icon of={Plus} />
            Add a finding
          </Button>
        </fieldset>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={save.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={save.isPending}>
            Record it
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function AddFinding({ assessment, onClose }: { assessment: Assessment; onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const [form, setForm] = useState<FindingDraft & { found_on: string }>({ ref: "", title: "", severity: "medium", found_on: "" });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const save = useMutation({
    mutationFn: async () => {
      const out = await api.POST("/v1/compliance/security-assessments/{assessment_id}/findings", {
        params: { path: { assessment_id: assessment.assessment_id }, header: { "Idempotency-Key": newKey() } },
        body: { ref: form.ref.trim(), title: form.title.trim(), severity: form.severity, ...(form.found_on ? { found_on: form.found_on } : {}) },
      });
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await Promise.all(["security-assessments", "compliance-reviews"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()} dismissible={!save.isPending} eyebrow="Security assessments" title="Add a finding"
      description="A finding reported later, such as at a retest. It is due by its severity, from the day it was found.">
      {problem && <Refusal problem={problem} title="The finding was not saved" />}
      <form className="fy-stack fy-stack--3" onSubmit={(e) => { e.preventDefault(); save.mutate(); }} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-ref`}>
            Reference
          </label>
          <input className="fy-input" id={`${id}-ref`} value={form.ref} onChange={(e) => setForm({ ...form, ref: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-title`}>
            What it is
          </label>
          <input className="fy-input" id={`${id}-title`} value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-severity`}>
            Severity
          </label>
          <select className="fy-select" id={`${id}-severity`} value={form.severity} onChange={(e) => setForm({ ...form, severity: e.target.value as Severity })}>
            {severities.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-found`}>
            Day found (the assessment's last day if left empty)
          </label>
          <input className="fy-input" type="date" id={`${id}-found`} value={form.found_on} onChange={(e) => setForm({ ...form, found_on: e.target.value })} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={save.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={save.isPending}>
            Add the finding
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function StepFinding({ finding, onClose }: { finding: Finding; onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const next: "fixed" | "retested" = finding.state === "fixed" ? "retested" : "fixed";
  const [step, setStep] = useState<"fixed" | "retested" | "accepted">(next);
  const [on, setOn] = useState("");
  const [reason, setReason] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const save = useMutation({
    mutationFn: async () => {
      const out = await api.POST("/v1/compliance/security-findings/{finding_id}/steps", {
        params: { path: { finding_id: finding.finding_id }, header: { "Idempotency-Key": newKey() } },
        body: { step, on, ...(step === "accepted" ? { reason: reason.trim() } : {}) },
      });
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await Promise.all(["security-assessments", "compliance-reviews"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()} dismissible={!save.isPending} eyebrow={finding.ref} title="Record a step"
      description={`${finding.title}. A finding is fixed, then retested; or accepted, with a reason, before it is retested.`}>
      {problem && <Refusal problem={problem} title="The step was not saved" />}
      <form className="fy-stack fy-stack--3" onSubmit={(e) => { e.preventDefault(); save.mutate(); }} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-step`}>
            Step
          </label>
          <select className="fy-select" id={`${id}-step`} value={step} onChange={(e) => setStep(e.target.value as typeof step)}>
            <option value={next}>{next === "fixed" ? "Fixed" : "Retested"}</option>
            {finding.state === "open" || finding.state === "fixed" ? <option value="accepted">Accepted by the Owner</option> : null}
          </select>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-on`}>
            Day it happened
          </label>
          <input className="fy-input" type="date" id={`${id}-on`} value={on} onChange={(e) => setOn(e.target.value)} />
        </div>
        {step === "accepted" && (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-reason`}>
              Why the risk is taken
            </label>
            <textarea className="fy-input" id={`${id}-reason`} rows={3} maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} />
          </div>
        )}
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={save.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={save.isPending}>
            Record it
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
