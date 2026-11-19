import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { t } from "../i18n";

type Final = components["schemas"]["FinalPay"];
type Doc = { id: string; title: string };
type Change = React.ChangeEvent<HTMLInputElement | HTMLSelectElement>;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("final.failed");

// FinalPay is the final pay of a person who is leaving (NM-PAY-06, NM-PAY-08, NM-INT-08): when it is due and the latest day by MOM's rule for the case, the leave left and what it is worth as a proposal
// to add as an item, and the tax-clearance hold, which holds every dollar due until it is released with a document of the person as evidence. It draws nothing for a person nobody has recorded leaving.
export function FinalPay({ person, docs, onChange }: { person: string; docs: Doc[]; onChange: () => void }) {
  const [f, setF] = useState<Final | null>(null);
  const [from, setFrom] = useState(new Date().toISOString().slice(0, 10));
  const [evidence, setEvidence] = useState("");
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const load = () => api.GET("/v1/people/{id}/final-pay", { params: { path: { id: person } } }).then(({ data }) => setF(data ?? null), () => setF(null));
  useEffect(() => {
    setF(null);
    void load();
  }, [person]); // eslint-disable-line react-hooks/exhaustive-deps
  if (!f) return null;
  const hold = async (action: "hold" | "release") => {
    const body = { action, effective_from: from, evidence_id: action === "release" ? evidence || docs[0]?.id : undefined };
    const { error } = await api.POST("/v1/people/{id}/tax-hold", { params: { path: { id: person } }, body });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t(action === "hold" ? "final.heldDone" : "final.releasedDone") });
    if (!error) {
      void load();
      onChange();
    }
  };
  return (
    <section className="fy-stack fy-stack--2" aria-labelledby="final-h">
      <h3 className="fy-h3" id="final-h">{t("final.title", { person: f.person })}</h3>
      <p>{t("final.due", { due: f.due, latest: f.latest, month: f.month.slice(0, 7) })}</p>
      <p className="fy-secondary">{f.rule}</p>
      {!f.notice_served && f.kind === "resignation" && <p className="fy-secondary">{t("final.noNotice")}</p>}
      {f.untaken_days && <p>{t("final.leave", { days: f.untaken_days })}{f.proposed_leave_pay ? ` ${t("final.proposal", { pay: f.proposed_leave_pay })}` : ""}</p>}
      <ul className="fy-stack fy-stack--1">
        {(f.items ?? []).map((i) => <li key={i.id}>{t("final.item", { name: i.name, amount: i.amount, used: i.used ? t("payroll.used") : t("payroll.waiting") })}</li>)}
      </ul>
      <p>{f.hold.held ? t("final.holdOn", { from: f.hold.effective_from ?? "" }) : t("final.holdOff")}</p>
      <div className="fy-stack fy-stack--2">
        <label className="fy-field"><span>{t("final.from")}</span><input className="fy-input" type="date" value={from} onChange={(ev: Change) => setFrom(ev.target.value)} /></label>
        {f.hold.held ? (
          <>
            <label className="fy-field">
              <span>{t("final.evidence")}</span>
              <select className="fy-input" value={evidence || docs[0]?.id || ""} onChange={(ev: Change) => setEvidence(ev.target.value)}>
                {docs.map((d) => <option key={d.id} value={d.id}>{d.title}</option>)}
              </select>
            </label>
            <button className="fy-btn fy-btn--primary" type="button" disabled={docs.length === 0} onClick={() => hold("release")}>{t("final.release")}</button>
          </>
        ) : (
          <button className="fy-btn fy-btn--secondary" type="button" onClick={() => hold("hold")}>{t("final.hold")}</button>
        )}
      </div>
      {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
    </section>
  );
}
