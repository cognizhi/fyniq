import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { t } from "../i18n";

type Candidate = components["schemas"]["Candidate"];
type Kit = components["schemas"]["InterviewKit"];
type Note = { ok: boolean; text: string } | null;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("pipeline.failed");
const next: Record<string, string[]> = { applied: ["screening", "interview", "not_progressing"], screening: ["interview", "not_progressing"], interview: ["offer", "not_progressing"], offer: ["interview", "not_progressing"], not_progressing: ["screening"] };

// Pipeline is the candidates of one approved requisition (NM-REC-04, NM-REC-05): the board with each candidate's stage and consent, adding a candidate, moving one along the board with the reason of a decision,
// a note, the person's consent to be kept for future roles, and the interview kit with its fair-wording check. A recruiter writes; the API and the database decide who reads what.
export function Pipeline({ requisition, canWrite, canHire = false }: { requisition: string; canWrite: boolean; canHire?: boolean }) {
  const [list, setList] = useState<Candidate[]>([]);
  const [kit, setKit] = useState<Kit | null>(null);
  const [note, setNote] = useState<Note>(null);
  const [f, setF] = useState({ name: "", email: "", cv: "", keep: false });
  const [questions, setQuestions] = useState("");
  const [criteria, setCriteria] = useState("");
  const [requirement, setRequirement] = useState("");
  const [reason, setReason] = useState("");
  const [text, setText] = useState("");
  const [start, setStart] = useState("");
  const [employment, setEmployment] = useState<"full_time" | "part_time" | "contract" | "intern">("full_time");
  const [passOn, setPassOn] = useState("");
  const load = () => {
    api.GET("/v1/requisitions/{id}/candidates", { params: { path: { id: requisition } } }).then(({ data }) => setList(data?.candidates ?? []), () => setList([]));
    api.GET("/v1/requisitions/{id}/interview-kit", { params: { path: { id: requisition } } }).then(({ data }) => {
      setKit(data ?? null);
      if (data?.exists) (setQuestions(data.questions.join("\n")), setCriteria(data.criteria.join("\n")), setRequirement(data.requirement_note ?? ""));
    }, () => setKit(null));
  };
  useEffect(load, [requisition]); // eslint-disable-line react-hooks/exhaustive-deps
  const done = (error: unknown, ok: string) => {
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: ok });
    if (!error) load();
    return !error;
  };
  const lines = (s: string) => s.split("\n").map((x) => x.trim()).filter(Boolean);
  const id = { params: { path: { id: requisition } } };
  return (
    <section className="fy-stack fy-stack--2" aria-label={t("pipeline.title")}>
      <h3 className="fy-h3">{t("pipeline.title")}</h3>
      {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
      {list.length === 0 && <p className="fy-secondary">{t("pipeline.none")}</p>}
      <ul className="fy-stack fy-stack--2">
        {list.map((c) => (
          <li key={c.id} className="fy-card fy-card--pad fy-stack fy-stack--2">
            <span className="fy-strong">{t("pipeline.head", { name: c.name, stage: t(`pipeline.stage.${c.stage}`) })}</span>
            <span className="fy-secondary">{t("pipeline.line", { email: c.email, via: t(`pipeline.via.${c.applied_via}`) })}</span>
            {c.consent_future && <span className="fy-chip">{t("pipeline.keep")}</span>}
            {c.retention_until && <span className="fy-secondary">{t("pipeline.due", { on: c.retention_until })}</span>}
            {c.stage_reason && <span className="fy-secondary">{t("pipeline.reason", { reason: c.stage_reason })}</span>}
            {canWrite && (next[c.stage] ?? []).length > 0 && (
              <span className="fy-row" style={{ gap: "var(--space-2)" }}>
                {(next[c.stage] ?? []).map((to) => (
                  <button key={to} className="fy-btn fy-btn--secondary fy-btn--sm" type="button" aria-label={t("pipeline.move", { name: c.name, to: t(`pipeline.stage.${to}`) })}
                    onClick={async () => done((await api.POST("/v1/candidates/{id}/moves", { params: { path: { id: c.id } }, body: { to: to as "screening", reason: to === "not_progressing" ? reason : undefined } })).error, t("pipeline.moved"))}>{t(`pipeline.stage.${to}`)}</button>
                ))}
              </span>
            )}
            {c.hire && <span className="fy-secondary">{t("pipeline.hired", { start: c.hire.start_date })}</span>}
            {c.hire?.pass_needed && !c.hire.pass_application_on && canWrite && (
              <form className="fy-row" style={{ gap: "var(--space-2)" }} aria-label={t("pipeline.pass", { name: c.name })} onSubmit={async (e) => { e.preventDefault(); done((await api.POST("/v1/candidates/{id}/pass-application", { params: { path: { id: c.id } }, body: { on: passOn } })).error, t("pipeline.passSaved")); }}>
                <label className="fy-field"><span>{t("pipeline.passDay")}</span><input className="fy-input" type="date" required value={passOn} onChange={(e) => setPassOn(e.target.value)} /></label>
                <button className="fy-btn fy-btn--secondary fy-btn--sm" type="submit">{t("pipeline.passRecord")}</button>
              </form>
            )}
            {c.hire?.pass_application_on && <span className="fy-secondary">{t("pipeline.passOn", { on: c.hire.pass_application_on })}</span>}
            {canHire && c.stage === "offer" && (
              <form className="fy-row" style={{ gap: "var(--space-2)" }} aria-label={t("pipeline.hire", { name: c.name })} onSubmit={async (e) => { e.preventDefault(); done((await api.POST("/v1/candidates/{id}/hire", { params: { path: { id: c.id } }, body: { start_date: start, employment_type: employment } })).error, t("pipeline.hireSaved")); }}>
                <label className="fy-field"><span>{t("pipeline.startDay")}</span><input className="fy-input" type="date" required value={start} onChange={(e) => setStart(e.target.value)} /></label>
                <label className="fy-field"><span>{t("pipeline.employment")}</span><select className="fy-input" value={employment} onChange={(e) => setEmployment(e.target.value as typeof employment)}>{(["full_time", "part_time", "contract", "intern"] as const).map((k) => <option key={k} value={k}>{t(`pipeline.type.${k}`)}</option>)}</select></label>
                <button className="fy-btn fy-btn--primary fy-btn--sm" type="submit">{t("pipeline.hireButton", { name: c.name })}</button>
              </form>
            )}
            {canWrite && (
              <span className="fy-row" style={{ gap: "var(--space-2)" }}>
                <button className="fy-btn fy-btn--secondary fy-btn--sm" type="button" aria-label={t(c.consent_future ? "pipeline.withdraw" : "pipeline.consent", { name: c.name })}
                  disabled={!c.consent_future && !!c.consent_future_at}
                  onClick={async () => done((await api.POST("/v1/candidates/{id}/future-consent", { params: { path: { id: c.id } }, body: { given: !c.consent_future } })).error, t("pipeline.consentSaved"))}>{t(c.consent_future ? "pipeline.withdrawShort" : "pipeline.consentShort")}</button>
              </span>
            )}
          </li>
        ))}
      </ul>
      {canWrite && (
        <>
          <label className="fy-field"><span>{t("pipeline.decisionReason")}</span><input className="fy-input" maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} /></label>
          <label className="fy-field"><span>{t("pipeline.noteLabel")}</span><input className="fy-input" maxLength={4000} value={text} onChange={(e) => setText(e.target.value)} /></label>
          <p className="fy-secondary">{t("pipeline.noteLead")}</p>
          {list[0] && <button className="fy-btn fy-btn--secondary fy-btn--sm" type="button" onClick={async () => { if (done((await api.POST("/v1/candidates/{id}/notes", { params: { path: { id: list[0]!.id } }, body: { body: text } })).error, t("pipeline.noted"))) setText(""); }}>{t("pipeline.note", { name: list[0].name })}</button>}
          <form className="fy-stack fy-stack--2" aria-label={t("pipeline.add")} onSubmit={async (e) => { e.preventDefault(); if (done((await api.POST("/v1/requisitions/{id}/candidates", { ...id, body: { name: f.name, email: f.email, cv: f.cv || undefined, consent_future: f.keep } })).error, t("pipeline.added"))) setF({ name: "", email: "", cv: "", keep: false }); }}>
            <h4 className="fy-h3">{t("pipeline.add")}</h4>
            <label className="fy-field"><span>{t("pipeline.name")}</span><input className="fy-input" required maxLength={120} value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} /></label>
            <label className="fy-field"><span>{t("pipeline.email")}</span><input className="fy-input" type="email" required maxLength={200} value={f.email} onChange={(e) => setF({ ...f, email: e.target.value })} /></label>
            <label className="fy-field"><span>{t("pipeline.cv")}</span><textarea className="fy-input" maxLength={20000} value={f.cv} onChange={(e) => setF({ ...f, cv: e.target.value })} /></label>
            <label className="fy-field fy-row" style={{ gap: "var(--space-2)" }}><input type="checkbox" checked={f.keep} onChange={(e) => setF({ ...f, keep: e.target.checked })} /><span>{t("pipeline.keepFuture")}</span></label>
            <p className="fy-secondary">{t("pipeline.consentLead")}</p>
            <button className="fy-btn fy-btn--secondary" type="submit">{t("pipeline.addButton")}</button>
          </form>
        </>
      )}
      <form className="fy-stack fy-stack--2" aria-label={t("pipeline.kit")} onSubmit={async (e) => { e.preventDefault(); done((await api.PUT("/v1/requisitions/{id}/interview-kit", { ...id, body: { questions: lines(questions), criteria: lines(criteria), requirement_note: requirement || undefined } })).error, t("pipeline.kitSaved")); }}>
        <h4 className="fy-h3">{t("pipeline.kit")}</h4>
        <p className="fy-secondary">{t("pipeline.kitLead")}</p>
        <label className="fy-field"><span>{t("pipeline.questions")}</span><textarea className="fy-input" readOnly={!canWrite} value={questions} onChange={(e) => setQuestions(e.target.value)} /></label>
        <label className="fy-field"><span>{t("pipeline.criteria")}</span><textarea className="fy-input" readOnly={!canWrite} value={criteria} onChange={(e) => setCriteria(e.target.value)} /></label>
        {kit && kit.findings.length > 0 && (
          <div role="status" className="fy-banner fy-banner--warning">
            <span className="fy-strong">{t("hiring.flagged")}</span>
            <ul>{kit.findings.map((x) => <li key={x.category}>{t("hiring.finding", { category: t(`hiring.category.${x.category}`), phrase: x.phrase })}</li>)}</ul>
          </div>
        )}
        {canWrite && <label className="fy-field"><span>{t("pipeline.kitRequirement")}</span><input className="fy-input" maxLength={1000} value={requirement} onChange={(e) => setRequirement(e.target.value)} /></label>}
        {canWrite && <button className="fy-btn fy-btn--secondary" type="submit">{t("pipeline.saveKit")}</button>}
      </form>
    </section>
  );
}
