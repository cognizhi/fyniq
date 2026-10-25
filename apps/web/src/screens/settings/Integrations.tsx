import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Check, CircleCheck, CircleX, Hourglass, KeyRound, Plug, RefreshCw, Search, ShieldCheck, X } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import type { Approval } from "../approvals/words";
import {
  dataClassesLabel,
  healthChip,
  kindLabel,
  kinds,
  missingSecrets,
  processedIn,
  regionsLabel,
  stateChip,
  upgradeOf,
  type CatalogueEntry,
  type Connector,
  type InstallationChange,
  type Kind,
  type Scope,
} from "./connectorWords";
import { SettingsPage } from "./SettingsPage";

async function listCatalogue() {
  const r = await api.GET("/v1/connector-catalogue", { params: { query: { page_size: 200 } } });
  if (r.error) throw r.error;
  return r.data.items ?? [];
}

async function listConnectors(scope: Scope) {
  const r =
    scope === "installation"
      ? await api.GET("/v1/installation/connectors", { params: { query: { page_size: 200 } } })
      : await api.GET("/v1/organisation/connectors", { params: { query: { page_size: 200 } } });
  if (r.error) throw r.error;
  return (r.data.items ?? []).filter((c) => c.state !== "uninstalled");
}

export async function pendingChanges() {
  const r = await api.GET("/v1/installation/changes", { params: { query: { page_size: 200 } } });
  if (r.error) throw r.error;
  return (r.data.items ?? []).filter((c) => c.status === "pending");
}

// Asked is what an install, a step or a change waits for: the other Super admin's approval of an installation change,
// or an Owner's approval of the organisation's request (FR-INT-26).
// Answer is any write's answer, whichever path it took.
export type Answer = { data?: unknown; error?: unknown; response: Response };

export type Asked = { kind: "change"; change: InstallationChange } | { kind: "approval"; approval: Approval };

export function AskedBanner({ asked }: { asked: Asked }) {
  return (
    <div className="fy-banner fy-banner--info" role="status">
      <Icon of={Hourglass} size={20} />
      <div className="fy-banner__text">
        <span className="fy-strong">Waiting for a second approver</span>
        {asked.kind === "change" ? (
          <span className="fy-small">
            {asked.change.summary}: the other Super admin approves it below or on their own page, by {dateTime(asked.change.expires_at)}. Nothing changes until then.
          </span>
        ) : (
          <span className="fy-small">
            {asked.approval.summary}. Nothing changes until an Owner who did not ask approves it.{" "}
            <Link to="/approvals/$approvalId" params={{ approvalId: asked.approval.id }}>
              View the request
            </Link>
          </span>
        )}
      </div>
    </div>
  );
}

