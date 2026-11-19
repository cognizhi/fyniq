import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { HoursWorked } from "../components/TimePanels";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Leave = components["schemas"]["LeaveRequest"];
type Flex = components["schemas"]["FlexRequest"];
type Note = { ok: boolean; text: string } | null;
type State = { status: "loading" } | { status: "failed" } | { status: "ready"; leave: Leave[]; flex: Flex[] };
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("requests.failed");
const arrangements = ["flexi_time", "flexi_place", "flexi_load"] as const;

// MyRequests is nm-my-requests: where a person's leave and flexible work requests are, with the formal flexible work request and its reply deadline (NM-LV-10). The
// reply deadline is the API's (the country pack's months after receipt); nothing here works it out.
export function MyRequests({ roles }: { roles: readonly string[] }) {
  const [state, set] = useState<State>({ status: "loading" });
  const [note, setNote] = useState<Note>(null);
  const load = async () => {
    try {
      const [leave, flex] = await Promise.all([api.GET("/v1/leave-requests", { params: { query: { mine: true } } }), api.GET("/v1/flexible-work-requests", { params: { query: { mine: true } } })]);
      set(leave.data && flex.data ? { status: "ready", leave: leave.data.requests ?? [], flex: flex.data.requests ?? [] } : { status: "failed" });
    } catch {
      set({ status: "failed" });
    }
  };
  useEffect(() => {
    void load();
  }, []);
  const done = async (error: unknown, ok: string) => {
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: ok });
    if (!error) await load();
  };
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-my-requests" />
        <h1 className="fy-h1" title={helpText("requests.page").tooltip}>{t("requests.title")}</h1>
        <p className="fy-secondary">{t("requests.lead")}</p>
        {state.status === "loading" && <p role="status" className="fy-secondary">{t("requests.loading")}</p>}
        {state.status === "failed" && <p role="alert" className="fy-secondary">{t("requests.failed")}</p>}
        {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
        {state.status === "ready" && (
          <>
            <section className="fy-stack fy-stack--2" aria-labelledby="leave-h">
              <h2 className="fy-h2" id="leave-h">{t("requests.leave")}</h2>
              {state.leave.length === 0 && <p className="fy-secondary">{t("requests.noLeave")}</p>}
              <ul className="fy-stack fy-stack--1">
                {state.leave.map((r) => (
                  <li key={r.id} className="fy-row" style={{ gap: "var(--space-3)" }}>
                    <span>{t("requests.leaveLine", { from: r.from, to: r.to, days: r.days, state: t(`state.${r.state}`) })}</span>
                    {(r.state === "pending" || r.state === "approved") && (
                      <button className="fy-btn fy-btn--secondary" type="button" onClick={async () => done((await api.POST("/v1/leave-requests/{id}/cancellation", { params: { path: { id: r.id } } })).error, t("requests.cancelled"))}>
                        {t("requests.cancel")}
                      </button>
                    )}
                  </li>
                ))}
              </ul>
            </section>
            <section className="fy-stack fy-stack--2" aria-labelledby="flex-h">
              <h2 className="fy-h2" id="flex-h">{t("requests.flex")}</h2>
              {state.flex.length === 0 && <p className="fy-secondary">{t("requests.noFlex")}</p>}
              <ul className="fy-stack fy-stack--2">
                {state.flex.map((r) => (
                  <li key={r.id} className="fy-stack fy-stack--1">
                    <span>
                      {r.state === "replied"
                        ? t("requests.flexReplied", { arrangement: t(`requests.arrangement.${r.arrangement}`), from: r.from, outcome: t(`requests.outcome.${r.outcome ?? "accepted"}`) })
                        : t("requests.flexLine", { arrangement: t(`requests.arrangement.${r.arrangement}`), from: r.from, state: t(`state.${r.state === "withdrawn" ? "cancelled" : "pending"}`), due: r.reply_due })}
                    </span>
                    {r.written_reply && <span className="fy-secondary">{t("requests.reply", { reply: r.written_reply })}</span>}
                    {r.state === "received" && (
                      <button className="fy-btn fy-btn--secondary" type="button" onClick={async () => done((await api.POST("/v1/flexible-work-requests/{id}/withdrawal", { params: { path: { id: r.id } } })).error, t("requests.withdrawn"))}>
                        {t("requests.withdraw")}
                      </button>
                    )}
                  </li>
                ))}
              </ul>
              <FlexForm sent={(due) => done(undefined, t("requests.sent", { due }))} failed={(e) => done(e, "")} />
            </section>
            <HoursWorked />
          </>
        )}
      </main>
    </Shell>
  );
}

function FlexForm({ sent, failed }: { sent: (due: string) => unknown; failed: (e: unknown) => unknown }) {
  const [arrangement, setArrangement] = useState<(typeof arrangements)[number]>("flexi_time");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [reason, setReason] = useState("");
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const { data, error } = await api.POST("/v1/me/flexible-work-requests", { body: { arrangement, from, to: to || undefined, reason } });
    if (data) await sent(data.reply_due);
    else await failed(error);
  };
  return (
    <form className="fy-stack fy-stack--2" onSubmit={submit} aria-labelledby="newflex-h">
      <h3 className="fy-h3" id="newflex-h">{t("requests.newFlex")}</h3>
      <label className="fy-field">
        <span>{t("requests.arrangement")}</span>
        <select className="fy-input" value={arrangement} onChange={(e) => setArrangement(e.target.value as (typeof arrangements)[number])}>
          {arrangements.map((a) => (
            <option key={a} value={a}>{t(`requests.arrangement.${a}`)}</option>
          ))}
        </select>
      </label>
      <label className="fy-field">
        <span>{t("requests.from")}</span>
        <input className="fy-input" type="date" required value={from} onChange={(e) => setFrom(e.target.value)} />
      </label>
      <label className="fy-field">
        <span>{t("requests.to")}</span>
        <input className="fy-input" type="date" value={to} min={from} onChange={(e) => setTo(e.target.value)} />
      </label>
      <label className="fy-field">
        <span>{t("requests.reason")}</span>
        <textarea className="fy-input" required maxLength={2000} value={reason} onChange={(e) => setReason(e.target.value)} />
      </label>
      <button className="fy-btn fy-btn--primary" type="submit">{t("requests.send")}</button>
    </form>
  );
}
