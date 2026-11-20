import { useEffect, useState } from "react";
import { api } from "../api/client";
import { confirmWithPasskey } from "../api/stepup";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, team } from "./Shell";

type Order = components["schemas"]["ErasureOrderView"];
type Note = { ok: boolean; text: string } | null;
interface Go {
  (): Promise<Note>;
}
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("erasure.failed");

// Erasures is nm-erasures (NM-SEC-08): an Owner or an HR administrator asks for a former employee's personal data to be erased once the retention periods have run, and a second one approves, each with a passkey.
// The page names people by ID only. What the database keeps append-only (letters, payslip figures, pay run lines) stays, and the page says so.
export function Erasures({ roles }: { roles: readonly string[] }) {
  const [orders, setOrders] = useState<Order[]>([]);
  const [person, setPerson] = useState("");
  const [reason, setReason] = useState("");
  const [note, setNote] = useState<Note>(null);
  const allowed = roles.some((r) => r === "owner" || r === "hr_admin");
  const load = () => {
    if (allowed) api.GET("/v1/erasures").then(({ data }) => setOrders(data?.items ?? []), () => setOrders([]));
  };
  useEffect(load, []); // eslint-disable-line react-hooks/exhaustive-deps
  const withPasskey = async (go: Go) => {
    const failed = await confirmWithPasskey().catch(() => "");
    if (failed !== undefined) return setNote({ ok: false, text: failed || t("letters.passkeyFailed") });
    setNote(await go());
    load();
  };
  const ask = (ev: React.FormEvent) => {
    ev.preventDefault();
    return withPasskey(async () => {
      const { error } = await api.POST("/v1/erasures", { body: { person_id: person, reason } });
      if (!error) {
        setPerson("");
        setReason("");
      }
      return error ? { ok: false, text: problem(error) } : { ok: true, text: t("erasure.asked") };
    });
  };
  const decide = (id: string, approve: boolean) =>
    withPasskey(async () => {
      const { error } = await api.POST("/v1/erasures/{id}/decision", { params: { path: { id } }, body: { approve } });
      return error ? { ok: false, text: problem(error) } : { ok: true, text: t(approve ? "erasure.approved" : "erasure.refused") };
    });
  return (
    <Shell roles={roles} directory={team(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-erasures" />
        <h1 className="fy-h1" title={helpText("erasure.page").tooltip}>{t("erasure.title")}</h1>
        {!allowed && <p role="alert" className="fy-secondary">{t("erasure.denied")}</p>}
        {allowed && (
          <>
            <p className="fy-secondary">{t("erasure.lead")}</p>
            {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
            <form className="fy-stack fy-stack--2" onSubmit={ask} aria-labelledby="ask-h">
              <h2 className="fy-h2" id="ask-h">{t("erasure.ask")}</h2>
              <label className="fy-field">{t("erasure.person")}<input className="fy-input" value={person} onChange={(e) => setPerson(e.target.value)} required pattern="[0-9a-fA-F-]{36}" autoComplete="off" /></label>
              <label className="fy-field">{t("erasure.reason")}<input className="fy-input" value={reason} onChange={(e) => setReason(e.target.value)} required maxLength={500} /></label>
              <button className="fy-btn fy-btn--primary" type="submit">{t("erasure.askButton")}</button>
            </form>
            <section className="fy-stack fy-stack--2" aria-labelledby="orders-h">
              <h2 className="fy-h2" id="orders-h">{t("erasure.orders")}</h2>
              {orders.length === 0 ? <p className="fy-secondary">{t("erasure.none")}</p> : (
                <table className="fy-table">
                  <thead><tr><th scope="col">{t("erasure.col.person")}</th><th scope="col">{t("erasure.col.state")}</th><th scope="col">{t("erasure.col.eligible")}</th><th scope="col">{t("erasure.col.action")}</th></tr></thead>
                  <tbody>{orders.map((o) => (
                    <tr key={o.id}>
                      <td>{o.person_id.slice(0, 8)}</td><td>{t(`erasure.state.${o.state}`)}</td><td>{o.eligible_on}</td>
                      <td>{o.state === "requested" && (
                        <span className="fy-row" style={{ gap: "var(--space-3)" }}>
                          <button className="fy-btn fy-btn--primary fy-btn--sm" type="button" onClick={() => decide(o.id, true)}>{t("erasure.approve")}</button>
                          <button className="fy-btn fy-btn--secondary fy-btn--sm" type="button" onClick={() => decide(o.id, false)}>{t("erasure.refuse")}</button>
                        </span>
                      )}</td>
                    </tr>
                  ))}</tbody>
                </table>
              )}
            </section>
            <p className="fy-secondary">{t("erasure.keeps")}</p>
          </>
        )}
      </main>
    </Shell>
  );
}