// ChangeDecision lets a Super admin approve or reject the other's installation change, over the envelope they read
// (its SHA-256), with step-up. The one who asked sees that it waits for the other.
export function ChangeDecision({ change, onDecided }: { change: InstallationChange; onDecided: () => void }) {
  const { session } = useSignedIn();
  const confirm = useConfirm();
  const id = useId();
  const [rejecting, setRejecting] = useState(false);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const approve = helpText("connector.approve");
  const reject = helpText("connector.reject");
  if (change.requested_by === session.user_id) return <span className="fy-small fy-secondary">Waiting for the other Super admin</span>;
  const decide = async (yes: boolean) => {
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const params = { path: { change_id: change.id }, header: { "Idempotency-Key": key } };
    const r = await guarded<Answer>(confirm, () =>
      yes
        ? api.POST("/v1/installation/changes/{change_id}/approve", { params, body: { envelope_hash: change.envelope_hash } })
        : api.POST("/v1/installation/changes/{change_id}/reject", { params, body: { envelope_hash: change.envelope_hash, reason } }),
    );
    setBusy(false);
    if (r.error) {
      setProblem(problemOf(r.error));
      return;
    }
    setRejecting(false);
    onDecided();
  };
  return (
    <div className="fy-stack fy-stack--2">
      {problem && <Refusal problem={problem} title="The change was not decided" />}
      <div className="fy-row fy-row--wrap gap-2">
        <Button size="sm" variant="primary" onClick={() => decide(true)} disabled={busy} title={approve.tooltip} aria-label={`${approve.label}: ${change.summary}`}>
          <Icon of={Check} />
          {approve.label}
        </Button>
        <Button size="sm" variant="ghost" onClick={() => setRejecting(true)} disabled={busy} title={reject.tooltip} aria-label={`${reject.label}: ${change.summary}`}>
          <Icon of={X} />
          {reject.label}
        </Button>
      </div>
      <Dialog
        open={rejecting}
        onOpenChange={(o) => !o && setRejecting(false)}
        dismissible={!busy}
        eyebrow="Installation change"
        title={`Reject: ${change.summary}`}
        footer={
          <>
            <Button variant="ghost" onClick={() => setRejecting(false)} disabled={busy}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={() => decide(false)} disabled={busy || !reason.trim()}>
              {reject.label}
            </Button>
          </>
        }
      >
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-why`}>
            Why <span className="fy-req" aria-hidden="true">*</span>
          </label>
          <textarea className="fy-textarea" id={`${id}-why`} value={reason} required onChange={(e) => setReason(e.target.value)} aria-describedby={`${id}-why-help`} />
          <span className="fy-help" id={`${id}-why-help`}>
            The one who asked reads it. A secret they set for this change is destroyed.
          </span>
        </div>
      </Dialog>
    </div>
  );
}

function Chip({ of }: { of: { label: string; tone: string; icon: typeof Plug } }) {
  return (
    <span className={`fy-chip fy-chip--${of.tone}`}>
      <Icon of={of.icon} />
      {of.label}
    </span>
  );
}

// Installed is the table of installed connectors, with each one's status, health, secrets and last change.
function Installed({ connectors, catalogue }: { connectors: Connector[]; catalogue: CatalogueEntry[] }) {
  const health = helpText("connector.health");
  if (connectors.length === 0) return <p className="fy-secondary p-4">No connectors installed here yet. Install one from the catalogue below.</p>;
  return (
    <div className="fy-tablewrap">
      <table className="fy-table fy-table--cards">
        <thead>
          <tr>
            <th scope="col">Connector</th>
            <th scope="col">Status</th>
            <th scope="col" title={health.tooltip}>
              {health.label}
            </th>
            <th scope="col">Secrets</th>
            <th scope="col">Last change</th>
          </tr>
        </thead>
        <tbody>
          {connectors.map((c) => {
            const entry = catalogue.find((e) => e.id === c.connector);
            const kind = entry?.capabilities?.[0]?.kind;
            const up = upgradeOf(c);
            const missing = missingSecrets(c);
            return (
              <tr key={c.id}>
                <td data-card="title" className="whitespace-normal">
                  <span className="fy-stack gap-0.5">
                    <Link to="/settings/integrations/$connector" params={{ connector: c.id }} search={{ scope: c.installation ? "installation" : "organisation" }} className="fy-strong">
                      {c.name}
                    </Link>
                    <span className="fy-small fy-secondary">
                      {kind ? kindLabel(kind) : c.connector}
                      {kind === "outbound-email" && ` · ${c.position === 1 ? "primary" : `failover ${c.position - 1}`}`}
                      {c.installation ? " · installation-wide" : " · this organisation"} · version {c.manifest_version}
                    </span>
                    {up !== "none" && (
                      <span className="fy-chip fy-chip--info self-start">
                        <Icon of={RefreshCw} />
                        {c.release_version} {up === "consent" ? "asks for new access" : "available"}
                      </span>
                    )}
                  </span>
                </td>
                <td data-card="status">
                  <Chip of={stateChip[c.state]} />
                </td>
                <td data-card="meta" className="whitespace-normal">
                  <span className="fy-stack gap-0.5">
                    <Chip of={healthChip[c.health]} />
                    {c.test && !c.test.passed && <span className="fy-small fy-secondary">{c.test.reason}</span>}
                  </span>
                </td>
                <td data-card="meta" className="whitespace-normal">
                  {missing.length > 0 ? (
                    <span className="fy-chip fy-chip--warning">
                      <Icon of={KeyRound} />
                      {missing.length === 1 ? "1 not set" : `${missing.length} not set`}
                    </span>
                  ) : (c.secrets ?? []).length === 0 ? (
                    <span className="fy-secondary">None needed</span>
                  ) : (
                    <span className="fy-small">
                      Set{" "}
                      {date(
                        (c.secrets ?? [])
                          .map((s) => s.replaced_at ?? "")
                          .sort()
                          .at(-1) ?? c.installed_at,
                      )}
                    </span>
                  )}
                </td>
                <td data-card="meta" className="whitespace-nowrap">
                  {dateTime(c.changed_at)}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

// Catalogue is the connectors compiled into this release, by kind of service, each with its manifest's summary:
// who publishes it, what it sends out of Fyniq and where it processes it (FR-INT-25).
function Catalogue({
  entries,
  installed,
  mayInstall,
  onInstall,
}: {
  entries: CatalogueEntry[];
  installed: Connector[];
  mayInstall: (e: CatalogueEntry) => boolean;
  onInstall: (e: CatalogueEntry) => void;
}) {
  const id = useId();
  const [kind, setKind] = useState<Kind | "all">("all");
  const [query, setQuery] = useState("");
  const install = helpText("connector.install");
  const classes = helpText("connector.data-classes");
  const q = query.trim().toLowerCase();
  const shown = entries.filter((e) => !q || `${e.name} ${e.publisher} ${e.summary}`.toLowerCase().includes(q));
  const groups = kinds.filter(([k]) => kind === "all" || kind === k).map(([k, label]) => [k, label, shown.filter((e) => (e.capabilities ?? []).some((c) => c.kind === k))] as const);
  const offered = kinds.filter(([k]) => entries.some((e) => (e.capabilities ?? []).some((c) => c.kind === k)));
  return (
    <section className="fy-card" aria-labelledby={`${id}-title`}>
      <div className="fy-card__head">
        <div className="fy-stack gap-0.5">
          <h2 className="fy-h3" id={`${id}-title`}>
            Connector catalogue
          </h2>
          <span className="fy-small fy-secondary">Shipped with this release. Each shows its manifest: who publishes it, what it sends out of Fyniq and where it processes it.</span>
        </div>
      </div>
      <div className="fy-card__body fy-stack fy-stack--4">
        <div className="fy-row fy-row--wrap gap-3">
          <div className="fy-field min-w-0 flex-[1_1_220px]">
            <label className="fy-label fy-sr" htmlFor={`${id}-q`}>
              Search connectors
            </label>
            <span className="fy-row gap-2">
              <Icon of={Search} />
              <input className="fy-input" id={`${id}-q`} type="search" placeholder="Search connectors" value={query} onChange={(e) => setQuery(e.target.value)} />
            </span>
          </div>
          <div className="fy-chipset" role="group" aria-label="Kind of service">
            {[["all", "All"] as const, ...offered].map(([k, label]) => (
              <button key={k} className="fy-filter" type="button" aria-pressed={kind === k} onClick={() => setKind(k)}>
                {label}
              </button>
            ))}
          </div>
        </div>
        {groups.every(([, , es]) => es.length === 0) && <p className="fy-secondary">No connector matches.</p>}
        {groups
          .filter(([, , es]) => es.length > 0)
          .map(([k, label, es]) => (
            <section key={k} className="fy-stack fy-stack--2" aria-labelledby={`${id}-${k}`}>
              <h3 className="fy-h3" id={`${id}-${k}`}>
                {label}
              </h3>
              <ul className="fy-list rounded-md border border-border" aria-labelledby={`${id}-${k}`}>
                {es.map((e) => {
                  const here = installed.find((c) => c.connector === e.id && c.installation === (e.scope === "installation"));
                  return (
                    <li key={e.id} className="fy-list__item">
                      <span className="fy-list__icon">
                        <Icon of={Plug} size={20} />
                      </span>
                      <span className="fy-list__main">
                        <span className="fy-list__title whitespace-normal">
                          {e.name}
                          {e.scope === "installation" && (
                            <span className="fy-badge fy-badge--quiet ms-2">
                              <Icon of={ShieldCheck} />
                              Super admin
                            </span>
                          )}
                        </span>
                        <span className="fy-list__meta whitespace-normal">
                          {e.publisher} · {e.version} · <span title={classes.tooltip}>sends {dataClassesLabel(e.data_classes)}</span> · processed {processedIn(e.regions)}
                        </span>
                        <span className="fy-small fy-secondary whitespace-normal">{e.summary}</span>
                      </span>
                      {here ? (
                        <span className="fy-chip fy-chip--good">
                          <Icon of={CircleCheck} />
                          Installed
                        </span>
                      ) : (
                        mayInstall(e) && (
                          <Button size="sm" onClick={() => onInstall(e)} title={install.tooltip} aria-label={`${install.label}: ${e.name}`}>
                            {install.label}
                          </Button>
                        )
                      )}
                    </li>
                  );
                })}
              </ul>
            </section>
          ))}
        <p className="fy-small fy-secondary">
          A connector uses only the commands and read APIs the app uses, never approves anything, and reaches only the places its manifest names, through Fyniq's checked outbound
          client. Installing, enabling, upgrading and uninstalling are audited and need a second person.
        </p>
      </div>
    </section>
  );
}

// Integrations is Settings › Integrations (docs/design/screens ConnectorCatalogue; FR-INT-25, FR-INT-26): the installed
// connectors with their status, health, secrets and available upgrades, and the catalogue the release ships, grouped
// by kind of service. A Super admin manages the installation's connectors, such as the email providers, and decides
// the other Super admin's changes; an Administrator asks to install the organisation's own, which an Owner approves.
export function Integrations() {
  const { session } = useSignedIn();
  const queries = useQueryClient();
  const navigate = useNavigate();
  const confirm = useConfirm();
  const superAdmin = Boolean(session.super_admin);
  const roles = session.roles ?? [];
  const mayAsk = roles.includes("administrator");
  const [show, setShow] = useState<"all" | Scope>("all");
  const [installing, setInstalling] = useState<CatalogueEntry | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [asked, setAsked] = useState<Asked | null>(null);
  const catalogue = useQuery({ queryKey: ["connector-catalogue"], queryFn: listCatalogue });
  const organisation = useQuery({ queryKey: ["connectors", "organisation"], queryFn: () => listConnectors("organisation") });
  const installation = useQuery({ queryKey: ["connectors", "installation"], queryFn: () => listConnectors("installation"), enabled: superAdmin });
  const changes = useQuery({ queryKey: ["installation-changes"], queryFn: pendingChanges, enabled: superAdmin });
  const install = helpText("connector.install");
  const all = [...(installation.data ?? []), ...(organisation.data ?? [])];
  const listed = all.filter((c) => show === "all" || (show === "installation") === c.installation);
  const failing = all.filter((c) => c.health === "failing");
  const enabled = all.filter((c) => c.state === "enabled");
  const upgrades = all.filter((c) => upgradeOf(c) !== "none");
  const unset = all.filter((c) => missingSecrets(c).length > 0);
  const refresh = () => Promise.all(["connectors", "installation-changes", "approvals"].map((k) => queries.invalidateQueries({ queryKey: [k] })));

  const runInstall = async () => {
    if (!installing) return;
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const params = { header: { "Idempotency-Key": key } };
    const body = { connector: installing.id };
    const r = await guarded<Answer>(confirm, () =>
      installing.scope === "installation" ? api.POST("/v1/installation/connectors", { params, body }) : api.POST("/v1/organisation/connectors", { params, body }),
    );
    setBusy(false);
    if (r.error || !r.data) {
      setProblem(problemOf(r.error));
      return;
    }
    const scope = installing.scope;
    setInstalling(null);
    await refresh();
    if (r.response.status === 202) {
      setAsked(scope === "installation" ? { kind: "change", change: r.data as InstallationChange } : { kind: "approval", approval: r.data as unknown as Approval });
      return;
    }
    await navigate({ to: "/settings/integrations/$connector", params: { connector: (r.data as Connector).id }, search: { scope } });
  };

  return (
    <SettingsPage title="Integrations" screen="connector-catalogue" lead="Connectors link Fyniq to other services. Each one is off until it is installed, configured, tested and enabled, and every step needs a second person.">
      {asked && <AskedBanner asked={asked} />}
      {failing.map((c) => (
        <div key={c.id} className="fy-banner fy-banner--critical" role="status">
          <Icon of={CircleX} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">{c.name} failed its last test</span>
            <span className="fy-small">
              {c.test?.reason}{" "}
              <Link to="/settings/integrations/$connector" params={{ connector: c.id }} search={{ scope: c.installation ? "installation" : "organisation" }}>
                Fix it
              </Link>
            </span>
          </div>
        </div>
      ))}
      <div className="fy-grid fy-grid--4 gap-3">
        <div className="fy-tile">
          <span className="fy-tile__label">
            <Icon of={Plug} />
            Installed
          </span>
          <span className="fy-tile__value fy-num">{all.length}</span>
          <span className="fy-tile__meta">
            {enabled.length} enabled · {all.filter((c) => c.state === "paused").length} paused · {all.filter((c) => c.state === "disabled").length} off
          </span>
        </div>
        <div className="fy-tile">
          <span className="fy-tile__label">
            <Icon of={CircleCheck} />
            Health
          </span>
          <span className="fy-tile__value fy-num">
            {enabled.filter((c) => c.health === "ok").length}
            <small> of {enabled.length} OK</small>
          </span>
          <span className="fy-tile__meta">{failing.length > 0 ? `${failing.length} failing: ${failing.map((c) => c.name).join(", ")}` : "None failing"}</span>
        </div>
        <div className="fy-tile">
          <span className="fy-tile__label">
            <Icon of={KeyRound} />
            Secrets
          </span>
          <span className="fy-tile__value fy-num">
            {unset.length}
            <small> not set</small>
          </span>
          <span className="fy-tile__meta">{unset.length > 0 ? unset.map((c) => c.name).join(", ") : "Every secret is set"}</span>
        </div>
        <div className="fy-tile">
          <span className="fy-tile__label">
            <Icon of={RefreshCw} />
            Upgrades
          </span>
          <span className="fy-tile__value fy-num">
            {upgrades.length}
            <small> available</small>
          </span>
          <span className="fy-tile__meta">
            {upgrades.length > 0 ? `${upgrades.map((c) => c.name).join(", ")}${upgrades.some((c) => c.upgrade_widens) ? " · new access needs consent" : ""}` : "Every connector is current"}
          </span>
        </div>
      </div>
      {superAdmin && (changes.data ?? []).length > 0 && (
        <section className="fy-card" aria-labelledby="int-changes">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="int-changes">
              Installation changes waiting
            </h2>
            <span className="fy-badge fy-badge--quiet">{changes.data?.length}</span>
          </div>
          <ul className="fy-list" aria-labelledby="int-changes">
            {(changes.data ?? []).map((ch) => (
              <li key={ch.id} className="fy-list__item">
                <span className="fy-list__icon">
                  <Icon of={Hourglass} size={20} />
                </span>
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">{ch.summary}</span>
                  <span className="fy-list__meta">
                    Asked {dateTime(ch.requested_at)} · expires {dateTime(ch.expires_at)}
                  </span>
                </span>
                <ChangeDecision change={ch} onDecided={refresh} />
              </li>
            ))}
          </ul>
        </section>
      )}
      <section className="fy-card" aria-labelledby="int-installed">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="int-installed">
            Installed connectors
          </h2>
          {superAdmin && (
            <div className="fy-chipset" role="group" aria-label="Show">
              {(
                [
                  ["all", "All"],
                  ["organisation", "This organisation"],
                  ["installation", "Installation-wide"],
                ] as const
              ).map(([k, label]) => (
                <button key={k} className="fy-filter" type="button" aria-pressed={show === k} onClick={() => setShow(k)}>
                  {label}
                </button>
              ))}
            </div>
          )}
        </div>
        {organisation.isError && <Refusal problem={problemOf(organisation.error)} title="The organisation's connectors could not be listed" />}
        {installation.isError && <Refusal problem={problemOf(installation.error)} title="The installation's connectors could not be listed" />}
        {(organisation.isPending || (superAdmin && installation.isPending)) && <p className="fy-secondary p-4">Loading the connectors…</p>}
        {organisation.data && (!superAdmin || installation.data) && <Installed connectors={listed} catalogue={catalogue.data ?? []} />}
      </section>
      {catalogue.isError && <Refusal problem={problemOf(catalogue.error)} title="The catalogue could not be listed" />}
      {catalogue.data && (
        <Catalogue
          entries={catalogue.data}
          installed={all}
          mayInstall={(e) => (e.scope === "installation" ? superAdmin : mayAsk)}
          onInstall={(e) => {
            setProblem(null);
            setInstalling(e);
          }}
        />
      )}
      <Dialog
        open={installing !== null}
        onOpenChange={(o) => !o && setInstalling(null)}
        dismissible={!busy}
        wide
        eyebrow={installing?.scope === "installation" ? "Install for the whole installation" : "Install for this organisation"}
        title={installing ? `${install.label} ${installing.name}` : install.label}
        description={installing?.summary}
        footer={
          installing && (
            <>
              <Button variant="ghost" onClick={() => setInstalling(null)} disabled={busy}>
                Cancel
              </Button>
              <Button variant="primary" onClick={runInstall} disabled={busy}>
                {installing.scope === "installation" ? install.label : "Ask to install"}
              </Button>
            </>
          )
        }
      >
        {problem && <Refusal problem={problem} title="The connector was not installed" />}
        {installing && (
          <>
            <dl className="fy-dl">
              <div>
                <dt>Publisher</dt>
                <dd>
                  {installing.publisher} · version {installing.version}
                </dd>
              </div>
              <div>
                <dt>Sends out of Fyniq</dt>
                <dd>{dataClassesLabel(installing.data_classes)}</dd>
              </div>
              <div>
                <dt>Processed in</dt>
                <dd>{regionsLabel(installing.regions)}</dd>
              </div>
              <div>
                <dt>Reaches</dt>
                <dd className="fy-mono">{(installing.destinations ?? []).map((d) => d.host ?? `the ${d.configured} you set`).join(", ") || "nowhere"}</dd>
              </div>
              <div>
                <dt>Secrets</dt>
                <dd>{(installing.secrets ?? []).map((s) => s.title).join(", ") || "None"}</dd>
              </div>
              <div>
                <dt>Acts when</dt>
                <dd>{(installing.capabilities ?? []).map((c) => c.trigger_rule).join(" ")}</dd>
              </div>
            </dl>
            <p className="fy-small fy-secondary">
              Installing records your consent to this manifest and leaves the connector off: configure it, set its secrets, test it, then enable it.
              {installing.scope === "installation"
                ? " With two Super admins, the other approves it first."
                : " An Owner who did not ask approves it first."}
            </p>
          </>
        )}
      </Dialog>
    </SettingsPage>
  );
}
