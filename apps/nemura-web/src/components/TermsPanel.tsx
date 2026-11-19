import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { helpText, t } from "../i18n";

type Terms = components["schemas"]["Terms"];
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("terms.failed");
type Change = React.ChangeEvent<HTMLInputElement | HTMLSelectElement>;
const today = (): string => new Date().toISOString().slice(0, 10);

// TermsPanel is the key employment terms on nm-person (NM-LTR-06): the base terms the people team writes, the pay terms only the Payroll administrator and the Owner see and
// write, and the request for the KETs letter. A part the signed-in roles may not write is not drawn as a form.
export function TermsPanel({ person, roles }: { person: string; roles: readonly string[] }) {
  const base = roles.some((r) => ["hr_officer", "hr_admin", "owner"].includes(r));
  const pay = roles.some((r) => ["payroll_admin", "owner"].includes(r));
  const [terms, setTerms] = useState<Terms | null>(null);
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const [form, setForm] = useState({ from: today(), duties: "", notice: "", medical: "", end: "", period: "", salary: "", allowances: "", deductions: "", overtime: "", other: "" });
  const [open, setOpen] = useState(false);
  const load = () =>
    api.GET("/v1/people/{id}/terms", { params: { path: { id: person } } }).then(
      ({ data }) => setTerms(data?.recorded ? data.terms : null),
      () => setTerms(null),
    );
  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [person]);
  const set = (k: keyof typeof form) => (e: Change) => setForm({ ...form, [k]: e.target.value });
  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    const body: { effective_from: string; base?: components["schemas"]["TermsBase"]; pay?: components["schemas"]["TermsPay"] } = { effective_from: form.from };
    if (base) body.base = { main_duties: form.duties, notice_period: form.notice, medical_benefits: form.medical, contract_end: form.end || undefined };
    if (pay)
      body.pay = { salary_period: (form.period || undefined) as components["schemas"]["TermsPay"]["salary_period"], basic_salary: form.salary || undefined, fixed_allowances: form.allowances, fixed_deductions: form.deductions, overtime_period: form.overtime, other_pay: form.other };
    const { error } = await api.POST("/v1/people/{id}/terms", { params: { path: { id: person } }, body });
    if (error) return setNote({ ok: false, text: problem(error) });
    setNote({ ok: true, text: t("terms.saved") });
    setOpen(false);
    load();
  };
  const ask = async () => {
    const { data, error } = await api.POST("/v1/people/{id}/letters", { params: { path: { id: person } }, body: { type: "kets", purpose: t("terms.purpose") } });
    setNote(data ? { ok: true, text: t("terms.asked", { due: data.due_on }) } : { ok: false, text: problem(error) });
  };
  if (!base && !pay) return null;
  return (
    <section className="fy-stack fy-stack--2" aria-labelledby="terms-h" id="terms">
      <h2 className="fy-h2" id="terms-h" title={helpText("terms.page").tooltip}>{t("terms.title")}</h2>
      {terms ? (
        <dl className="fy-stack fy-stack--2">
          <Row label="terms.duties" v={terms.base.main_duties} />
          <Row label="terms.notice" v={terms.base.notice_period} />
          {terms.base.medical_benefits && <Row label="terms.medical" v={terms.base.medical_benefits} />}
          {terms.base.contract_end && <Row label="terms.end" v={terms.base.contract_end} />}
          {terms.pay?.basic_salary && <Row label="terms.salary" v={`${terms.pay.basic_salary} / ${terms.pay.salary_period ?? ""}`} />}
          {terms.pay?.fixed_allowances && <Row label="terms.allowances" v={terms.pay.fixed_allowances} />}
          {terms.pay?.fixed_deductions && <Row label="terms.deductions" v={terms.pay.fixed_deductions} />}
          {terms.pay?.overtime_period && <Row label="terms.overtime" v={terms.pay.overtime_period} />}
          {terms.pay?.other_pay && <Row label="terms.other" v={terms.pay.other_pay} />}
          <Row label="terms.since" v={terms.effective_from} />
        </dl>
      ) : (
        <p className="fy-secondary">{t("terms.none")}</p>
      )}
      <div className="fy-row" style={{ gap: "var(--space-3)" }}>
        <button className="fy-btn fy-btn--secondary" type="button" onClick={() => setOpen(!open)} aria-expanded={open}>{t("terms.record")}</button>
        {base && <button className="fy-btn fy-btn--primary" type="button" onClick={ask}>{t("terms.ask")}</button>}
      </div>
      {open && (
        <form className="fy-stack fy-stack--2" onSubmit={save} aria-label={t("terms.record")}>
          <label className="fy-field"><span>{t("terms.from")}</span><input className="fy-input" type="date" required value={form.from} onChange={set("from")} /></label>
          {base && (
            <>
              <label className="fy-field"><span>{t("terms.duties")}</span><input className="fy-input" required maxLength={1000} value={form.duties} onChange={set("duties")} /></label>
              <label className="fy-field"><span>{t("terms.notice")}</span><input className="fy-input" required maxLength={300} value={form.notice} onChange={set("notice")} /></label>
              <label className="fy-field"><span>{t("terms.medical")}</span><input className="fy-input" maxLength={500} value={form.medical} onChange={set("medical")} /></label>
              <label className="fy-field"><span>{t("terms.end")}</span><input className="fy-input" type="date" value={form.end} onChange={set("end")} /></label>
            </>
          )}
          {pay && (
            <>
              <label className="fy-field">
                <span>{t("terms.period")}</span>
                <select className="fy-input" value={form.period} onChange={set("period")}>
                  <option value="">{t("terms.noperiod")}</option>
                  {["hourly", "daily", "weekly", "fortnightly", "monthly"].map((p) => <option key={p} value={p}>{t(`terms.period.${p}`)}</option>)}
                </select>
              </label>
              <label className="fy-field"><span>{t("terms.salary")}</span><input className="fy-input" inputMode="decimal" pattern="[0-9]{1,14}(\.[0-9]{1,2})?" value={form.salary} onChange={set("salary")} /></label>
              <label className="fy-field"><span>{t("terms.allowances")}</span><input className="fy-input" maxLength={500} value={form.allowances} onChange={set("allowances")} /></label>
              <label className="fy-field"><span>{t("terms.deductions")}</span><input className="fy-input" maxLength={500} value={form.deductions} onChange={set("deductions")} /></label>
              <label className="fy-field"><span>{t("terms.overtime")}</span><input className="fy-input" maxLength={200} value={form.overtime} onChange={set("overtime")} /></label>
              <label className="fy-field"><span>{t("terms.other")}</span><input className="fy-input" maxLength={500} value={form.other} onChange={set("other")} /></label>
            </>
          )}
          <button className="fy-btn fy-btn--primary" type="submit">{t("terms.save")}</button>
        </form>
      )}
      {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
    </section>
  );
}

function Row({ label, v }: { label: string; v: string }) {
  return (
    <div className="fy-row" style={{ gap: "var(--space-3)" }}>
      <dt className="fy-strong">{t(label)}</dt>
      <dd>{v}</dd>
    </div>
  );
}
