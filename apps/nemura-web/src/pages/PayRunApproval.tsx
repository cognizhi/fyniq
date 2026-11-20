import { useEffect, useState } from "react";
import { api } from "../api/client";
import { confirmWithPasskey, signApprovalEnvelope } from "../api/stepup";
import type { components } from "../api/schema";
import { PayPayments } from "../components/PayPayments";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Detail = components["schemas"]["RunDetail"];
type Changes = components["schemas"]["RunChanges"];
type Envelope = components["schemas"]["RunApprovalEnvelope"];
type Submission = components["schemas"]["RunSubmissionView"];
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("approval.failed");

// PayRunApproval is nm-payrun-approval: the run's newest version for the approver to read and approve (FR-PAY-07, NM-ROLE-04, NM-PAY-11): its totals and digest, every ✖ and ⚠, every override with
// its reason, the run against last month's with each difference explained by its cause and what has no explanation listed, and each person's month with its working. The approver confirms with a
// passkey over the digest they read; the person who prepared the version cannot approve it, and says so here instead of drawing a button that would be refused.
export function PayRunApproval({ id, roles, member }: { id: string; roles: readonly string[]; member: string }) {
  const [run, setRun] = useState<Detail | null>(null);
  const [changes, setChanges] = useState<Changes | null>(null);
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const [env, setEnv] = useState<Envelope | null>(null);
  const [envAsked, setEnvAsked] = useState(false); // the plain approval waits until the page knows whether the apps are linked
  const [sub, setSub] = useState<Submission | null>(null);
  const load = () => {
    api.GET("/v1/payroll/runs/{id}", { params: { path: { id } } }).then(({ data }) => setRun(data ?? null), () => setRun(null));
    api.GET("/v1/payroll/runs/{id}/changes", { params: { path: { id } } }).then(({ data }) => setChanges(data ?? null), () => setChanges(null));
    api.GET("/v1/payroll/runs/{id}/submission", { params: { path: { id } } }).then(({ data }) => setSub(data ?? null), () => setSub(null));
  };
  useEffect(load, [id]); // eslint-disable-line react-hooks/exhaustive-deps
  const allowed = roles.some((r) => r === "payroll_admin" || r === "owner");
  const v = run?.latest;
  // While linked to Fyniq the approver reads the approval envelope, the figures Fyniq will post, and signs its digest (NM-INT-06); the page asks for it once the newest version is known.
  const calculatedVersion = v?.state === "calculated" ? v.version : 0;
  useEffect(() => {
    setEnv(null);
    setEnvAsked(false);
    if (!calculatedVersion) return;
    const done = (e: Envelope | null) => {
      setEnv(e);
      setEnvAsked(true);
    };
    api.GET("/v1/payroll/runs/{id}/approval-envelope", { params: { path: { id }, query: { version: calculatedVersion } } }).then(({ data }) => done(data ?? null), () => done(null));
  }, [id, calculatedVersion]);
  const linked = !!env?.linked;
  const approvedNumber = (run?.versions ?? []).find((x) => x.state === "approved")?.version ?? 0;
  const hasApproved = approvedNumber > 0;
  const mine = !!v && v.prepared_by === member;
  const flagged = (run?.lines ?? []).flatMap((l) => (l.flags ?? []).map((f) => ({ key: l.person_id + f.code + f.message, fail: f.fail, message: `${l.person}: ${f.message}` })));
  const approve = async () => {
    if (!v) return;
    const failed = await confirmWithPasskey().catch(() => "");
    if (failed !== undefined) return setNote({ ok: false, text: failed || t("letters.passkeyFailed") });
    const { error } = await api.POST("/v1/payroll/runs/{id}/approval", { params: { path: { id } }, body: { version: v.version, digest: v.digest } });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t("approval.done") });
    if (!error) load();
  };
  const approveWithProof = async () => {
    if (!v || !env?.ready || !env.challenge || !env.envelope_sha256) return;
    const failed = await confirmWithPasskey().catch(() => "");
    if (failed !== undefined) return setNote({ ok: false, text: failed || t("letters.passkeyFailed") });
    const proof = await signApprovalEnvelope(env.challenge, env.rp_id).catch(() => "");
    if (typeof proof === "string") return setNote({ ok: false, text: proof || t("approval.linked.signFailed") });
    const { error } = await api.POST("/v1/payroll/runs/{id}/linked-approval", { params: { path: { id } }, body: { version: v.version, digest: v.digest, envelope_sha256: env.envelope_sha256, approval: proof } });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t("approval.linked.done") });
    if (!error) load();
  };
  const sendAgain = async () => {
    if (!sub) return;
    const { error } = await api.POST("/v1/payroll/runs/{id}/submission", { params: { path: { id } }, body: { version: sub.version } });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t("approval.linked.sentAgain") });
    if (!error) load();
  };
  const publish = async () => {
    if (!v) return;
    const { data, error } = await api.POST("/v1/payroll/runs/{id}/payslips", { params: { path: { id } }, body: { version: v.version } });
    setNote(data ? { ok: true, text: t("approval.published", { n: String(data.published) }) } : { ok: false, text: problem(error) });
  };
  const makeFile = async (kind: "journal" | "payments") => {
    if (!v) return;
    if (kind === "payments") {
      const failed = await confirmWithPasskey().catch(() => "");
      if (failed !== undefined) return setNote({ ok: false, text: failed || t("letters.passkeyFailed") });
    }
    const { data, error } = await api.POST("/v1/payroll/runs/{id}/exports", { params: { path: { id } }, body: { version: approvedNumber, kind } });
    if (!data) return setNote({ ok: false, text: problem(error) });
    const bytes = Uint8Array.from(atob(data.content_base64), (c) => c.charCodeAt(0));
    const link = document.createElement("a");
    link.href = URL.createObjectURL(new Blob([bytes], { type: "text/csv" }));
    link.download = `${kind}-${data.wage_month.slice(0, 7)}-v${data.version}.csv`;
    link.click();
    const left = (data.skipped ?? []).map((k) => `${k.person}: ${k.reason}`).join(" ");
    setNote({ ok: true, text: t("exports.done", { lines: String(data.lines), sha: data.sha256.slice(0, 12), left }) });
  };
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-payrun-approval" />
        <h1 className="fy-h1" title={helpText("approval.page").tooltip}>{t("approval.title")}</h1>
        {!allowed && <p role="alert" className="fy-secondary">{t("payroll.denied")}</p>}
        {allowed && !run && <p role="status" className="fy-secondary">{t("approval.loading")}</p>}
        {allowed && run && v && (
          <>
            <p>{t("approval.run", { group: run.pay_group, month: run.month, version: String(v.version), state: t(`runs.state.${v.state}`) })}</p>
            <p>{t("approval.digest", { digest: v.digest })}</p>
            <p>{t("runs.totals", { gross: v.totals.gross, ee: v.totals.cpf_employee, er: v.totals.cpf_employer, sdl: v.totals.sdl_payable, net: v.totals.net, cost: v.totals.employer_cost })}</p>
            {v.reason && <p>{t("approval.reason", { reason: v.reason })}</p>}
            <section className="fy-stack fy-stack--2" aria-labelledby="flags-h">
              <h2 className="fy-h2" id="flags-h">{t("approval.flags")}</h2>
              {flagged.length === 0 && <p className="fy-secondary">{t("approval.noFlags")}</p>}
              <ul className="fy-stack fy-stack--1">
                {flagged.map((x) => <li key={x.key}>{t(x.fail ? "runs.fail" : "runs.warn", { message: x.message })}</li>)}
              </ul>
            </section>
            {(run.overrides ?? []).length > 0 && (
              <section className="fy-stack fy-stack--2" aria-labelledby="ov-h">
                <h2 className="fy-h2" id="ov-h">{t("approval.overrides")}</h2>
                <ul className="fy-stack fy-stack--1">{(run.overrides ?? []).map((x) => <li key={x.id}>{t("runs.overrideLine", { person: x.person, field: x.field, value: x.value, reason: x.reason })}</li>)}</ul>
              </section>
            )}
            <section className="fy-stack fy-stack--2" aria-labelledby="chg-h">
              <h2 className="fy-h2" id="chg-h">{t("approval.changes")}</h2>
              {changes && !changes.has_previous && <p className="fy-secondary">{t("approval.noPrevious")}</p>}
              {changes?.has_previous && (
                <>
                  <p>{t("approval.compare", { gross: changes.gross_now ?? "", grossPrev: changes.gross_previous ?? "", net: changes.net_now ?? "", netPrev: changes.net_previous ?? "", cost: changes.employer_cost_now ?? "", costPrev: changes.employer_cost_previous ?? "" })}</p>
                  <ul className="fy-stack fy-stack--1">{(changes.changes ?? []).map((c) => <li key={c.person + c.cause + (c.item ?? "")}>{t("approval.change", { person: c.person, cause: t(`approval.cause.${c.cause}`), amount: c.amount, item: c.item ?? "" })}</li>)}</ul>
                  {(changes.unexplained ?? []).length > 0 && (
                    <>
                      <h3 className="fy-h3">{t("approval.unexplained")}</h3>
                      <ul className="fy-stack fy-stack--1">{(changes.unexplained ?? []).map((u) => <li key={u.person_id}>{u.person}: {u.why}</li>)}</ul>
                    </>
                  )}
                </>
              )}
            </section>
            <section className="fy-stack fy-stack--2" aria-labelledby="lines-h">
              <h2 className="fy-h2" id="lines-h">{t("approval.lines")}</h2>
              {(run.lines ?? []).map((l) => (
                <details key={l.person_id}>
                  <summary>{t("approval.lineSummary", { person: l.person, gross: l.gross, net: l.net })}</summary>
                  <ul className="fy-stack fy-stack--1">{(l.items ?? []).map((it) => <li key={it.key}>{t("approval.item", { name: it.name, amount: it.amount, class: t(`approval.class.${it.class}`) })}</li>)}</ul>
                  <ol className="fy-stack fy-stack--1">{(l.working ?? []).map((w, i) => <li key={i}>{w.rule}: {w.text}</li>)}</ol>
                </details>
              ))}
            </section>
            {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
            {v.state === "calculated" && mine && <p role="status" className="fy-secondary">{t("approval.mine")}</p>}
            {v.state === "calculated" && !mine && v.flags_failing > 0 && <p role="alert" className="fy-secondary">{t("approval.failing", { n: String(v.flags_failing) })}</p>}
            {v.state === "calculated" && !mine && v.flags_failing === 0 && linked && env && (
              <section className="fy-stack fy-stack--2" aria-labelledby="proof-h">
                <h2 className="fy-h2" id="proof-h" title={helpText("approval.proof").tooltip}>{t("approval.linked.title")}</h2>
                <p className="fy-secondary">{t("approval.linked.lead")}</p>
                {(env.issues ?? []).length > 0 && <ul role="alert" className="fy-stack fy-stack--1">{(env.issues ?? []).map((x) => <li key={x}>{x}</li>)}</ul>}
                {env.ready && (
                  <>
                    <p>{t("approval.linked.mapping", { version: String(env.envelope.mapping_version), month: env.envelope.wage_month })}</p>
                    <ul className="fy-stack fy-stack--1">{(env.envelope.departments ?? []).map((d) => <li key={d.department_id}>{t("approval.linked.department", { id: d.department_id.slice(0, 8), gross: d.gross })}</li>)}</ul>
                    <p>{t("approval.linked.totals", { ee: env.envelope.employee_cpf, er: env.envelope.employer_cpf, sdl: env.envelope.sdl, shg: env.envelope.shg, reimbursements: env.envelope.reimbursements })}</p>
                    {(env.envelope.deductions ?? []).length > 0 && <p>{t("approval.linked.deductions", { list: (env.envelope.deductions ?? []).map((d) => `${d.kind} ${d.amount}`).join(", ") })}</p>}
                    <p>{t("approval.linked.digest", { digest: env.envelope_sha256 ?? "" })}</p>
                  </>
                )}
                <p className="fy-secondary">{t("approval.linked.enrol")}</p>
                <button className="fy-btn fy-btn--primary" type="button" disabled={!env.ready} onClick={approveWithProof}>{t("approval.linked.approve", { version: String(v.version) })}</button>
              </section>
            )}
            {v.state === "calculated" && !mine && v.flags_failing === 0 && envAsked && !linked && <button className="fy-btn fy-btn--primary" type="button" onClick={approve}>{t("approval.approve", { version: String(v.version) })}</button>}
            {sub?.linked && sub.version > 0 && (
              <section className="fy-stack fy-stack--2" aria-labelledby="sub-h">
                <h2 className="fy-h2" id="sub-h">{t("approval.linked.submission")}</h2>
                <p role="status">{t(`approval.linked.state.${sub.state}`, { version: String(sub.version), code: sub.code ?? "" })}</p>
                {sub.posting?.status === "posted" && <p role="status">{t("approval.linked.posted", { reference: sub.posting.reference ?? "" })}</p>}
                {sub.posting?.status === "refused" && <p role="alert">{t("approval.linked.refused", { code: sub.posting.code ?? "", message: sub.posting.message ?? "" })}</p>}
                {(sub.code === "PPL-19" || sub.posting?.code === "PPL-19") && <p className="fy-secondary">{t("approval.linked.enrolFix")}</p>}
                {sub.retryable && <button className="fy-btn fy-btn--secondary" type="button" onClick={sendAgain}>{t("approval.linked.sendAgain")}</button>}
              </section>
            )}
            {v.state === "approved" && <p role="status" className="fy-secondary">{t("approval.approved")}</p>}
            {hasApproved && (
              <section className="fy-stack fy-stack--2" aria-labelledby="exports-h">
                <h2 className="fy-h2" id="exports-h">{t("exports.title")}</h2>
                <p className="fy-secondary">{t("exports.lead")}</p>
                <div className="fy-row" style={{ gap: "var(--space-3)" }}>
                  <button className="fy-btn fy-btn--secondary" type="button" onClick={() => makeFile("journal")}>{t("exports.journal")}</button>
                  <button className="fy-btn fy-btn--secondary" type="button" onClick={() => makeFile("payments")}>{t("exports.payments")}</button>
                </div>
              </section>
            )}
            {hasApproved && <PayPayments run={id} />}
            {v.state === "approved" && <button className="fy-btn fy-btn--secondary" type="button" onClick={publish}>{t("approval.publish", { version: String(v.version) })}</button>}
          </>
        )}
      </main>
    </Shell>
  );
}
