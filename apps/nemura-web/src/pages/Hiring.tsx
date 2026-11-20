import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Requisition = components["schemas"]["Requisition"];
type Finding = components["schemas"]["WordingFinding"];
type Entry = components["schemas"]["DirectoryEntry"];
type Note = { ok: boolean; text: string } | null;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("hiring.failed");
const exemptions = ["fewer_than_10_employees", "ep_salary_22500", "role_one_month_or_less", "transferee", "s_pass_exempt"] as const;
type Change = React.ChangeEvent<HTMLInputElement | HTMLSelectElement>;
const blank = { title: "", department: "", reason: "new" as "new" | "replacement", replaces: "", min: "", max: "", start: "", pass: false };

// Hiring is nm-hiring, first slice (NM-REC-01 to NM-REC-03): requisitions the Owner approves, a job post for each with the fair-wording check, and the Fair Consideration Framework's advert dates. The Recruiter, the HR administrator and the Owner ask
// and write (rev 0.69); the HR officer reads; the Owner approves, even a requisition they asked for. The candidate pipeline, interview kits and offers come later. The candidate pipeline, interview kits and offers come later.
export function Hiring({ roles }: { roles: readonly string[] }) {
  const [list, setList] = useState<Requisition[]>([]);
  const [people, setPeople] = useState<Entry[]>([]);
  const [off, setOff] = useState(false);
  const [note, setNote] = useState<Note>(null);
  const [f, setF] = useState(blank);
  const [open, setOpen] = useState("");
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [requirement, setRequirement] = useState("");
  const [advert, setAdvert] = useState("");
  const [kind, setKind] = useState<(typeof exemptions)[number]>("fewer_than_10_employees");
  const [exNote, setExNote] = useState("");
  const [found, setFound] = useState<Finding[]>([]);
  const owner = roles.includes("owner");
  const recruiter = roles.includes("recruiter") || owner || roles.includes("hr_admin"); // who may ask and write: Appendix B (rev 0.69) gives it to the Recruiter, the HR administrator and the Owner
  const allowed = recruiter || owner || roles.some((r) => r === "hr_officer" || r === "hr_admin");
  const load = () => {
    if (!allowed) return;
    api.GET("/v1/requisitions").then(({ data, error }) => (setOff(!data && !!error), setList(data?.requisitions ?? [])), () => setList([]));
    if (recruiter) api.GET("/v1/people").then(({ data }) => setPeople(data?.people ?? []), () => setPeople([]));
  };
  useEffect(load, []); // eslint-disable-line react-hooks/exhaustive-deps
  const done = (error: unknown, ok: string) => {
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: ok });
    if (!error) load();
    return !error;
  };
  const set = (k: keyof typeof blank) => (e: Change) => setF({ ...f, [k]: e.target.type === "checkbox" ? (e.target as HTMLInputElement).checked : e.target.value });
  const ask = async (e: React.FormEvent) => {
    e.preventDefault();
    const body = { title: f.title, department: f.department, reason: f.reason, replaces_person_id: f.reason === "replacement" ? f.replaces || people[0]?.id : undefined, salary_min: f.min, salary_max: f.max, target_start: f.start, pass_needed: f.pass };
    if (done((await api.POST("/v1/requisitions", { body })).error, t("hiring.asked"))) setF(blank);
  };
  const check = async (text: string) => setFound((await api.POST("/v1/fair-wording-check", { body: { text } })).data?.findings ?? []);
  const post = (r: Requisition) => r.job_post;
  const pick = (r: Requisition) => {
    setOpen(open === r.id ? "" : r.id);
    setTitle(post(r)?.title ?? r.title);
    setBody(post(r)?.body ?? "");
    setRequirement("");
    setFound(post(r)?.findings ?? []);
  };
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-hiring" />
        <h1 className="fy-h1" title={helpText("hiring.page").tooltip}>{t("hiring.title")}</h1>
        {!allowed && <p role="alert" className="fy-secondary">{t("hiring.denied")}</p>}
        {allowed && (
          <>
            <p className="fy-secondary">{t("hiring.lead")}</p>
            {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
            <h2 className="fy-h2">{t("hiring.requisitions")}</h2>
            {off && <p className="fy-secondary">{t("hiring.off")}</p>}
            {!off && list.length === 0 && <p className="fy-secondary">{t("hiring.none")}</p>}
            <ul className="fy-stack fy-stack--3">
              {list.map((r) => (
                <li key={r.id} className="fy-card fy-card--pad fy-stack fy-stack--2">
                  <span className="fy-strong">{t("hiring.head", { title: r.title, team: r.department, state: t(`hiring.state.${r.state}`) })}</span>
                  <span className="fy-secondary">{t("hiring.line", { min: r.salary_min, max: r.salary_max, start: r.target_start, plan: t(`hiring.plan.${r.plan_status}`) })}</span>
                  {r.replaces && <span className="fy-secondary">{t("hiring.replaces", { name: r.replaces })}</span>}
                  {r.pass_needed && <span className="fy-secondary">{t("hiring.pass")}</span>}
                  {r.decision_reason && <span className="fy-secondary">{t("hiring.decision", { by: r.decided_by ?? "", reason: r.decision_reason })}</span>}
                  {owner && r.state === "requested" && (
                    <span className="fy-row" style={{ gap: "var(--space-2)" }}>
                      <button className="fy-btn fy-btn--primary fy-btn--sm" type="button" onClick={async () => done((await api.POST("/v1/requisitions/{id}/decision", { params: { path: { id: r.id } }, body: { approve: true } })).error, t("hiring.approved"))}>{t("hiring.approve", { title: r.title })}</button>
                      <button className="fy-btn fy-btn--secondary fy-btn--sm" type="button" onClick={async () => done((await api.POST("/v1/requisitions/{id}/decision", { params: { path: { id: r.id } }, body: { approve: false } })).error, t("hiring.rejected"))}>{t("hiring.reject", { title: r.title })}</button>
                    </span>
                  )}
                  {post(r) && post(r)!.state !== "draft" && (
                    <span className="fy-secondary">{t("hiring.postState", { state: t(`hiring.post.${post(r)!.state}`), on: post(r)!.published_on ?? "" })}</span>
                  )}
                  {post(r)?.advert_posted_on && <span className="fy-secondary">{t("hiring.advertOn", { on: post(r)!.advert_posted_on ?? "", from: post(r)!.offer_from ?? "" })}</span>}
                  {post(r)?.exemption && <span className="fy-secondary">{t("hiring.exemptedBy", { kind: t(`hiring.exemption.${post(r)!.exemption}`), note: post(r)!.exemption_note ?? "" })}</span>}
                  {r.pass_needed && post(r)?.offer_blocked && <span className="fy-chip">{t("hiring.offerBlocked")}</span>}
                  {recruiter && (r.state === "requested" || r.state === "approved") && <button className="fy-btn fy-btn--secondary fy-btn--sm" type="button" aria-expanded={open === r.id} onClick={() => pick(r)}>{t("hiring.workOn", { title: r.title })}</button>}
                  {open === r.id && recruiter && (
                    <div className="fy-stack fy-stack--2">
                      {(!post(r) || post(r)!.state === "draft") && (
                        <form className="fy-stack fy-stack--2" aria-label={t("hiring.postForm")} onSubmit={async (e) => { e.preventDefault(); done((await api.PUT("/v1/requisitions/{id}/job-post", { params: { path: { id: r.id } }, body: { title, body } })).error, t("hiring.postSaved")); await check(title + "\n" + body); }}>
                          <h3 className="fy-h3">{t("hiring.postForm")}</h3>
                          <label className="fy-field"><span>{t("hiring.postTitle")}</span><input className="fy-input" required maxLength={120} value={title} onChange={(e) => setTitle(e.target.value)} /></label>
                          <label className="fy-field"><span>{t("hiring.postBody")}</span><textarea className="fy-input" required maxLength={8000} value={body} onChange={(e) => setBody(e.target.value)} /></label>
                          <button className="fy-btn fy-btn--secondary" type="submit">{t("hiring.saveDraft")}</button>
                        </form>
                      )}
                      {found.length > 0 && (
                        <div role="status" className="fy-banner fy-banner--warning">
                          <span className="fy-strong">{t("hiring.flagged")}</span>
                          <ul>{found.map((x) => <li key={x.category}>{t("hiring.finding", { category: t(`hiring.category.${x.category}`), phrase: x.phrase })}</li>)}</ul>
                          <span className="fy-small">{t("hiring.flaggedLead")}</span>
                        </div>
                      )}
                      {post(r)?.state === "draft" && r.state === "approved" && (
                        <div className="fy-stack fy-stack--2">
                          {found.length > 0 && <label className="fy-field"><span>{t("hiring.requirement")}</span><input className="fy-input" maxLength={1000} value={requirement} onChange={(e) => setRequirement(e.target.value)} /></label>}
                          <button className="fy-btn fy-btn--primary" type="button" onClick={async () => done((await api.POST("/v1/requisitions/{id}/job-post/publication", { params: { path: { id: r.id } }, body: { requirement_note: requirement || undefined } })).error, t("hiring.published"))}>{t("hiring.publish")}</button>
                        </div>
                      )}
                      {post(r) && post(r)!.state !== "closed" && r.pass_needed && !post(r)!.advert_posted_on && (
                        <form className="fy-stack fy-stack--2" aria-label={t("hiring.advertForm")} onSubmit={async (e) => { e.preventDefault(); done((await api.POST("/v1/requisitions/{id}/job-post/advert", { params: { path: { id: r.id } }, body: { posted_on: advert } })).error, t("hiring.advertSaved")); }}>
                          <h3 className="fy-h3">{t("hiring.advertForm")}</h3>
                          <p className="fy-secondary">{t("hiring.advertLead")}</p>
                          <label className="fy-field"><span>{t("hiring.advertDay")}</span><input className="fy-input" type="date" required value={advert} onChange={(e) => setAdvert(e.target.value)} /></label>
                          <button className="fy-btn fy-btn--secondary" type="submit">{t("hiring.recordAdvert")}</button>
                        </form>
                      )}
                      {post(r) && post(r)!.state !== "closed" && r.pass_needed && !post(r)!.exemption && (
                        <form className="fy-stack fy-stack--2" aria-label={t("hiring.exemptionForm")} onSubmit={async (e) => { e.preventDefault(); done((await api.POST("/v1/requisitions/{id}/job-post/exemption", { params: { path: { id: r.id } }, body: { kind, note: exNote } })).error, t("hiring.exemptionSaved")); }}>
                          <h3 className="fy-h3">{t("hiring.exemptionForm")}</h3>
                          <label className="fy-field"><span>{t("hiring.exemptionKind")}</span><select className="fy-input" value={kind} onChange={(e) => setKind(e.target.value as (typeof exemptions)[number])}>{exemptions.map((k) => <option key={k} value={k}>{t(`hiring.exemption.${k}`)}</option>)}</select></label>
                          <label className="fy-field"><span>{t("hiring.exemptionEvidence")}</span><input className="fy-input" required maxLength={500} value={exNote} onChange={(e) => setExNote(e.target.value)} /></label>
                          <button className="fy-btn fy-btn--secondary" type="submit">{t("hiring.recordExemption")}</button>
                        </form>
                      )}
                      {post(r)?.state === "published" && <button className="fy-btn fy-btn--secondary" type="button" onClick={async () => done((await api.POST("/v1/requisitions/{id}/job-post/closing", { params: { path: { id: r.id } } })).error, t("hiring.closed"))}>{t("hiring.close")}</button>}
                    </div>
                  )}
                </li>
              ))}
            </ul>
            {recruiter && !off && (
              <form className="fy-stack fy-stack--2" onSubmit={ask} aria-label={t("hiring.ask")}>
                <h2 className="fy-h2">{t("hiring.ask")}</h2>
                <label className="fy-field"><span>{t("hiring.role")}</span><input className="fy-input" required maxLength={120} value={f.title} onChange={set("title")} /></label>
                <label className="fy-field"><span>{t("hiring.team")}</span><input className="fy-input" required maxLength={120} value={f.department} onChange={set("department")} /></label>
                <label className="fy-field"><span>{t("hiring.why")}</span>
                  <select className="fy-input" value={f.reason} onChange={set("reason")}><option value="new">{t("hiring.reason.new")}</option><option value="replacement">{t("hiring.reason.replacement")}</option></select>
                </label>
                {f.reason === "replacement" && (
                  <label className="fy-field"><span>{t("hiring.replaced")}</span>
                    <select className="fy-input" value={f.replaces || people[0]?.id || ""} onChange={set("replaces")}>{people.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</select>
                  </label>
                )}
                <label className="fy-field"><span>{t("hiring.min")}</span><input className="fy-input" required inputMode="decimal" pattern="[0-9]{1,9}(\.[0-9]{1,2})?" value={f.min} onChange={set("min")} /></label>
                <label className="fy-field"><span>{t("hiring.max")}</span><input className="fy-input" required inputMode="decimal" pattern="[0-9]{1,9}(\.[0-9]{1,2})?" value={f.max} onChange={set("max")} /></label>
                <label className="fy-field"><span>{t("hiring.start")}</span><input className="fy-input" type="date" required value={f.start} onChange={set("start")} /></label>
                <label className="fy-field fy-row" style={{ gap: "var(--space-2)" }}><input type="checkbox" checked={f.pass} onChange={set("pass")} /><span>{t("hiring.needsPass")}</span></label>
                <button className="fy-btn fy-btn--primary" type="submit">{t("hiring.send")}</button>
              </form>
            )}
          </>
        )}
      </main>
    </Shell>
  );
}
