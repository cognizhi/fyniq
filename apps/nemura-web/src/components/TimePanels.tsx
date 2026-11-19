import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { t } from "../i18n";

type Overtime = components["schemas"]["OvertimeRecord"];
type Input = components["schemas"]["PayInput"];
type Sick = components["schemas"]["SickNotification"];
type Line = components["schemas"]["AccrualLine"];
type Note = { ok: boolean; text: string } | null;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("team.failed");
const kinds = ["overtime", "rest_day", "public_holiday"] as const;
const branches = ["employer_request", "employee_request"] as const;

// The people team's panels for hours worked, pay inputs, sick days beyond the limit and the month-end accrual (NM-TIM-04, NM-LV-08, NM-LV-09). The API decides who may see
// or do each; a panel the caller may not use shows the API's refusal in words.
export function OvertimeApprovals() {
  const [list, setList] = useState<Overtime[]>([]);
  const [note, setNote] = useState<Note>(null);
  const [reasons, setReasons] = useState<Record<string, string>>({});
  const load = async () => {
    try {
      const { data } = await api.GET("/v1/overtime", { params: { query: { state: "pending" } } });
      setList(data?.records ?? []);
    } catch {
      setList([]);
    }
  };
  useEffect(() => {
    void load();
  }, []);
  const decide = async (r: Overtime, decision: "approve" | "refuse") => {
    const { error } = await api.POST("/v1/overtime/{id}/decision", { params: { path: { id: r.id } }, body: { decision, reason: reasons[r.id] ?? "" } });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t(decision === "approve" ? "otteam.approved" : "otteam.refused") });
    if (!error) await load();
  };
  return (
    <section className="fy-stack fy-stack--2" aria-labelledby="otteam-h">
      <h2 className="fy-h2" id="otteam-h">{t("otteam.title")}</h2>
      {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
      {list.length === 0 ? <p className="fy-secondary">{t("otteam.none")}</p> : (
        <ul className="fy-stack fy-stack--2">
          {list.map((r) => (
            <li key={r.id} className="fy-row" style={{ gap: "var(--space-2)", flexWrap: "wrap" }}>
              <span>{t("otteam.line", { name: r.person_name, date: r.date, kind: t(`ot.kind.${r.kind}`), hours: r.hours })} {r.over_limit && <strong>{t("ot.overFlag")}</strong>}</span>
              <button className="fy-btn fy-btn--primary" type="button" onClick={() => decide(r, "approve")}>{t("otteam.approve")}</button>
              <input className="fy-input" aria-label={t("otteam.reasonFor", { name: r.person_name, date: r.date })} value={reasons[r.id] ?? ""} maxLength={500} onChange={(e) => setReasons({ ...reasons, [r.id]: e.target.value })} />
              <button className="fy-btn fy-btn--secondary" type="button" onClick={() => decide(r, "refuse")}>{t("otteam.refuse")}</button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

export function PayInputs() {
  const [list, setList] = useState<Input[]>([]);
  useEffect(() => {
    api.GET("/v1/pay-inputs").then(({ data }) => setList(data?.inputs ?? []), () => setList([]));
  }, []);
  return (
    <section className="fy-stack fy-stack--2" aria-labelledby="inputs-h">
      <h2 className="fy-h2" id="inputs-h">{t("inputs.title")}</h2>
      {list.length === 0 ? <p className="fy-secondary">{t("inputs.none")}</p> : (
        <ul className="fy-stack fy-stack--1">
          {list.map((i) => (
            <li key={i.id}>{t("inputs.line", { name: i.person_name, kind: t(`inputs.kind.${i.kind}`), quantity: i.quantity, unit: t(`inputs.unit.${i.unit}`), from: i.from, to: i.to })}</li>
          ))}
        </ul>
      )}
    </section>
  );
}

export function UnpaidSickDays() {
  const [list, setList] = useState<Sick[]>([]);
  const [note, setNote] = useState<Note>(null);
  const load = async () => {
    try {
      const { data } = await api.GET("/v1/sick-leave");
      setList((data?.notifications ?? []).filter((n) => n.unpaid_days !== "0.00" && !n.unpaid_decision));
    } catch {
      setList([]);
    }
  };
  useEffect(() => {
    void load();
  }, []);
  const decide = async (n: Sick, treatment: "unpaid" | "annual") => {
    const { error } = await api.POST("/v1/sick-leave/{id}/unpaid-decision", { params: { path: { id: n.id } }, body: { treatment } });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t("unpaid.saved") });
    if (!error) await load();
  };
  return (
    <section className="fy-stack fy-stack--2" aria-labelledby="unpaid-h">
      <h2 className="fy-h2" id="unpaid-h">{t("unpaid.title")}</h2>
      {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
      {list.length === 0 ? <p className="fy-secondary">{t("unpaid.none")}</p> : (
        <ul className="fy-stack fy-stack--2">
          {list.map((n) => (
            <li key={n.id} className="fy-row" style={{ gap: "var(--space-2)", flexWrap: "wrap" }}>
              <span>{t("unpaid.line", { name: n.person_name, from: n.from, to: n.to, days: n.unpaid_days })}</span>
              <button className="fy-btn fy-btn--secondary" type="button" onClick={() => decide(n, "unpaid")}>{t("unpaid.unpaid")}</button>
              <button className="fy-btn fy-btn--secondary" type="button" onClick={() => decide(n, "annual")}>{t("unpaid.annual")}</button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

const thisMonth = (): string => {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
};

export function Accrual({ writes }: { writes: boolean }) {
  const [month, setMonth] = useState(thisMonth());
  const [lines, setLines] = useState<Line[]>([]);
  const [note, setNote] = useState<Note>(null);
  const load = async () => {
    try {
      const { data } = await api.GET("/v1/leave/accruals", { params: { query: { month } } });
      setLines(data?.lines ?? []);
    } catch {
      setLines([]);
    }
  };
  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [month]);
  const act = async (path: "/v1/leave/accruals" | "/v1/leave/accruals/approval", ok: string) => {
    const { error } = await api.POST(path, { body: { month } });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: ok });
    if (!error) await load();
  };
  return (
    <section className="fy-stack fy-stack--2" aria-labelledby="accrual-h">
      <h2 className="fy-h2" id="accrual-h">{t("accrual.title")}</h2>
      <p className="fy-secondary">{t("accrual.lead")}</p>
      <label className="fy-field">
        <span>{t("accrual.month")}</span>
        <input className="fy-input" type="month" value={month} onChange={(e) => setMonth(e.target.value)} />
      </label>
      {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
      {lines.length === 0 ? <p className="fy-secondary">{t("accrual.none")}</p> : (
        <ul className="fy-stack fy-stack--1">
          {lines.map((l) => (
            <li key={l.id}>{t("accrual.line", { name: l.person_name, carried: l.carried_days, left: l.annual_left, state: t(`accrual.state.${l.state}`) })}</li>
          ))}
        </ul>
      )}
      {writes && (
        <div className="fy-row" style={{ gap: "var(--space-2)" }}>
          <button className="fy-btn fy-btn--secondary" type="button" onClick={() => act("/v1/leave/accruals", t("accrual.drafted"))}>{t("accrual.draft")}</button>
          <button className="fy-btn fy-btn--primary" type="button" onClick={() => act("/v1/leave/accruals/approval", t("accrual.approved"))}>{t("accrual.approve")}</button>
        </div>
      )}
    </section>
  );
}

// HoursWorked is the signed-in person's own hours on special days: the list and the form to enter a day (NM-TIM-04).
export function HoursWorked() {
  const [list, setList] = useState<Overtime[]>([]);
  const [note, setNote] = useState<Note>(null);
  const [date, setDate] = useState("");
  const [kind, setKind] = useState<(typeof kinds)[number]>("overtime");
  const [hours, setHours] = useState("");
  const [branch, setBranch] = useState<(typeof branches)[number]>("employer_request");
  const [text, setText] = useState("");
  const load = async () => {
    try {
      const { data } = await api.GET("/v1/overtime", { params: { query: { mine: true } } });
      setList(data?.records ?? []);
    } catch {
      setList([]);
    }
  };
  useEffect(() => {
    void load();
  }, []);
  const send = async (e: React.FormEvent) => {
    e.preventDefault();
    const { data, error } = await api.POST("/v1/me/overtime", { body: { date, kind, hours, branch: kind === "rest_day" ? branch : undefined, note: text } });
    setNote(data ? { ok: true, text: data.over_limit ? t("ot.over") : t("ot.saved") } : { ok: false, text: problem(error) });
    if (data) await load();
  };
  return (
    <section className="fy-stack fy-stack--2" aria-labelledby="ot-h">
      <h2 className="fy-h2" id="ot-h">{t("ot.title")}</h2>
      <p className="fy-secondary">{t("ot.lead")}</p>
      {list.length === 0 ? <p className="fy-secondary">{t("ot.none")}</p> : (
        <ul className="fy-stack fy-stack--1">
          {list.map((r) => (
            <li key={r.id}>{t("ot.line", { date: r.date, kind: t(`ot.kind.${r.kind}`), hours: r.hours, state: t(`state.${r.state === "refused" ? "refused" : r.state === "approved" ? "approved" : "pending"}`) })}{r.branch ? ` ${t(`ot.branch.${r.branch}`)}` : ""}</li>
          ))}
        </ul>
      )}
      <form className="fy-stack fy-stack--2" onSubmit={send} aria-labelledby="ot-h">
        <label className="fy-field">
          <span>{t("ot.date")}</span>
          <input className="fy-input" type="date" required value={date} onChange={(e) => setDate(e.target.value)} />
        </label>
        <label className="fy-field">
          <span>{t("ot.kind")}</span>
          <select className="fy-input" value={kind} onChange={(e) => setKind(e.target.value as (typeof kinds)[number])}>
            {kinds.map((k) => (
              <option key={k} value={k}>{t(`ot.kind.${k}`)}</option>
            ))}
          </select>
        </label>
        {kind === "rest_day" && (
          <label className="fy-field">
            <span>{t("ot.branch")}</span>
            <select className="fy-input" value={branch} onChange={(e) => setBranch(e.target.value as (typeof branches)[number])}>
              {branches.map((b) => (
                <option key={b} value={b}>{t(`ot.branch.${b}`)}</option>
              ))}
            </select>
          </label>
        )}
        <label className="fy-field">
          <span>{t("ot.hours")}</span>
          <input className="fy-input" inputMode="decimal" required value={hours} onChange={(e) => setHours(e.target.value)} />
        </label>
        <label className="fy-field">
          <span>{t("ot.note")}</span>
          <textarea className="fy-input" maxLength={500} value={text} onChange={(e) => setText(e.target.value)} />
        </label>
        <button className="fy-btn fy-btn--primary" type="submit">{t("ot.send")}</button>
      </form>
      {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
    </section>
  );
}

// Encashment pays out days of a person's annual leave for a leave year (NM-LV-08); the people team only, and the API refuses more than the balance.
export function Encashment({ person, year, saved }: { person: string; year: number; saved: () => unknown }) {
  const [days, setDays] = useState("");
  const [on, setOn] = useState("");
  const [why, setWhy] = useState("");
  const [note, setNote] = useState<Note>(null);
  const send = async (e: React.FormEvent) => {
    e.preventDefault();
    const { error } = await api.POST("/v1/people/{id}/leave/{year}/encashment", { params: { path: { id: person, year } }, body: { days, on, note: why } });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t("encash.saved") });
    if (!error) await saved();
  };
  return (
    <form className="fy-stack fy-stack--2" onSubmit={send} aria-labelledby="encash-h">
      <h3 className="fy-h3" id="encash-h">{t("encash.title")}</h3>
      <label className="fy-field">
        <span>{t("encash.days")}</span>
        <input className="fy-input" inputMode="decimal" required value={days} onChange={(e) => setDays(e.target.value)} />
      </label>
      <label className="fy-field">
        <span>{t("encash.on")}</span>
        <input className="fy-input" type="date" required value={on} onChange={(e) => setOn(e.target.value)} />
      </label>
      <label className="fy-field">
        <span>{t("encash.note")}</span>
        <input className="fy-input" maxLength={500} value={why} onChange={(e) => setWhy(e.target.value)} />
      </label>
      <button className="fy-btn fy-btn--secondary" type="submit">{t("encash.send")}</button>
      {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
    </form>
  );
}
