import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { t } from "../i18n";

type Payments = components["schemas"]["RunPayments"];
type Doc = { id: string; title: string };
type Change = React.ChangeEvent<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>;
const STATES = ["scheduled", "released", "sent", "accepted", "paid", "rejected", "returned", "unknown"] as const;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("payments.failed");

// PayPayments is the payments of an approved run (NM-PAY-10): each person's state, what was owed and what was confirmed, and a form to record an outcome with a document of the person as evidence.
// "Paid" is never a bare word: the form has no way to send one without evidence.
export function PayPayments({ run }: { run: string }) {
  const [p, setP] = useState<Payments | null>(null);
  const [docs, setDocs] = useState<Doc[]>([]);
  const [f, setF] = useState({ person: "", state: "paid", amount: "", date: new Date().toISOString().slice(0, 10), evidence: "", note: "" });
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const load = () => api.GET("/v1/payroll/runs/{id}/payments", { params: { path: { id: run } } }).then(({ data }) => setP(data ?? null), () => setP(null));
  useEffect(() => {
    void load();
  }, [run]); // eslint-disable-line react-hooks/exhaustive-deps
  const person = f.person || p?.lines?.[0]?.person_id || "";
  useEffect(() => {
    if (!person) return;
    api.GET("/v1/people/{id}/documents", { params: { path: { id: person } } }).then(
      ({ data }) => setDocs((data?.documents ?? []).map((d) => ({ id: d.id, title: d.title }))),
      () => setDocs([]),
    );
  }, [person]);
  if (!p) return null;
  const lines = p.lines ?? [];
  const owed = lines.find((l) => l.person_id === person)?.owed ?? "";
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    const { error } = await api.POST("/v1/payroll/runs/{id}/payments", { params: { path: { id: run } }, body: { person_id: person, state: f.state as (typeof STATES)[number], amount: f.amount || owed, date: f.date, evidence_id: f.evidence || docs[0]?.id || "", note: f.note || undefined } });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t("payments.saved") });
    if (!error) void load();
  };
  return (
    <section className="fy-stack fy-stack--2" aria-labelledby="payments-h">
      <h2 className="fy-h2" id="payments-h">{t("payments.title")}</h2>
      <p>{t("payments.run", { version: String(p.version), state: t(`payments.run.${p.state}`) })}</p>
      <ul className="fy-stack fy-stack--1">
        {lines.map((l) => (
          <li key={l.person_id}>
            {t("payments.line", { person: l.person, owed: l.owed, state: l.state ? t(`payments.state.${l.state}`) : t("payments.none") })}
            {l.attention && <span className="fy-chip"> {t("payments.attention")}</span>}
            {l.amount_mismatch && <span className="fy-chip"> {t("payments.mismatch")}</span>}
          </li>
        ))}
      </ul>
      <form className="fy-stack fy-stack--2" onSubmit={submit} aria-label={t("payments.record")}>
        <h3 className="fy-h3">{t("payments.record")}</h3>
        <p className="fy-secondary">{t("payments.lead")}</p>
        <label className="fy-field">
          <span>{t("payments.person")}</span>
          <select className="fy-input" value={person} onChange={(ev: Change) => setF({ ...f, person: ev.target.value })}>
            {lines.map((l) => <option key={l.person_id} value={l.person_id}>{l.person}</option>)}
          </select>
        </label>
        <label className="fy-field">
          <span>{t("payments.stateLabel")}</span>
          <select className="fy-input" value={f.state} onChange={(ev: Change) => setF({ ...f, state: ev.target.value })}>
            {STATES.map((s) => <option key={s} value={s}>{t(`payments.state.${s}`)}</option>)}
          </select>
        </label>
        <label className="fy-field"><span>{t("payments.amount")}</span><input className="fy-input" inputMode="decimal" value={f.amount || owed} onChange={(ev: Change) => setF({ ...f, amount: ev.target.value })} /></label>
        <label className="fy-field"><span>{t("payments.date")}</span><input className="fy-input" type="date" required value={f.date} onChange={(ev: Change) => setF({ ...f, date: ev.target.value })} /></label>
        <label className="fy-field">
          <span>{t("payments.evidence")}</span>
          <select className="fy-input" value={f.evidence || docs[0]?.id || ""} onChange={(ev: Change) => setF({ ...f, evidence: ev.target.value })}>
            {docs.map((d) => <option key={d.id} value={d.id}>{d.title}</option>)}
          </select>
        </label>
        {docs.length === 0 && <p className="fy-secondary">{t("payments.noEvidence")}</p>}
        <label className="fy-field"><span>{t("payments.note")}</span><input className="fy-input" maxLength={500} value={f.note} onChange={(ev: Change) => setF({ ...f, note: ev.target.value })} /></label>
        <button className="fy-btn fy-btn--primary" type="submit" disabled={docs.length === 0}>{t("payments.submit")}</button>
      </form>
      {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
    </section>
  );
}
