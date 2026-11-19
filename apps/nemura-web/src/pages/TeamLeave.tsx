import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { FlexReplies } from "../components/FlexReplies";
import { LeaveCalendar } from "../components/LeaveCalendar";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Request = components["schemas"]["LeaveRequest"];
type Sick = components["schemas"]["SickNotification"];
type State = { status: "loading" } | { status: "failed" } | { status: "ready"; list: Request[] };
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("team.failed");

// TeamLeave is nm-team-leave: the requests waiting for the caller's decision and the ones decided lately, each with the person, the dates and the days (NM-LV-04). A
// refusal needs a reason, which the API enforces; the caller never decides their own, and sees only the requests the API puts in their scope.
export function TeamLeave({ roles }: { roles: readonly string[] }) {
  const [state, set] = useState<State>({ status: "loading" });
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const [reasons, setReasons] = useState<Record<string, string>>({});
  const [sick, setSick] = useState<Sick[]>([]);
  const replier = roles.some((r) => ["hr_officer", "hr_admin", "owner"].includes(r));
  const peopleTeam = roles.some((r) => ["hr_officer", "hr_admin"].includes(r));
  const load = async () => {
    try {
      const { data } = await api.GET("/v1/leave-requests");
      set(data ? { status: "ready", list: data.requests ?? [] } : { status: "failed" });
    } catch {
      set({ status: "failed" });
    }
  };
  const loadSick = async () => {
    if (!peopleTeam) return;
    try {
      const { data } = await api.GET("/v1/sick-leave", { params: { query: { unverified: true } } });
      setSick(data?.notifications ?? []);
    } catch {
      setSick([]);
    }
  };
  useEffect(() => {
    void load();
    void loadSick();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const verify = async (n: Sick) => {
    const { error } = await api.POST("/v1/sick-leave/{id}/verification", { params: { path: { id: n.id } } });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t("team.verified") });
    if (!error) await loadSick();
  };
  const decide = async (r: Request, decision: "approve" | "refuse") => {
    const { error } = await api.POST("/v1/leave-requests/{id}/decision", { params: { path: { id: r.id } }, body: { decision, reason: reasons[r.id] ?? "" } });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t(decision === "approve" ? "team.approved" : "team.refused") });
    if (!error) await load();
  };
  const list = state.status === "ready" ? state.list : [];
  const pending = list.filter((r) => r.state === "pending");
  const decided = list.filter((r) => r.state !== "pending");
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-team-leave" />
        <h1 className="fy-h1" title={helpText("team.page").tooltip}>{t("team.title")}</h1>
        <p className="fy-secondary">{t("team.lead")}</p>
        {state.status === "loading" && <p role="status" className="fy-secondary">{t("team.loading")}</p>}
        {state.status === "failed" && <p role="alert" className="fy-secondary">{t("team.none")}</p>}
        {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
        {state.status === "ready" && (
          <>
            <section className="fy-stack fy-stack--2" aria-labelledby="pending-h">
              <h2 className="fy-h2" id="pending-h">{t("team.pending")}</h2>
              {pending.length === 0 ? (
                <p className="fy-secondary">{t("team.noPending")}</p>
              ) : (
                <table className="fy-table">
                  <thead>
                    <tr>
                      <th scope="col">{t("team.col.person")}</th>
                      <th scope="col">{t("team.col.dates")}</th>
                      <th scope="col">{t("team.col.days")}</th>
                      <th scope="col">{t("team.col.note")}</th>
                      <th scope="col">{t("team.col.decide")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pending.map((r) => (
                      <tr key={r.id}>
                        <td>{r.person_name}</td>
                        <td>{t("team.dates", { from: r.from, to: r.to })}</td>
                        <td className="fy-num">{r.days}</td>
                        <td>{r.note ?? ""}</td>
                        <td>
                          <div className="fy-row" style={{ gap: "var(--space-2)", flexWrap: "wrap" }}>
                            <button className="fy-btn fy-btn--primary" type="button" onClick={() => decide(r, "approve")}>{t("team.approve")}</button>
                            <input className="fy-input" aria-label={t("team.reasonFor", { name: r.person_name })} value={reasons[r.id] ?? ""} maxLength={500} onChange={(e) => setReasons({ ...reasons, [r.id]: e.target.value })} />
                            <button className="fy-btn fy-btn--secondary" type="button" onClick={() => decide(r, "refuse")}>{t("team.refuse")}</button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </section>
            <section className="fy-stack fy-stack--2" aria-labelledby="decided-h">
              <h2 className="fy-h2" id="decided-h">{t("team.decided")}</h2>
              {decided.length === 0 ? (
                <p className="fy-secondary">{t("team.noDecided")}</p>
              ) : (
                <table className="fy-table">
                  <thead>
                    <tr>
                      <th scope="col">{t("team.col.person")}</th>
                      <th scope="col">{t("team.col.dates")}</th>
                      <th scope="col">{t("team.col.days")}</th>
                      <th scope="col">{t("team.col.state")}</th>
                      <th scope="col">{t("team.col.reason")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {decided.map((r) => (
                      <tr key={r.id}>
                        <td>{r.person_name}</td>
                        <td>{t("team.dates", { from: r.from, to: r.to })}</td>
                        <td className="fy-num">{r.days}</td>
                        <td>{t(`state.${r.state}`)}</td>
                        <td>{r.decision_reason ?? ""}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </section>
          </>
        )}
        {peopleTeam && (
          <section className="fy-stack fy-stack--2" aria-labelledby="sick-h">
            <h2 className="fy-h2" id="sick-h">{t("team.sickTitle")}</h2>
            {sick.length === 0 ? (
              <p className="fy-secondary">{t("team.noSick")}</p>
            ) : (
              <table className="fy-table">
                <thead>
                  <tr>
                    <th scope="col">{t("team.col.person")}</th>
                    <th scope="col">{t("team.col.dates")}</th>
                    <th scope="col">{t("team.col.days")}</th>
                    <th scope="col">{t("team.col.certificate")}</th>
                    <th scope="col">{t("team.col.decide")}</th>
                  </tr>
                </thead>
                <tbody>
                  {sick.map((n) => (
                    <tr key={n.id}>
                      <td>{n.person_name}</td>
                      <td>{t("team.dates", { from: n.from, to: n.to })}</td>
                      <td className="fy-num">{n.days}</td>
                      <td>{t("team.issuedBy", { issuer: n.issuer ?? "" })}</td>
                      <td>
                        <button className="fy-btn fy-btn--primary" type="button" onClick={() => verify(n)}>{t("team.verify")}</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </section>
        )}
        {replier && <FlexReplies />}
        <LeaveCalendar />
      </main>
    </Shell>
  );
}
