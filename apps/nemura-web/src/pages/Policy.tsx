import { useEffect, useState, type FormEvent } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Rules = components["schemas"]["PolicyRules"];
type Finding = components["schemas"]["Finding"];

const list = (s: string): number[] => s.split(",").map((x) => x.trim()).filter((x) => x !== "").map(Number).filter((n) => Number.isFinite(n));

// Policy is nm-leave-policy: the company's entitlements, set against the statute of the pack in force; a policy below it is refused in plain words, naming each rule (NM-PACK-04).
export function Policy({ roles }: { roles: readonly string[] }) {
  const [current, setCurrent] = useState<{ in_force: boolean; rules: Rules } | null>(null);
  const [findings, setFindings] = useState<Finding[]>([]);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const load = () => {
    void api.GET("/v1/policy").then(({ data }) => data && setCurrent({ in_force: data.in_force, rules: data.rules }));
    void api.GET("/v1/checks").then(({ data }) => setFindings(data?.findings ?? []));
  };
  useEffect(load, []);
  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    const text = (k: string): string => String(f.get(k) ?? "");
    setBusy(true);
    setMsg(null);
    try {
      const { error } = await api.PUT("/v1/policy", {
        body: {
          effective_from: text("from"),
          rules: {
            annual_leave_days: list(text("annual")), sick_outpatient_days: list(text("outpatient")), sick_hospital_days: list(text("hospital")),
            childcare_citizen_days: Number(text("childcare")), extended_childcare_days: Number(text("extended")), childcare_non_citizen_days: Number(text("noncitizen")),
          },
        },
      });
      if (error) setMsg({ ok: false, text: (error as { detail?: string }).detail ?? t("packs.failed") });
      else {
        setMsg({ ok: true, text: t("policy.saved") });
        load();
      }
    } catch {
      setMsg({ ok: false, text: t("packs.failed") });
    } finally {
      setBusy(false);
    }
  };
  const r = current?.rules;
  const canSet = admin(roles);
  return (
    <Shell roles={roles} directory={team(roles)} features={canSet}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-leave-policy" />
        <h1 className="fy-h1" title={helpText("policy.page").tooltip}>{t("policy.title")}</h1>
        <p className="fy-secondary">{t("policy.lead")}</p>
        <section aria-labelledby="checks-h" className="fy-stack fy-stack--2">
          <h2 className="fy-h2" id="checks-h">{t("checks.title")}</h2>
          {findings.length === 0 ? <p className="fy-secondary">{t("checks.none")}</p> : <ul className="fy-stack fy-stack--1">{findings.map((x, i) => <li key={i}>{x.message}</li>)}</ul>}
        </section>
        {current && !current.in_force && <p className="fy-secondary">{t("policy.none")}</p>}
        <form className="fy-stack fy-stack--3" onSubmit={(e) => void submit(e)} key={JSON.stringify(r)}>
          <Field id="pol-from" name="from" label="policy.from" type="date" defaultValue="" />
          <Field id="pol-annual" name="annual" label="policy.annual" help="policy.annual.help" defaultValue={(r?.annual_leave_days ?? []).join(", ")} />
          <Field id="pol-out" name="outpatient" label="policy.outpatient" defaultValue={(r?.sick_outpatient_days ?? []).join(", ")} />
          <Field id="pol-hosp" name="hospital" label="policy.hospital" defaultValue={(r?.sick_hospital_days ?? []).join(", ")} />
          <Field id="pol-child" name="childcare" label="policy.childcare" type="number" defaultValue={String(r?.childcare_citizen_days ?? "")} />
          <Field id="pol-ext" name="extended" label="policy.extended" type="number" defaultValue={String(r?.extended_childcare_days ?? "")} />
          <Field id="pol-non" name="noncitizen" label="policy.noncitizen" type="number" defaultValue={String(r?.childcare_non_citizen_days ?? "")} />
          {msg && <p role={msg.ok ? "status" : "alert"} className="fy-secondary">{msg.text}</p>}
          {canSet && <button className="fy-btn fy-btn--primary" type="submit" disabled={busy}>{busy ? t("policy.saving") : t("policy.submit")}</button>}
        </form>
      </main>
    </Shell>
  );
}

function Field({ id, name, label, help, type = "text", defaultValue }: { id: string; name: string; label: string; help?: string; type?: string; defaultValue: string }) {
  return (
    <div className="fy-field">
      <label className="fy-label" htmlFor={id}>{t(label)}</label>
      <input className="fy-input" id={id} name={name} type={type} defaultValue={defaultValue} required aria-describedby={help ? `${id}-help` : undefined} />
      {help && <span className="fy-help" id={`${id}-help`}>{t(help)}</span>}
    </div>
  );
}
