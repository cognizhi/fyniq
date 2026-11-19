import { useEffect, useState } from "react";
import { api } from "../api/client";
import { confirmWithPasskey } from "../api/stepup";
import type { components } from "../api/schema";
import { t } from "../i18n";

type Delegation = components["schemas"]["Delegation"];
type Type = components["schemas"]["LetterType"];
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("deleg.failed");

// Delegations is a signatory's standing permission to issue a standard letter at once (NM-LTR-04): the type, a monthly limit and an end date at most twelve months away. The
// Owner makes one after a fresh passkey confirmation and revokes it; the HR administrator reads them. Pay is never delegated: the API refuses it, and the type list leaves it out.
export function Delegations({ types, signatory }: { types: Type[]; signatory: boolean }) {
  const [list, setList] = useState<Delegation[]>([]);
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const [typ, setTyp] = useState("");
  const [monthly, setMonthly] = useState("50");
  const [ends, setEnds] = useState("");
  const load = () =>
    api.GET("/v1/letters/delegations").then(
      ({ data }) => setList(data?.delegations ?? []),
      () => setList([]),
    );
  useEffect(() => {
    void load();
  }, []);
  const offered = types.filter((x) => x.enabled && x.published_version > 0 && !x.needs && x.key !== "kets");
  const make = async (e: React.FormEvent) => {
    e.preventDefault();
    const failed = await confirmWithPasskey().catch(() => "");
    if (failed !== undefined) return setNote({ ok: false, text: failed || t("letters.passkeyFailed") });
    const { error } = await api.POST("/v1/letters/delegations", { body: { type: typ || offered[0]?.key || "", monthly_limit: Number(monthly), ends_on: ends } });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t("deleg.made") });
    if (!error) await load();
  };
  const revoke = async (id: string) => {
    const { error } = await api.POST("/v1/letters/delegations/{id}/revocation", { params: { path: { id } }, body: {} });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t("deleg.revoked") });
    if (!error) await load();
  };
  return (
    <section className="fy-stack fy-stack--2" aria-labelledby="deleg-h">
      <h2 className="fy-h2" id="deleg-h">{t("deleg.title")}</h2>
      <p className="fy-secondary">{t("deleg.lead")}</p>
      {list.length === 0 && <p className="fy-secondary">{t("deleg.none")}</p>}
      <ul className="fy-stack fy-stack--1">
        {list.map((d) => (
          <li key={d.id} className="fy-row" style={{ gap: "var(--space-3)" }}>
            <span>{t("deleg.line", { type: d.type, used: String(d.issued_this_month), limit: String(d.monthly_limit), ends: d.ends_on, state: t(`deleg.state.${d.state}`) })}</span>
            {signatory && d.state === "active" && <button className="fy-btn fy-btn--secondary" type="button" onClick={() => revoke(d.id)}>{t("deleg.revoke", { type: d.type })}</button>}
          </li>
        ))}
      </ul>
      {signatory && offered.length > 0 && (
        <form className="fy-stack fy-stack--2" onSubmit={make} aria-label={t("deleg.new")}>
          <h3 className="fy-h3">{t("deleg.new")}</h3>
          <label className="fy-field">
            <span>{t("deleg.type")}</span>
            <select className="fy-input" value={typ || offered[0]?.key} onChange={(e) => setTyp(e.target.value)}>
              {offered.map((x) => <option key={x.key} value={x.key}>{x.title}</option>)}
            </select>
          </label>
          <label className="fy-field"><span>{t("deleg.monthly")}</span><input className="fy-input" type="number" min={1} max={1000} required value={monthly} onChange={(e) => setMonthly(e.target.value)} /></label>
          <label className="fy-field"><span>{t("deleg.ends")}</span><input className="fy-input" type="date" required value={ends} onChange={(e) => setEnds(e.target.value)} /></label>
          <button className="fy-btn fy-btn--primary" type="submit">{t("deleg.make")}</button>
        </form>
      )}
      {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
    </section>
  );
}
