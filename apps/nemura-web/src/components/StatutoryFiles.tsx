import { useState } from "react";
import { api } from "../api/client";
import { confirmWithPasskey } from "../api/stepup";
import { t } from "../i18n";

type Change = React.ChangeEvent<HTMLInputElement | HTMLSelectElement>;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("statutory.failed");

// StatutoryFiles makes the two filings an approved version leads to: the CPF EZPay file, with the employer's own CPF Submission Number parts and advice code, and the IR8A payload of
// the income year the run is paid in, with who is authorised to file and how IRAS reaches them (NM-PAY-13, NM-PAY-14). Nothing is guessed: the details are asked for, each file needs a fresh
// passkey as it holds NRICs, and a person left out is listed with why. A file is never proof that anything was submitted.
export function StatutoryFiles({ run, version, onDone }: { run: string; version: number; onDone: (n: { ok: boolean; text: string }) => void }) {
  const [cpf, setCpf] = useState({ payment_type: "", serial: "01", advice_code: "01" });
  const [ais, setAis] = useState({ authorised_person: "", contact_number: "", email: "", submission_type: "O" });
  const make = async (kind: "cpf" | "ais", ev: React.FormEvent) => {
    ev.preventDefault();
    const failed = await confirmWithPasskey().catch(() => "");
    if (failed !== undefined) return onDone({ ok: false, text: failed || t("letters.passkeyFailed") });
    const body = kind === "cpf" ? { version, kind, ...cpf } : { version, kind, ...ais, submission_type: ais.submission_type as "O" | "A" | "R" };
    const { data, error } = await api.POST("/v1/payroll/runs/{id}/exports", { params: { path: { id: run } }, body });
    if (!data) return onDone({ ok: false, text: problem(error) });
    const bytes = Uint8Array.from(atob(data.content_base64), (c) => c.charCodeAt(0));
    const link = document.createElement("a");
    link.href = URL.createObjectURL(new Blob([bytes], { type: kind === "ais" ? "application/json" : "text/plain" }));
    link.download = data.file_name || `${kind}-${data.wage_month.slice(0, 7)}-v${data.version}`;
    link.click();
    const left = (data.skipped ?? []).map((k) => `${k.person}: ${k.reason}`).join(" ");
    onDone({ ok: true, text: t("exports.done", { lines: String(data.lines), sha: data.sha256.slice(0, 12), left }) });
  };
  return (
    <section className="fy-stack fy-stack--3" aria-labelledby="statutory-h">
      <h2 className="fy-h2" id="statutory-h">{t("statutory.title")}</h2>
      <p className="fy-secondary">{t("statutory.lead")}</p>
      <form className="fy-stack fy-stack--2" aria-label={t("statutory.cpf.title")} onSubmit={(e) => void make("cpf", e)}>
        <h3 className="fy-h3">{t("statutory.cpf.title")}</h3>
        <label className="fy-field">
          <span className="fy-label">{t("statutory.cpf.type")}</span>
          <input className="fy-input" required pattern="[A-Za-z0-9]{3}" maxLength={3} value={cpf.payment_type} onChange={(e: Change) => setCpf({ ...cpf, payment_type: e.target.value })} />
        </label>
        <label className="fy-field">
          <span className="fy-label">{t("statutory.cpf.serial")}</span>
          <input className="fy-input" required pattern="(0[1-9]|[1-9][0-9])" maxLength={2} value={cpf.serial} onChange={(e: Change) => setCpf({ ...cpf, serial: e.target.value })} />
        </label>
        <label className="fy-field">
          <span className="fy-label">{t("statutory.cpf.advice")}</span>
          <input className="fy-input" required pattern="(0[1-9]|[1-9][0-9])" maxLength={2} value={cpf.advice_code} onChange={(e: Change) => setCpf({ ...cpf, advice_code: e.target.value })} />
        </label>
        <button className="fy-btn fy-btn--secondary" type="submit">{t("statutory.cpf.make")}</button>
      </form>
      <form className="fy-stack fy-stack--2" aria-label={t("statutory.ais.title")} onSubmit={(e) => void make("ais", e)}>
        <h3 className="fy-h3">{t("statutory.ais.title")}</h3>
        <label className="fy-field">
          <span className="fy-label">{t("statutory.ais.person")}</span>
          <input className="fy-input" required maxLength={100} value={ais.authorised_person} onChange={(e: Change) => setAis({ ...ais, authorised_person: e.target.value })} />
        </label>
        <label className="fy-field">
          <span className="fy-label">{t("statutory.ais.phone")}</span>
          <input className="fy-input" required inputMode="numeric" pattern="[0-9]{1,12}" value={ais.contact_number} onChange={(e: Change) => setAis({ ...ais, contact_number: e.target.value })} />
        </label>
        <label className="fy-field">
          <span className="fy-label">{t("statutory.ais.email")}</span>
          <input className="fy-input" required type="email" value={ais.email} onChange={(e: Change) => setAis({ ...ais, email: e.target.value })} />
        </label>
        <label className="fy-field">
          <span className="fy-label">{t("statutory.ais.type")}</span>
          <select className="fy-input" value={ais.submission_type} onChange={(e: Change) => setAis({ ...ais, submission_type: e.target.value })}>
            <option value="O">{t("statutory.ais.type.O")}</option>
            <option value="A">{t("statutory.ais.type.A")}</option>
            <option value="R">{t("statutory.ais.type.R")}</option>
          </select>
        </label>
        <button className="fy-btn fy-btn--secondary" type="submit">{t("statutory.ais.make")}</button>
      </form>
    </section>
  );
}
