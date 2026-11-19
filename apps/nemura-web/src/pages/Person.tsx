import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t, textKeys } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Record = components["schemas"]["Record"];
type State = { status: "loading" } | { status: "missing" } | { status: "ready"; rec: Record };

// value is a coded value in words: the catalogue names each one.
const value = (v: string): string => (textKeys.includes(`value.${v}`) ? t(`value.${v}`) : v);
const yesNo = (b: boolean): string => t(b ? "person.yes" : "person.no");

// Person is nm-person: one person's record with their effective-dated job history and, for the people team, their legal classification (NM-PPL-01, NM-PPL-08).
export function Person({ id, roles }: { id: string; roles: readonly string[] }) {
  const [state, set] = useState<State>({ status: "loading" });
  useEffect(() => {
    let live = true;
    api.GET("/v1/people/{id}", { params: { path: { id } } }).then(
      ({ data }) => live && set(data ? { status: "ready", rec: data } : { status: "missing" }),
      () => live && set({ status: "missing" }),
    );
    return () => {
      live = false;
    };
  }, [id]);
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-person" />
        {state.status === "loading" && <p role="status" className="fy-secondary">{t("person.loading")}</p>}
        {state.status === "missing" && <p role="alert" className="fy-secondary">{t("person.notfound")}</p>}
        {state.status === "ready" && <Record rec={state.rec} />}
        {state.status === "ready" && <a href={`/people/${id}/leave`}>{t("person.leave")}</a>}
        <a href="/people/directory">{t("person.back")}</a>
      </main>
    </Shell>
  );
}

function Record({ rec }: { rec: Record }) {
  const c = rec.classification;
  return (
    <>
      <h1 className="fy-h1" title={helpText("person.page").tooltip}>{rec.preferred_name ? `${rec.legal_name} (${rec.preferred_name})` : rec.legal_name}</h1>
      <section className="fy-stack fy-stack--2" aria-labelledby="contact-h">
        <h2 className="fy-h2" id="contact-h">{t("person.contact")}</h2>
        <dl className="fy-stack fy-stack--1">
          {rec.work_email && <Fact label="person.email" v={rec.work_email} />}
          {rec.phone && <Fact label="person.phone" v={rec.phone} />}
          <Fact label="person.start" v={rec.employment_start} />
          {rec.probation_end && <Fact label="person.probation" v={rec.probation_end} />}
          {rec.leaving_date && <Fact label="person.leaving" v={rec.leaving_date} />}
          {rec.leaving_reason && <Fact label="person.reason" v={rec.leaving_reason} />}
          <Fact label={rec.has_login ? "person.login" : "person.nologin"} v="" />
        </dl>
      </section>
      <section className="fy-stack fy-stack--2" aria-labelledby="jobs-h">
        <h2 className="fy-h2" id="jobs-h">{t("person.jobs")}</h2>
        <table className="fy-table">
          <thead>
            <tr>
              <th scope="col">{t("person.from")}</th>
              <th scope="col">{t("person.to")}</th>
              <th scope="col">{t("person.title")}</th>
              <th scope="col">{t("directory.department")}</th>
              <th scope="col">{t("person.type")}</th>
              <th scope="col">{t("person.worksite")}</th>
            </tr>
          </thead>
          <tbody>
            {(rec.jobs ?? []).map((j) => (
              <tr key={j.effective_from}>
                <td>{j.effective_from}</td>
                <td>{j.effective_to ?? t("person.current")}</td>
                <td>{j.job_title}</td>
                <td>{j.department}</td>
                <td>{value(j.employment_type)}</td>
                <td>{j.worksite}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
      {c && (
        <section className="fy-stack fy-stack--2" aria-labelledby="class-h">
          <h2 className="fy-h2" id="class-h">{t("person.classification")}</h2>
          <dl className="fy-stack fy-stack--1">
            <Fact label="person.contract" v={value(c.contract_type)} />
            <Fact label="person.ea" v={yesNo(c.ea_covered)} />
            <Fact label="person.partiv" v={value(c.part_iv_class)} />
            <Fact label="person.residency" v={value(c.residency)} />
            {c.pr_start_date && <Fact label="person.prstart" v={c.pr_start_date} />}
            {c.birth_date && <Fact label="person.birth" v={c.birth_date} />}
          </dl>
        </section>
      )}
    </>
  );
}

function Fact({ label, v }: { label: string; v: string }) {
  return (
    <div className="fy-row" style={{ gap: "var(--space-3)" }}>
      <dt className="fy-strong">{t(label)}</dt>
      {v && <dd>{v}</dd>}
    </div>
  );
}
