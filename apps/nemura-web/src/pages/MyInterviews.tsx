import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, team } from "./Shell";

type Interview = components["schemas"]["MyInterview"];
type Candidate = components["schemas"]["Candidate"];
type Note = { ok: boolean; text: string } | null;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("interviews.failed");

// MyInterviews is nm-my-interviews (NM-REC-04): the candidates the hiring team assigned to the person, each with the role and the scorecard's criteria. The person reads the application and scores a criterion from
// 1 to 5 with the comment it rests on; a score is kept as written. They never see the team's notes or another interviewer's scores: the database returns only what is theirs.
export function MyInterviews({ roles }: { roles: readonly string[] }) {
  const [list, setList] = useState<Interview[]>([]);
  const [open, setOpen] = useState<Candidate | null>(null);
  const [note, setNote] = useState<Note>(null);
  const [f, setF] = useState({ criterion: "", score: "3", comment: "" });
  const load = () => {
    api.GET("/v1/me/interviews").then(({ data }) => setList(data?.interviews ?? []), () => setList([]));
  };
  useEffect(load, []); // eslint-disable-line react-hooks/exhaustive-deps
  const read = async (i: Interview) => {
    const { data, error } = await api.GET("/v1/candidates/{id}", { params: { path: { id: i.candidate_id } } });
    if (error) return setNote({ ok: false, text: problem(error) });
    setOpen(data ?? null);
    setF({ criterion: i.criteria[0] ?? "", score: "3", comment: "" });
  };
  const score = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!open) return;
    const { error } = await api.POST("/v1/candidates/{id}/scores", { params: { path: { id: open.id } }, body: { criterion: f.criterion, score: Number(f.score), comment: f.comment || undefined } });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t("interviews.saved") });
    if (!error) {
      setF({ ...f, comment: "" });
      load();
      const { data } = await api.GET("/v1/candidates/{id}", { params: { path: { id: open.id } } });
      setOpen(data ?? open);
    }
  };
  const current = list.find((i) => i.candidate_id === open?.id);
  return (
    <Shell roles={roles} directory={team(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-my-interviews" />
        <h1 className="fy-h1" title={helpText("interviews.page").tooltip}>{t("interviews.title")}</h1>
        <p className="fy-secondary">{t("interviews.lead")}</p>
        {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
        {list.length === 0 && <p className="fy-secondary">{t("interviews.none")}</p>}
        <ul className="fy-stack fy-stack--2">
          {list.map((i) => (
            <li key={i.candidate_id} className="fy-card fy-card--pad fy-stack fy-stack--2">
              <span className="fy-strong">{t("interviews.head", { name: i.name, role: i.requisition_title, stage: t(`pipeline.stage.${i.stage}`) })}</span>
              {i.criteria.length > 0 ? <span className="fy-secondary">{t("interviews.criteria", { list: i.criteria.join(", ") })}</span> : <span className="fy-secondary">{t("interviews.noKit")}</span>}
              {i.scored.length > 0 && <span className="fy-secondary">{t("interviews.scored", { list: i.scored.join(", ") })}</span>}
              <button className="fy-btn fy-btn--secondary fy-btn--sm" type="button" aria-label={t("interviews.open", { name: i.name })} aria-expanded={open?.id === i.candidate_id} onClick={() => void read(i)}>{t("interviews.open", { name: i.name })}</button>
              {open?.id === i.candidate_id && (
                <div className="fy-stack fy-stack--2">
                  <h2 className="fy-h3">{t("interviews.cv")}</h2>
                  <p style={{ whiteSpace: "pre-wrap" }}>{open.cv ? open.cv : t("interviews.noCV")}</p>
                  {(open.scores ?? []).length > 0 && (
                    <>
                      <h2 className="fy-h3">{t("interviews.mine")}</h2>
                      <ul>{(open.scores ?? []).map((s, k) => <li key={k}>{t("interviews.scoreLine", { criterion: s.criterion, score: String(s.score), comment: s.comment ?? "" })}</li>)}</ul>
                    </>
                  )}
                  {current && current.criteria.length > 0 && (
                    <form className="fy-stack fy-stack--2" aria-label={t("interviews.save")} onSubmit={score}>
                      <label className="fy-field"><span>{t("interviews.criterion")}</span>
                        <select className="fy-input" value={f.criterion} onChange={(e) => setF({ ...f, criterion: e.target.value })}>{current.criteria.map((c) => <option key={c} value={c}>{c}</option>)}</select>
                      </label>
                      <label className="fy-field"><span>{t("interviews.score")}</span>
                        <select className="fy-input" value={f.score} onChange={(e) => setF({ ...f, score: e.target.value })}>{["1", "2", "3", "4", "5"].map((n) => <option key={n} value={n}>{n}</option>)}</select>
                      </label>
                      <label className="fy-field"><span>{t("interviews.comment")}</span><input className="fy-input" maxLength={1000} value={f.comment} onChange={(e) => setF({ ...f, comment: e.target.value })} /></label>
                      <button className="fy-btn fy-btn--primary" type="submit">{t("interviews.save")}</button>
                    </form>
                  )}
                </div>
              )}
            </li>
          ))}
        </ul>
      </main>
    </Shell>
  );
}
