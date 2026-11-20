import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { helpText, t } from "../i18n";

type Candidate = components["schemas"]["Candidate"];
type Kit = components["schemas"]["InterviewKit"];
type Interviewer = components["schemas"]["Interviewer"];
type AccessLink = components["schemas"]["CandidateAccessLink"];
type Note = { ok: boolean; text: string } | null;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("pipeline.failed");
const next: Record<string, string[]> = { applied: ["screening", "interview", "not_progressing"], screening: ["interview", "not_progressing"], interview: ["offer", "not_progressing"], offer: ["interview", "not_progressing"], not_progressing: ["screening"] };

// CandidateAccess is a candidate's own link (NM-REC-05): the hiring team makes it, for the days chosen, and sends it themselves; its token is shown once. The links made are listed with when each works until, and one is revoked here.
function CandidateAccess({ candidate, name, onChange }: { candidate: string; name: string; onChange: (error: unknown, ok: string) => boolean }) {
  const [days, setDays] = useState("14");
  const [links, setLinks] = useState<AccessLink[]>([]);
  const [made, setMade] = useState("");
  const load = () => {
    api.GET("/v1/candidates/{id}/access-links", { params: { path: { id: candidate } } }).then(({ data }) => setLinks(data?.links ?? []), () => setLinks([]));
  };
  useEffect(load, [candidate]); // eslint-disable-line react-hooks/exhaustive-deps
  const make = async (e: React.FormEvent) => {
    e.preventDefault();
    const { data, error } = await api.POST("/v1/candidates/{id}/access-links", { params: { path: { id: candidate } }, body: { days: Number(days) } });
    if (!onChange(error, t("pipeline.accessDone"))) return;
    setMade(t("pipeline.accessMade", { path: data?.path ?? "", token: data?.token ?? "" }));
    load();
  };
  const revoke = async (l: AccessLink) => {
    if (onChange((await api.POST("/v1/candidate-access-links/{id}/revocation", { params: { path: { id: l.id } } })).error, t("pipeline.accessRevokedDone"))) load();
  };
  return (
    <details className="fy-stack fy-stack--2">
      <summary title={helpText("pipeline.access").tooltip}>{t("pipeline.access", { name })}</summary>
      <p className="fy-secondary">{t("pipeline.accessLead")}</p>
      <form className="fy-row" style={{ gap: "var(--space-2)" }} aria-label={t("pipeline.access", { name })} onSubmit={make}>
        <label className="fy-field"><span>{t("pipeline.accessDays")}</span><input className="fy-input" type="number" min={1} max={30} required value={days} onChange={(e) => setDays(e.target.value)} /></label>
        <button className="fy-btn fy-btn--secondary fy-btn--sm" type="submit">{t("pipeline.accessButton")}</button>
      </form>
      {made && <p role="status"><code>{made}</code></p>}
      {links.length > 0 && <h5 className="fy-strong">{t("pipeline.accessList", { name })}</h5>}
      <ul className="fy-stack fy-stack--1">
        {links.map((l) => (
          <li key={l.id} className="fy-row" style={{ gap: "var(--space-2)" }}>
            <span>{t("pipeline.accessLine", { made: l.created_at.slice(0, 10), until: l.expires_at.slice(0, 10) })}{l.revoked_at ? ` (${t("pipeline.accessRevoked", { on: l.revoked_at.slice(0, 10) })})` : ""}</span>
            {!l.revoked_at && <button className="fy-btn fy-btn--secondary fy-btn--sm" type="button" aria-label={t("pipeline.accessRevoke", { made: l.created_at.slice(0, 10) })} onClick={() => void revoke(l)}>{t("pipeline.revokeButton")}</button>}
          </li>
        ))}
      </ul>
    </details>
  );
}

// Pipeline is the candidates of one approved requisition (NM-REC-04, NM-REC-05): the board with each candidate's stage and consent, adding a candidate, moving one along the board with the reason of a decision,
// a note, the person's consent to be kept for future roles, and the interview kit with its fair-wording check. A recruiter writes; the API and the database decide who reads what.
export function Pipeline({ requisition, canWrite, canHire = false, canDraft = false }: { requisition: string; canWrite: boolean; canHire?: boolean; canDraft?: boolean }) {
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
  const [interviewers, setInterviewers] = useState<Interviewer[]>([]);
  const [pick, setPick] = useState<Record<string, string>>({});
  const load = () => {
    if (canWrite) api.GET("/v1/interviewers").then(({ data }) => setInterviewers(data?.interviewers ?? []), () => setInterviewers([]));
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
            {c.retention_until && c.stage === "hired" && <span className="fy-secondary">{t("pipeline.hiredDue", { on: c.retention_until })}</span>}
            {canWrite && c.stage !== "hired" && (
              <form className="fy-row" style={{ gap: "var(--space-2)" }} aria-label={t("pipeline.assignLabel", { candidate: c.name })}
                onSubmit={async (e) => { e.preventDefault(); done((await api.POST("/v1/candidates/{id}/interviewers", { params: { path: { id: c.id } }, body: { member_id: pick[c.id] ?? interviewers[0]?.member_id ?? "" } })).error, t("pipeline.assigned")); }}>
                {interviewers.length === 0 ? <span className="fy-secondary">{t("pipeline.noInterviewers")}</span> : (
                  <>
                    <label className="fy-field"><span>{t("pipeline.interviewer")}</span>
                      <select className="fy-input" value={pick[c.id] ?? interviewers[0]?.member_id ?? ""} onChange={(e) => setPick({ ...pick, [c.id]: e.target.value })}>{interviewers.map((i) => <option key={i.member_id} value={i.member_id}>{i.name}</option>)}</select>
                    </label>
                    <button className="fy-btn fy-btn--secondary fy-btn--sm" type="submit" title={helpText("pipeline.assign").tooltip}>{t("pipeline.assignButton")}</button>
                  </>
                )}
              </form>
            )}
            {(c.interviewers ?? []).length > 0 && <span className="fy-secondary">{t("pipeline.interviewers", { names: (c.interviewers ?? []).join(", ") })}</span>}
            {canWrite && <CandidateAccess candidate={c.id} name={c.name} onChange={done} />}
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
        {canDraft && (
          <span className="fy-row" style={{ gap: "var(--space-2)" }} title={helpText("draft.assistant").tooltip}>
            <button className="fy-btn fy-btn--secondary fy-btn--sm" type="button" onClick={async () => done((await api.POST("/v1/assistant/drafts", { body: { task_type: "people.draft_interview_kit", request_id: requisition } })).error, t("draft.asked"))}>{t("draft.askKit")}</button>
            <button className="fy-btn fy-btn--secondary fy-btn--sm" type="button" onClick={async () => { const got = await api.GET("/v1/assistant/drafts", { params: { query: { request_id: requisition } } }); const p = got.data?.drafts?.find((x) => x.kind === "interview_kit")?.payload as { questions?: string; criteria?: string } | undefined; if (p) { setQuestions(p.questions ?? ""); setCriteria(p.criteria ?? ""); setNote({ ok: true, text: t("draft.kitUsed") }); } else setNote({ ok: false, text: t("draft.failed") }); }}>{t("draft.use")}</button>
          </span>
        )}
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
