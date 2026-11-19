import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { FinalPay } from "../components/FinalPay";
import { PayRuns } from "../components/PayRuns";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Group = components["schemas"]["PayGroup"];
type PayView = components["schemas"]["PayView"];
type Item = components["schemas"]["PayItem"];
type OneOff = components["schemas"]["OneOff"];
type Entry = components["schemas"]["DirectoryEntry"];
type Doc = components["schemas"]["Document"];
type Note = { ok: boolean; text: string } | null;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("payroll.failed");
const funds = ["", "CDAC", "ECF", "MBMF", "SINDA"] as const;
const today = (): string => new Date().toISOString().slice(0, 10);
const thisMonth = (): string => `${new Date().toISOString().slice(0, 7)}-01`;
const blankItem: Item = { key: "", name: "", kind: "allowance", nature: "allowance", amount: "", cpf_wages: true };
type Change = React.ChangeEvent<HTMLInputElement | HTMLSelectElement>;

// PayrollHR is nm-payroll-hr: the pay groups and calendar, a person's pay record with its allowances and fixed deductions, the permanent resident's joint election, the year to date
// imported at cut-over, and the one-off items of a month (FR-EMP-01, FR-PAY-14, FR-PAY-17, NM-PAY-02, NM-PAY-03). Pay is for the Payroll administrator and the Owner alone: the API
// refuses anyone else, and this page says so rather than drawing a form that would fail.
export function PayrollHR({ roles }: { roles: readonly string[] }) {
  const allowed = roles.some((r) => ["payroll_admin", "owner"].includes(r));
  const [groups, setGroups] = useState<Group[]>([]);
  const [people, setPeople] = useState<Entry[]>([]);
  const [person, setPerson] = useState("");
  const [pay, setPay] = useState<PayView | null>(null);
  const [docs, setDocs] = useState<Doc[]>([]);
  const [month, setMonth] = useState(thisMonth());
  const [oneOffs, setOneOffs] = useState<OneOff[]>([]);
  const [note, setNote] = useState<Note>(null);
  const [g, setG] = useState({ name: "", day: "25", cutoff: "7", approval: "3" });
  const [f, setF] = useState({ from: today(), group: "", basic: "", fund: "", optOut: false });
  const [items, setItems] = useState<Item[]>([]);
  const [y, setY] = useState({ year: String(new Date().getFullYear()), ow: "", aw: "", paid: "", through: "" });
  const [e, setE] = useState({ from: today(), evidence: "" });
  const [o, setO] = useState({ key: "", name: "", nature: "bonus", amount: "", cpf: true, forMonth: "", payable: "" });
  const pid = person || people[0]?.id || "";
  const done = (error: unknown, ok: string) => {
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: ok });
    return !error;
  };
  const loadGroups = () => api.GET("/v1/payroll/groups").then(({ data }) => setGroups(data?.groups ?? []), () => setGroups([]));
  const loadPay = () => {
    if (!pid) return;
    api.GET("/v1/people/{id}/pay", { params: { path: { id: pid } } }).then(({ data }) => setPay(data?.recorded ? data.pay : null), () => setPay(null));
    api.GET("/v1/people/{id}/documents", { params: { path: { id: pid } } }).then(({ data }) => setDocs(data?.documents ?? []), () => setDocs([]));
  };
  const loadOneOffs = () => api.GET("/v1/payroll/one-offs", { params: { query: { month } } }).then(({ data }) => setOneOffs(data?.items ?? []), () => setOneOffs([]));
  useEffect(() => {
    if (!allowed) return;
    void loadGroups();
    api.GET("/v1/people").then(({ data }) => setPeople(data?.people ?? []), () => setPeople([]));
  }, [allowed]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(loadPay, [pid]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (allowed) void loadOneOffs();
  }, [month, allowed]); // eslint-disable-line react-hooks/exhaustive-deps
  const makeGroup = async (ev: React.FormEvent) => {
    ev.preventDefault();
    if (done((await api.POST("/v1/payroll/groups", { body: { name: g.name, pay_day: Number(g.day), cutoff_days: Number(g.cutoff), approval_days: Number(g.approval) } })).error, t("payroll.groupMade"))) await loadGroups();
  };
  const savePay = async (ev: React.FormEvent) => {
    ev.preventDefault();
    const body = { effective_from: f.from, pay: { pay_group_id: f.group || groups[0]?.id || "", basic_monthly: f.basic, shg_fund: (f.fund || undefined) as "CDAC" | "ECF" | "MBMF" | "SINDA" | undefined, shg_opt_out: f.optOut, items } };
    if (done((await api.POST("/v1/people/{id}/pay", { params: { path: { id: pid } }, body })).error, t("payroll.paySaved"))) loadPay();
  };
  const importYTD = async (ev: React.FormEvent) => {
    ev.preventDefault();
    if (done((await api.POST("/v1/people/{id}/pay/year-to-date", { params: { path: { id: pid } }, body: { year: Number(y.year), ow: y.ow, aw_subject: y.aw, aw_paid: y.paid, through_month: y.through } })).error, t("payroll.ytdSaved"))) loadPay();
  };
  const elect = async (ev: React.FormEvent) => {
    ev.preventDefault();
    if (done((await api.POST("/v1/people/{id}/pr-election", { params: { path: { id: pid } }, body: { effective_from: e.from, evidence_document_id: e.evidence || docs[0]?.id || "" } })).error, t("payroll.electionSaved"))) loadPay();
  };
  const addOneOff = async (ev: React.FormEvent) => {
    ev.preventDefault();
    const item = { key: o.key, name: o.name, nature: o.nature as "bonus", amount: o.amount, cpf_wages: o.cpf, for_month: o.forMonth || undefined, payable_on: o.payable || undefined };
    if (done((await api.POST("/v1/people/{id}/pay/one-offs", { params: { path: { id: pid } }, body: { month, item } })).error, t("payroll.oneOffAdded"))) await loadOneOffs();
  };
  const setItem = (i: number, k: keyof Item, v: string | boolean) => setItems(items.map((it, j) => (j === i ? { ...it, [k]: v } : it)));
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-payroll-hr" />
        <h1 className="fy-h1" title={helpText("payroll.page").tooltip}>{t("payroll.title")}</h1>
        <p className="fy-secondary">{t("payroll.lead")}</p>
        {!allowed && <p role="alert" className="fy-secondary">{t("payroll.denied")}</p>}
        {allowed && (
          <>
            {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
            <section className="fy-stack fy-stack--2" aria-labelledby="groups-h">
              <h2 className="fy-h2" id="groups-h">{t("payroll.groups")}</h2>
              {groups.length === 0 && <p className="fy-secondary">{t("payroll.noGroups")}</p>}
              <ul className="fy-stack fy-stack--1">{groups.map((x) => <li key={x.id}>{t("payroll.group", { name: x.name, day: x.pay_day === 0 ? t("payroll.lastDay") : String(x.pay_day), cutoff: String(x.cutoff_days), approval: String(x.approval_days) })}</li>)}</ul>
              <form className="fy-stack fy-stack--2" onSubmit={makeGroup} aria-label={t("payroll.newGroup")}>
                <h3 className="fy-h3">{t("payroll.newGroup")}</h3>
                <label className="fy-field"><span>{t("payroll.groupName")}</span><input className="fy-input" required maxLength={80} value={g.name} onChange={(ev) => setG({ ...g, name: ev.target.value })} /></label>
                <label className="fy-field"><span>{t("payroll.payDay")}</span><input className="fy-input" type="number" min={0} max={28} required value={g.day} onChange={(ev) => setG({ ...g, day: ev.target.value })} /></label>
                <label className="fy-field"><span>{t("payroll.cutoff")}</span><input className="fy-input" type="number" min={0} max={20} required value={g.cutoff} onChange={(ev) => setG({ ...g, cutoff: ev.target.value })} /></label>
                <label className="fy-field"><span>{t("payroll.approval")}</span><input className="fy-input" type="number" min={0} max={20} required value={g.approval} onChange={(ev) => setG({ ...g, approval: ev.target.value })} /></label>
                <button className="fy-btn fy-btn--primary" type="submit">{t("payroll.makeGroup")}</button>
              </form>
            </section>
            <section className="fy-stack fy-stack--2" aria-labelledby="pay-h">
              <h2 className="fy-h2" id="pay-h">{t("payroll.person")}</h2>
              <label className="fy-field">
                <span>{t("onb.person")}</span>
                <select className="fy-input" value={pid} onChange={(ev: Change) => setPerson(ev.target.value)}>
                  {people.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
                </select>
              </label>
              {pay ? (
                <dl className="fy-stack fy-stack--1">
                  <div className="fy-row" style={{ gap: "var(--space-3)" }}><dt className="fy-strong">{t("payroll.basic")}</dt><dd>{pay.basic_monthly}</dd></div>
                  <div className="fy-row" style={{ gap: "var(--space-3)" }}><dt className="fy-strong">{t("payroll.groupLabel")}</dt><dd>{pay.pay_group}</dd></div>
                  {pay.shg_fund && <div className="fy-row" style={{ gap: "var(--space-3)" }}><dt className="fy-strong">{t("payroll.fund")}</dt><dd>{pay.shg_fund}{pay.shg_opt_out ? ` (${t("payroll.optedOut")})` : ""}</dd></div>}
                  {(pay.items ?? []).map((it) => <div key={it.key} className="fy-row" style={{ gap: "var(--space-3)" }}><dt className="fy-strong">{it.name}</dt><dd>{it.amount}</dd></div>)}
                  <div className="fy-row" style={{ gap: "var(--space-3)" }}><dt className="fy-strong">{t("payroll.since")}</dt><dd>{pay.effective_from}</dd></div>
                  {pay.joint_election && <div><dt className="fy-strong">{t("payroll.election")}</dt></div>}
                  {pay.year_to_date && <div className="fy-row" style={{ gap: "var(--space-3)" }}><dt className="fy-strong">{t("payroll.ytd")}</dt><dd>{t("payroll.ytdLine", { year: String(pay.year_to_date.year), ow: pay.year_to_date.ow, aw: pay.year_to_date.aw_subject })}</dd></div>}
                </dl>
              ) : <p className="fy-secondary">{t("payroll.noPay")}</p>}
              <form className="fy-stack fy-stack--2" onSubmit={savePay} aria-label={t("payroll.startPay")}>
                <h3 className="fy-h3">{t("payroll.startPay")}</h3>
                <label className="fy-field"><span>{t("payroll.from")}</span><input className="fy-input" type="date" required value={f.from} onChange={(ev) => setF({ ...f, from: ev.target.value })} /></label>
                <label className="fy-field">
                  <span>{t("payroll.groupLabel")}</span>
                  <select className="fy-input" value={f.group || groups[0]?.id} onChange={(ev: Change) => setF({ ...f, group: ev.target.value })}>
                    {groups.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}
                  </select>
                </label>
                <label className="fy-field"><span>{t("payroll.basic")}</span><input className="fy-input" inputMode="decimal" required pattern="[0-9]{1,14}(\.[0-9]{1,2})?" value={f.basic} onChange={(ev) => setF({ ...f, basic: ev.target.value })} /></label>
                <label className="fy-field">
                  <span>{t("payroll.fund")}</span>
                  <select className="fy-input" value={f.fund} onChange={(ev: Change) => setF({ ...f, fund: ev.target.value })}>
                    {funds.map((x) => <option key={x} value={x}>{x || t("payroll.noFund")}</option>)}
                  </select>
                </label>
                <label className="fy-field"><span>{t("payroll.optOut")}</span><input type="checkbox" checked={f.optOut} onChange={(ev) => setF({ ...f, optOut: ev.target.checked })} /></label>
                {items.map((it, i) => (
                  <fieldset key={i} className="fy-stack fy-stack--1">
                    <legend>{t("payroll.itemN", { n: String(i + 1) })}</legend>
                    <label className="fy-field"><span>{t("payroll.itemKey", { n: String(i + 1) })}</span><input className="fy-input" required maxLength={40} value={it.key} onChange={(ev) => setItem(i, "key", ev.target.value)} /></label>
                    <label className="fy-field"><span>{t("payroll.itemName", { n: String(i + 1) })}</span><input className="fy-input" required maxLength={80} value={it.name} onChange={(ev) => setItem(i, "name", ev.target.value)} /></label>
                    <label className="fy-field">
                      <span>{t("payroll.itemKind", { n: String(i + 1) })}</span>
                      <select className="fy-input" value={it.kind} onChange={(ev: Change) => setItems(items.map((x, j) => (j === i ? { ...x, kind: ev.target.value as Item["kind"], nature: ev.target.value === "allowance" ? "allowance" : "accommodation", cpf_wages: ev.target.value === "allowance" } : x)))}>
                        <option value="allowance">{t("payroll.allowance")}</option>
                        <option value="fixed_deduction">{t("payroll.fixedDeduction")}</option>
                      </select>
                    </label>
                    <label className="fy-field"><span>{t("payroll.itemAmount", { n: String(i + 1) })}</span><input className="fy-input" inputMode="decimal" required value={it.amount} onChange={(ev) => setItem(i, "amount", ev.target.value)} /></label>
                    {it.kind === "allowance" && <label className="fy-field"><span>{t("payroll.itemWages", { n: String(i + 1) })}</span><input type="checkbox" checked={it.cpf_wages} onChange={(ev) => setItem(i, "cpf_wages", ev.target.checked)} /></label>}
                    <button className="fy-btn fy-btn--secondary" type="button" onClick={() => setItems(items.filter((_, j) => j !== i))}>{t("payroll.removeItem", { n: String(i + 1) })}</button>
                  </fieldset>
                ))}
                <button className="fy-btn fy-btn--secondary" type="button" onClick={() => setItems([...items, blankItem])}>{t("payroll.addItem")}</button>
                <button className="fy-btn fy-btn--primary" type="submit">{t("payroll.savePay")}</button>
              </form>
              <form className="fy-stack fy-stack--2" onSubmit={importYTD} aria-label={t("payroll.ytdTitle")}>
                <h3 className="fy-h3">{t("payroll.ytdTitle")}</h3>
                <p className="fy-secondary">{t("payroll.ytdLead")}</p>
                <label className="fy-field"><span>{t("payroll.ytdYear")}</span><input className="fy-input" type="number" required value={y.year} onChange={(ev) => setY({ ...y, year: ev.target.value })} /></label>
                <label className="fy-field"><span>{t("payroll.ytdOw")}</span><input className="fy-input" inputMode="decimal" required value={y.ow} onChange={(ev) => setY({ ...y, ow: ev.target.value })} /></label>
                <label className="fy-field"><span>{t("payroll.ytdAw")}</span><input className="fy-input" inputMode="decimal" required value={y.aw} onChange={(ev) => setY({ ...y, aw: ev.target.value })} /></label>
                <label className="fy-field"><span>{t("payroll.ytdPaid")}</span><input className="fy-input" inputMode="decimal" required value={y.paid} onChange={(ev) => setY({ ...y, paid: ev.target.value })} /></label>
                <label className="fy-field"><span>{t("payroll.ytdThrough")}</span><input className="fy-input" type="date" required value={y.through} onChange={(ev) => setY({ ...y, through: ev.target.value })} /></label>
                <button className="fy-btn fy-btn--primary" type="submit">{t("payroll.importYtd")}</button>
              </form>
              <form className="fy-stack fy-stack--2" onSubmit={elect} aria-label={t("payroll.electionTitle")}>
                <h3 className="fy-h3">{t("payroll.electionTitle")}</h3>
                <p className="fy-secondary">{t("payroll.electionLead")}</p>
                <label className="fy-field"><span>{t("payroll.from")}</span><input className="fy-input" type="date" required value={e.from} onChange={(ev) => setE({ ...e, from: ev.target.value })} /></label>
                <label className="fy-field">
                  <span>{t("payroll.evidence")}</span>
                  <select className="fy-input" value={e.evidence || docs[0]?.id} onChange={(ev: Change) => setE({ ...e, evidence: ev.target.value })}>
                    {docs.map((d) => <option key={d.id} value={d.id}>{d.title}</option>)}
                  </select>
                </label>
                <button className="fy-btn fy-btn--primary" type="submit">{t("payroll.recordElection")}</button>
              </form>
            </section>
            {pid && <FinalPay person={pid} docs={docs} onChange={loadOneOffs} />}
            <PayRuns month={month} groups={groups} people={people.map((p) => ({ id: p.id, name: p.name }))} />
            <section className="fy-stack fy-stack--2" aria-labelledby="oneoff-h">
              <h2 className="fy-h2" id="oneoff-h">{t("payroll.oneOffs")}</h2>
              <label className="fy-field"><span>{t("payroll.month")}</span><input className="fy-input" type="month" value={month.slice(0, 7)} onChange={(ev) => setMonth(`${ev.target.value}-01`)} /></label>
              {oneOffs.length === 0 && <p className="fy-secondary">{t("payroll.noOneOffs")}</p>}
              <ul className="fy-stack fy-stack--1">{oneOffs.map((x) => <li key={x.id}>{t("payroll.oneOffLine", { person: x.person, name: x.name, amount: x.amount, used: x.used ? t("payroll.used") : t("payroll.waiting") })}</li>)}</ul>
              <form className="fy-stack fy-stack--2" onSubmit={addOneOff} aria-label={t("payroll.addOneOff")}>
                <h3 className="fy-h3">{t("payroll.addOneOff")}</h3>
                <label className="fy-field"><span>{t("payroll.itemKeyLabel")}</span><input className="fy-input" required maxLength={40} value={o.key} onChange={(ev) => setO({ ...o, key: ev.target.value })} /></label>
                <label className="fy-field"><span>{t("payroll.itemNameLabel")}</span><input className="fy-input" required maxLength={80} value={o.name} onChange={(ev) => setO({ ...o, name: ev.target.value })} /></label>
                <label className="fy-field">
                  <span>{t("payroll.nature")}</span>
                  <select className="fy-input" value={o.nature} onChange={(ev: Change) => setO({ ...o, nature: ev.target.value })}>
                    {["bonus", "commission", "allowance", "other", "leave_encashment", "notice_pay", "retrenchment_benefit"].map((n) => <option key={n} value={n}>{t(`payroll.nature.${n}`)}</option>)}
                  </select>
                </label>
                <label className="fy-field"><span>{t("payroll.amount")}</span><input className="fy-input" inputMode="decimal" required value={o.amount} onChange={(ev) => setO({ ...o, amount: ev.target.value })} /></label>
                <label className="fy-field"><span>{t("payroll.cpfWages")}</span><input type="checkbox" checked={o.cpf} onChange={(ev) => setO({ ...o, cpf: ev.target.checked })} /></label>
                <label className="fy-field"><span>{t("payroll.forMonth")}</span><input className="fy-input" type="date" value={o.forMonth} onChange={(ev) => setO({ ...o, forMonth: ev.target.value })} /></label>
                <label className="fy-field"><span>{t("payroll.payableOn")}</span><input className="fy-input" type="date" value={o.payable} onChange={(ev) => setO({ ...o, payable: ev.target.value })} /></label>
                <p className="fy-secondary">{t("payroll.classLead")}</p>
                <button className="fy-btn fy-btn--primary" type="submit">{t("payroll.addOneOffButton")}</button>
              </form>
            </section>
          </>
        )}
      </main>
    </Shell>
  );
}
