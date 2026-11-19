import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Summary = components["schemas"]["PayslipSummary"];
type View = components["schemas"]["PayslipView"];

// pdf shows a payslip's PDF as base64 in a new tab, without a file on a disk.
export async function openPdf(id: string): Promise<boolean> {
  const { data } = await api.GET("/v1/me/payslips/{id}/file", { params: { path: { id } } });
  if (!data) return false;
  const bytes = Uint8Array.from(atob(data.content_base64), (c) => c.charCodeAt(0));
  window.open(URL.createObjectURL(new Blob([bytes], { type: "application/pdf" })), "_blank", "noopener");
  return true;
}

// MyPay is nm-my-pay: a person's payslips by month, each with its net salary and a view, a print and a PDF (NM-PAY-04, NM-ESS-02). Only their own; never the company's figures.
export function MyPay({ roles }: { roles: readonly string[] }) {
  const [list, setList] = useState<Summary[] | null>(null);
  const [note, setNote] = useState<string | null>(null);
  useEffect(() => {
    api.GET("/v1/me/payslips").then(
      ({ data }) => setList(data?.payslips ?? []),
      () => setList([]),
    );
  }, []);
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-my-pay" />
        <h1 className="fy-h1" title={helpText("mypay.page").tooltip}>{t("mypay.title")}</h1>
        <p className="fy-secondary">{t("mypay.lead")}</p>
        {list !== null && list.length === 0 && <p className="fy-secondary">{t("mypay.none")}</p>}
        <ul className="fy-stack fy-stack--2">
          {(list ?? []).map((p) => (
            <li key={p.id} className="fy-row" style={{ gap: "var(--space-3)" }}>
              <a href={`/me/pay/${p.id}`}>{t("mypay.line", { month: p.month.slice(0, 7), net: p.net_salary, date: p.payment_date })}</a>
              {p.corrected && <span className="fy-chip">{t("mypay.corrected")}</span>}
              <button className="fy-btn fy-btn--secondary" type="button" onClick={async () => setNote((await openPdf(p.id)) ? null : t("mypay.failed"))}>{t("mypay.pdf", { month: p.month.slice(0, 7) })}</button>
            </li>
          ))}
        </ul>
        {note && <p role="alert" className="fy-secondary">{note}</p>}
      </main>
    </Shell>
  );
}

// Payslip is nm-payslip: one payslip with MOM's twelve items, a print view and the PDF (NM-PAY-04).
export function Payslip({ id, roles }: { id: string; roles: readonly string[] }) {
  const [v, setV] = useState<View | null | undefined>(undefined);
  useEffect(() => {
    api.GET("/v1/me/payslips/{id}", { params: { path: { id } } }).then(
      ({ data }) => setV(data ?? null),
      () => setV(null),
    );
  }, [id]);
  const row = (label: string, value: string) => (
    <div className="fy-row" style={{ gap: "var(--space-3)" }}>
      <dt className="fy-strong">{label}</dt>
      <dd>{value}</dd>
    </div>
  );
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-payslip" />
        {v === undefined && <p role="status" className="fy-secondary">{t("mypay.loading")}</p>}
        {v === null && <p role="alert" className="fy-secondary">{t("mypay.notfound")}</p>}
        {v && (
          <>
            <h1 className="fy-h1" title={helpText("mypay.page").tooltip}>{t("payslip.title", { month: v.month.slice(0, 7) })}</h1>
            <dl className="fy-stack fy-stack--1">
              {row(t("payslip.employer"), v.employer)}
              {row(t("payslip.employee"), v.employee)}
              {row(t("payslip.paid"), v.payment_date)}
              {row(t("payslip.period"), `${v.period_start} ${t("payslip.to")} ${v.period_end}`)}
              {row(t("payslip.basic"), v.basic_salary)}
              {(v.allowances ?? []).map((a) => row(t("payslip.allowance", { name: a.name }), a.amount))}
              {(v.additional_payments ?? []).map((a) => row(t("payslip.additional", { name: a.name }), a.amount))}
              {v.overtime_pay && v.overtime_hours && row(t("payslip.otHours"), v.overtime_hours)}
              {v.overtime_pay && row(t("payslip.otPay"), v.overtime_pay)}
              {v.overtime_period && row(t("payslip.otPeriod"), v.overtime_period)}
              {(v.deductions ?? []).map((a) => row(t("payslip.deduction", { name: a.name }), a.amount))}
              {row(t("payslip.net"), v.net_salary)}
              {v.reimbursements && row(t("payslip.reimbursements"), v.reimbursements)}
              {row(t("payslip.reference"), v.reference)}
            </dl>
            {v.corrected && <p className="fy-secondary">{t("payslip.corrected")}</p>}
            <div className="fy-row" style={{ gap: "var(--space-3)" }}>
              <button className="fy-btn fy-btn--secondary" type="button" onClick={() => window.print()}>{t("payslip.print")}</button>
              <button className="fy-btn fy-btn--primary" type="button" onClick={() => void openPdf(id)}>{t("payslip.pdf")}</button>
            </div>
          </>
        )}
        <a href="/me/pay">{t("payslip.back")}</a>
      </main>
    </Shell>
  );
}
