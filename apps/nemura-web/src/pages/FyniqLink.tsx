import { useEffect, useState } from "react";
import { api } from "../api/client";
import { confirmWithPasskey } from "../api/stepup";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, team } from "./Shell";

type Link = components["schemas"]["LinkState"];
type Health = components["schemas"]["LinkHealthView"];
interface Outcome {
  ok: boolean;
  text: string;
}
interface Go {
  (): Promise<Outcome>;
}
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("link.failed");

// FyniqLink is nm-fyniq-link (NM-ARC-04, NM-INT-04, 10, 13): how this company's Nemura and Fyniq work together. An Owner proposes the link with a passkey and another Owner or an Administrator approves it with Fyniq's half;
// once it is active the page shows its health (what waits, what failed and Fyniq's code), repairs a gap by sending the current state again, and, after a restore of Nemura's database, holds the link until each run
// Fyniq may have posted is decided against Fyniq's own record. It shows identities only: never a payload, a figure or an account.
export function FyniqLink({ roles }: { roles: readonly string[] }) {
  const [link, setLink] = useState<Link | null>(null);
  const [health, setHealth] = useState<Health | null>(null);
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const [org, setOrg] = useState("");
  const [half, setHalf] = useState({ key: "", client: "", clientKey: "", askKey: "" });
  const [refs, setRefs] = useState<Record<string, string>>({});
  const allowed = roles.some((r) => r === "owner" || r === "administrator");
  const load = () => {
    api.GET("/v1/link").then(({ data }) => setLink(data ?? null), () => setLink(null));
    api.GET("/v1/link/health").then(({ data }) => setHealth(data ?? null), () => setHealth(null));
  };
  useEffect(() => {
    if (allowed) load();
  }, [allowed]); // eslint-disable-line react-hooks/exhaustive-deps
  const withPasskey = async (go: Go) => {
    const failed = await confirmWithPasskey().catch(() => "");
    if (failed !== undefined) return setNote({ ok: false, text: failed || t("letters.passkeyFailed") });
    setNote(await go());
    load();
  };
  const propose = (ev: React.FormEvent) => {
    ev.preventDefault();
    return withPasskey(async () => {
      const { data, error } = await api.POST("/v1/link/proposal", { body: { fyniq_organisation_id: org } });
      if (data) setLink(data);
      return error ? { ok: false, text: problem(error) } : { ok: true, text: t("link.proposed") };
    });
  };
  const approve = (ev: React.FormEvent) => {
    ev.preventDefault();
    return withPasskey(async () => {
      let clientKey: Record<string, unknown>;
      let askKey: Record<string, unknown>;
      try {
        clientKey = JSON.parse(half.clientKey) as Record<string, unknown>;
        askKey = JSON.parse(half.askKey) as Record<string, unknown>;
      } catch {
        return { ok: false, text: t("link.keysNotJson") };
      }
      const { error } = await api.POST("/v1/link/approval", { body: { fyniq_link_key: half.key.trim(), fyniq_oauth_client_id: half.client.trim(), fyniq_client_public_key: clientKey, fyniq_ask_client_public_key: askKey } });
      return error ? { ok: false, text: problem(error) } : { ok: true, text: t("link.approved") };
    });
  };
  const repair = async () => {
    const { data, error } = await api.POST("/v1/link/repairs", { body: {} });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t("link.repaired", { n: String(data?.queued ?? 0) }) });
    load();
  };
  const decide = (runId: string, version: number, posted: boolean) =>
    withPasskey(async () => {
      const { data, error } = await api.POST("/v1/link/restore/decisions", { body: { run_id: runId, version, posted, reference: posted ? (refs[runId + version] ?? "") : "" } });
      return error ? { ok: false, text: problem(error) } : { ok: true, text: t(data?.resumed ? "link.restore.resumed" : "link.restore.decided") };
    });
  const state = link?.state ?? "none";
  const clients = link?.api_clients ?? [];
  const showClients = clients.length !== 0 && state !== "none";
  return (
    <Shell roles={roles} directory={team(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-fyniq-link" />
        <h1 className="fy-h1" title={helpText("link.page").tooltip}>{t("link.title")}</h1>
        {!allowed && <p role="alert" className="fy-secondary">{t("link.denied")}</p>}
        {allowed && !link && <p role="status" className="fy-secondary">{t("link.loading")}</p>}
        {allowed && link && (
          <>
            <p role="status"><span className={`fy-chip ${state === "active" && !health?.failing_over_24_hours ? "fy-chip--good" : "fy-chip--neutral"}`}>{t(`link.state.${state}`)}</span> {link.epoch ? t("link.epoch", { epoch: String(link.epoch) }) : ""}</p>
            {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
            {state === "none" && (
              <form className="fy-stack fy-stack--2" onSubmit={propose} aria-labelledby="propose-h">
                <h2 className="fy-h2" id="propose-h">{t("link.propose")}</h2>
                <p className="fy-secondary">{t("link.proposeLead")}</p>
                <label className="fy-field">{t("link.fyniqOrg")}<input className="fy-input" value={org} onChange={(e) => setOrg(e.target.value)} required pattern="[0-9a-fA-F-]{36}" autoComplete="off" /></label>
                <button className="fy-btn fy-btn--primary" type="submit" disabled={!roles.includes("owner")}>{t("link.proposeButton")}</button>
                {!roles.includes("owner") && <p className="fy-secondary">{t("link.ownerProposes")}</p>}
              </form>
            )}
            {state === "proposed" && (
              <>
                <section className="fy-stack fy-stack--2" aria-labelledby="give-h">
                  <h2 className="fy-h2" id="give-h">{t("link.give")}</h2>
                  <p className="fy-secondary">{t("link.giveLead")}</p>
                  <dl className="fy-stack fy-stack--1">
                    <dt>{t("link.nemuraOrg")}</dt><dd><code>{link.nemura_organisation_id}</code></dd>
                    <dt>{t("link.linkKey")}</dt><dd><code>{link.link_key}</code></dd>
                    <dt>{t("link.clientId")}</dt><dd><code>{link.nemura_client_id}</code></dd>
                    <dt>{t("link.askClientId")}</dt><dd><code>{link.nemura_ask_client_id}</code></dd>
                    {link.client_public_key && <><dt>{t("link.clientKey")}</dt><dd><code>{JSON.stringify(link.client_public_key)}</code></dd></>}
                  </dl>
                </section>
                <form className="fy-stack fy-stack--2" onSubmit={approve} aria-labelledby="approve-h">
                  <h2 className="fy-h2" id="approve-h">{t("link.approve")}</h2>
                  <p className="fy-secondary">{t("link.approveLead")}</p>
                  <label className="fy-field">{t("link.fyniqKey")}<input className="fy-input" value={half.key} onChange={(e) => setHalf({ ...half, key: e.target.value })} required autoComplete="off" /></label>
                  <label className="fy-field">{t("link.fyniqClientId")}<input className="fy-input" value={half.client} onChange={(e) => setHalf({ ...half, client: e.target.value })} required autoComplete="off" /></label>
                  <label className="fy-field">{t("link.fyniqClientKey")}<textarea className="fy-input" value={half.clientKey} onChange={(e) => setHalf({ ...half, clientKey: e.target.value })} required rows={3} /></label>
                  <label className="fy-field">{t("link.fyniqAskKey")}<textarea className="fy-input" value={half.askKey} onChange={(e) => setHalf({ ...half, askKey: e.target.value })} required rows={3} /></label>
                  <button className="fy-btn fy-btn--primary" type="submit">{t("link.approveButton")}</button>
                </form>
              </>
            )}
            {showClients && (
              <section className="fy-stack fy-stack--2" aria-labelledby="clients-h">
                <h2 className="fy-h2" id="clients-h">{t("link.clients")}</h2>
                <p className="fy-secondary">{t("link.clientsLead")}</p>
                <ul className="fy-stack fy-stack--1">{clients.map((c) => <li key={c.id}>{t("link.client", { name: c.name, scopes: (c.scopes ?? []).join(", ") })}</li>)}</ul>
              </section>
            )}
            {health && state !== "none" && state !== "proposed" && (
              <>
                {health.failing_over_24_hours && <p role="alert">{t("link.failing", { since: health.failing_since ?? "" })}</p>}
                <section className="fy-stack fy-stack--2" aria-labelledby="health-h">
                  <h2 className="fy-h2" id="health-h" title={helpText("link.health").tooltip}>{t("link.health")}</h2>
                  <p>{t("link.counts", { queued: String(health.queued), failed: String(health.failed), sent: String(health.sent), refused: String(health.refused) })}</p>
                  <p>{health.last_sent ? t("link.lastSent", { at: health.last_sent }) : t("link.neverSent")}</p>
                  <p className="fy-secondary">{t("link.checkpoint", { queued: String(health.last_sequence_queued), sent: String(health.last_sequence_sent), received: String(health.last_fyniq_sequence) })}</p>
                  {(health.undelivered ?? []).length > 0 && (
                    <table className="fy-table">
                      <caption className="fy-secondary">{t("link.undelivered")}</caption>
                      <thead><tr><th scope="col">{t("link.col.event")}</th><th scope="col">{t("link.col.object")}</th><th scope="col">{t("link.col.state")}</th><th scope="col">{t("link.col.code")}</th></tr></thead>
                      <tbody>{(health.undelivered ?? []).map((e) => (
                        <tr key={e.event_id}><td>{t("link.event", { type: e.type, sequence: String(e.sequence) })}</td><td>{t("link.object", { type: e.object_type, version: String(e.object_version) })}</td><td>{t(`link.eventState.${e.state}`)}</td><td>{e.code ?? ""}</td></tr>
                      ))}</tbody>
                    </table>
                  )}
                  {state === "active" && <button className="fy-btn fy-btn--secondary" type="button" onClick={repair} title={helpText("link.repair").tooltip}>{t("link.repair")}</button>}
                </section>
                {health.restore && (
                  <section className="fy-stack fy-stack--2" aria-labelledby="restore-h">
                    <h2 className="fy-h2" id="restore-h" title={helpText("link.restore").tooltip}>{t("link.restore.title")}</h2>
                    <p role="alert">{t("link.restore.lead", { by: health.restore.recorded_by, at: health.restore.recorded_at })}</p>
                    <p className="fy-secondary">{t("link.restore.checkpoint", { queued: String(health.restore.last_sequence_queued), sent: String(health.restore.last_sequence_sent), received: String(health.restore.last_fyniq_sequence) })}</p>
                    <ul className="fy-stack fy-stack--2">
                      {(health.restore.runs ?? []).map((r) => (
                        <li key={r.run_id + r.version} className="fy-stack fy-stack--1">
                          <span>{t("link.restore.run", { run: r.run_id.slice(0, 8), version: String(r.version) })}</span>
                          {r.outcome ? <span>{t(`link.restore.outcome.${r.outcome}`, { reference: r.reference ?? "" })}</span> : (
                            <span className="fy-row" style={{ gap: "var(--space-3)" }}>
                              <input className="fy-input" aria-label={t("link.restore.reference")} placeholder={t("link.restore.reference")} value={refs[r.run_id + r.version] ?? ""} onChange={(e) => setRefs({ ...refs, [r.run_id + r.version]: e.target.value })} />
                              <button className="fy-btn fy-btn--primary" type="button" disabled={!(refs[r.run_id + r.version] ?? "").trim()} onClick={() => decide(r.run_id, r.version, true)}>{t("link.restore.posted")}</button>
                              <button className="fy-btn fy-btn--secondary" type="button" onClick={() => decide(r.run_id, r.version, false)}>{t("link.restore.notPosted")}</button>
                            </span>
                          )}
                        </li>
                      ))}
                    </ul>
                  </section>
                )}
                {(health.bank_reconfirm ?? []).length > 0 && (
                  <section className="fy-stack fy-stack--2" aria-labelledby="reconfirm-h">
                    <h2 className="fy-h2" id="reconfirm-h" title={helpText("link.bankReconfirm").tooltip}>{t("link.bankReconfirm.title")}</h2>
                    <p role="alert">{t("link.bankReconfirm.lead")}</p>
                    <ul className="fy-stack fy-stack--2">
                      {(health.bank_reconfirm ?? []).map((r) => (
                        <li key={r.person_id}><a href={`/people/${r.person_id}`}>{t("link.bankReconfirm.person", { person: r.person_id.slice(0, 8), held: String(r.held_version), at: r.found_at })}</a></li>
                      ))}
                    </ul>
                  </section>
                )}
              </>
            )}
          </>
        )}
      </main>
    </Shell>
  );
}
