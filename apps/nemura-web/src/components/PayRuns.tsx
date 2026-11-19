import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { t } from "../i18n";

type Run = components["schemas"]["Run"];
type Detail = components["schemas"]["RunDetail"];
type Group = components["schemas"]["PayGroup"];
type Person = { id: string; name: string };
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("runs.failed");
const fields = ["cpf_employee", "cpf_employer", "sdl", "shg"] as const;
type Change = React.ChangeEvent<HTMLInputElement | HTMLSelectElement>;

// PayRuns is a month's pay runs (FR-PAY-01, FR-PAY-06, NM-PAY-09): calculate a run, read each person's month with its flags and working, override a figure with a reason, and calculate the next
// version. A version is read-only once written; every change is the next version, which supersedes the one before. The approval is its own screen.
export function PayRuns({ month, groups, people }: { month: string; groups: Group[]; people: Person[] }) {
  const [runs, setRuns] = useState<Run[]>([]);
  const [open, setOpen] = useState<Detail | null>(null);
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const [group, setGroup] = useState("");
  const [o, setO] = useState({ person: "", field: "cpf_employee" as (typeof fields)[number], value: "", reason: "" });
  const [reason, setReason] = useState("");
  const load = () => api.GET("/v1/payroll/runs", { params: { query: { month } } }).then(({ data }) => setRuns(data?.runs ?? []), () => setRuns([]));
  const show = (id: string) => api.GET("/v1/payroll/runs/{id}", { params: { path: { id } } }).then(({ data }) => setOpen(data ?? null), () => setOpen(null));
  useEffect(() => {
    setOpen(null);
    void load();
  }, [month]); // eslint-disable-line react-hooks/exhaustive-deps
  const done = (error: unknown, ok: string) => {
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: ok });
    return !error;
  };
  const start = async (ev: React.FormEvent) => {
    ev.preventDefault();
    const { data, error } = await api.POST("/v1/payroll/runs", { body: { month, pay_group_id: group || groups[0]?.id || "" } });
    if (done(error, t("runs.started"))) {
      await load();
      if (data) await show(data.id);
    }
  };
  const next = async () => {
    if (!open) return;
    if (done((await api.POST("/v1/payroll/runs/{id}/versions", { params: { path: { id: open.id } }, body: { reason } })).error, t("runs.calculated"))) {
      await load();
      await show(open.id);
    }
  };
  const override = async (ev: React.FormEvent) => {
    ev.preventDefault();
    if (!open) return;
    if (done((await api.POST("/v1/payroll/runs/{id}/overrides", { params: { path: { id: open.id } }, body: { person_id: o.person || people[0]?.id || "", field: o.field, value: o.value, reason: o.reason } })).error, t("runs.overridden"))) await show(open.id);
  };
  const withdraw = async (id: string) => {
    if (open && done((await api.POST("/v1/payroll/overrides/{id}/withdrawal", { params: { path: { id } } })).error, t("runs.withdrawn"))) await show(open.id);
  };
  return (
    <section className="fy-stack fy-stack--2" aria-labelledby="runs-h">
      <h2 className="fy-h2" id="runs-h">{t("runs.title")}</h2>
      {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
      {runs.length === 0 && <p className="fy-secondary">{t("runs.none")}</p>}
      <ul className="fy-stack fy-stack--1">
        {runs.map((r) => (
          <li key={r.id} className="fy-row" style={{ gap: "var(--space-3)" }}>
            <span>{t("runs.line", { group: r.pay_group, version: String(r.latest.version), state: t(`runs.state.${r.latest.state}`), people: String(r.latest.people) })}</span>
            <button className="fy-btn fy-btn--secondary" type="button" onClick={() => show(r.id)}>{t("runs.open", { group: r.pay_group })}</button>
          </li>
        ))}
      </ul>
      <form className="fy-stack fy-stack--2" onSubmit={start} aria-label={t("runs.start")}>
        <h3 className="fy-h3">{t("runs.start")}</h3>
        <label className="fy-field">
          <span>{t("payroll.groupLabel")}</span>
          <select className="fy-input" value={group || groups[0]?.id} onChange={(ev: Change) => setGroup(ev.target.value)}>
            {groups.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}
          </select>
        </label>
        <button className="fy-btn fy-btn--primary" type="submit">{t("runs.calculate")}</button>
      </form>
      {open && (
        <div className="fy-stack fy-stack--2" aria-label={t("runs.detail", { group: open.pay_group })}>
          <h3 className="fy-h3">{t("runs.detail", { group: open.pay_group })}</h3>
          <a href={`/people/payroll/runs/${open.id}`}>{t("runs.openApproval", { group: open.pay_group })}</a>
          <p>{t("runs.summary", { version: String(open.latest.version), state: t(`runs.state.${open.latest.state}`), digest: open.latest.digest.slice(0, 12), failing: String(open.latest.flags_failing), warning: String(open.latest.flags_warning) })}</p>
          <p>{t("runs.totals", { gross: open.latest.totals.gross, ee: open.latest.totals.cpf_employee, er: open.latest.totals.cpf_employer, sdl: open.latest.totals.sdl_payable, net: open.latest.totals.net, cost: open.latest.totals.employer_cost })}</p>
          <table className="fy-table">
            <thead>
              <tr>
                <th scope="col">{t("runs.person")}</th>
                <th scope="col">{t("runs.gross")}</th>
                <th scope="col">{t("runs.cpfEe")}</th>
                <th scope="col">{t("runs.cpfEr")}</th>
                <th scope="col">{t("runs.net")}</th>
                <th scope="col">{t("runs.flags")}</th>
              </tr>
            </thead>
            <tbody>
              {(open.lines ?? []).map((l) => (
                <tr key={l.person_id}>
                  <th scope="row">{l.person}</th>
                  <td>{l.gross}</td>
                  <td>{l.cpf_employee}</td>
                  <td>{l.cpf_employer}</td>
                  <td>{l.net}</td>
                  <td>{(l.flags ?? []).map((f) => <span key={f.code + f.message} className="fy-chip">{t(f.fail ? "runs.fail" : "runs.warn", { message: f.message })}</span>)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {(open.overrides ?? []).length > 0 && (
            <ul className="fy-stack fy-stack--1">
              {(open.overrides ?? []).map((x) => (
                <li key={x.id} className="fy-row" style={{ gap: "var(--space-3)" }}>
                  <span>{t("runs.overrideLine", { person: x.person, field: x.field, value: x.value, reason: x.reason })}</span>
                  <button className="fy-btn fy-btn--secondary" type="button" onClick={() => withdraw(x.id)}>{t("runs.withdraw", { person: x.person })}</button>
                </li>
              ))}
            </ul>
          )}
          <form className="fy-stack fy-stack--2" onSubmit={override} aria-label={t("runs.override")}>
            <h4 className="fy-h4">{t("runs.override")}</h4>
            <label className="fy-field">
              <span>{t("runs.person")}</span>
              <select className="fy-input" value={o.person || people[0]?.id} onChange={(ev: Change) => setO({ ...o, person: ev.target.value })}>
                {people.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </select>
            </label>
            <label className="fy-field">
              <span>{t("runs.figure")}</span>
              <select className="fy-input" value={o.field} onChange={(ev: Change) => setO({ ...o, field: ev.target.value as (typeof fields)[number] })}>
                {fields.map((f) => <option key={f} value={f}>{t(`runs.field.${f}`)}</option>)}
              </select>
            </label>
            <label className="fy-field"><span>{t("runs.value")}</span><input className="fy-input" inputMode="decimal" required value={o.value} onChange={(ev) => setO({ ...o, value: ev.target.value })} /></label>
            <label className="fy-field"><span>{t("runs.why")}</span><input className="fy-input" required minLength={10} maxLength={500} value={o.reason} onChange={(ev) => setO({ ...o, reason: ev.target.value })} /></label>
            <button className="fy-btn fy-btn--secondary" type="submit">{t("runs.addOverride")}</button>
          </form>
          <label className="fy-field"><span>{t("runs.correctionReason")}</span><input className="fy-input" maxLength={500} value={reason} onChange={(ev) => setReason(ev.target.value)} /></label>
          <button className="fy-btn fy-btn--primary" type="button" onClick={next}>{t("runs.next")}</button>
        </div>
      )}
    </section>
  );
}
