import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams, useSearch } from "@tanstack/react-router";
import { Check, ChevronLeft, CircleCheck, CircleX, FlaskConical, KeyRound, Pause, Play, Power, RefreshCw, Trash2, TriangleAlert, Webhook } from "lucide-react";
import { useId, useState, type ReactNode } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import type { Approval } from "../approvals/words";
import { dataClassesLabel, healthChip, kindLabel, regionsLabel, stateChip, upgradeOf, type CatalogueEntry, type Connector, type InstallationChange, type Scope } from "./connectorWords";
import { AskedBanner, ChangeDecision, type Answer, type Asked } from "./Integrations";

type Step = components["schemas"]["ConnectorStepRequest"]["step"];
type Property = { type?: string; title?: string; description?: string; enum?: string[]; default?: unknown; pattern?: string };

async function catalogueEntry(id: string): Promise<CatalogueEntry | undefined> {
  const r = await api.GET("/v1/connector-catalogue", { params: { query: { page_size: 200 } } });
  if (r.error) throw r.error;
  return (r.data.items ?? []).find((e) => e.id === id);
}

async function readConnector(scope: Scope, id: string) {
  const params = { path: { connector_id: id } };
  const r = scope === "installation" ? await api.GET("/v1/installation/connectors/{connector_id}", { params }) : await api.GET("/v1/organisation/connectors/{connector_id}", { params });
  if (r.error) throw r.error;
  return { connector: r.data, etag: etagOf(r.response) || `"${r.data.version}"` };
}

// history is every request about this connector: the installation's changes, or the organisation's approval requests.
async function history(scope: Scope, c: Connector): Promise<{ changes: InstallationChange[]; approvals: Approval[] }> {
  if (scope === "installation") {
    const r = await api.GET("/v1/installation/changes", { params: { query: { page_size: 200 } } });
    if (r.error) throw r.error;
    return { changes: (r.data.items ?? []).filter((ch) => ch.connector_id === c.id || (ch.step === "install" && ch.connector === c.connector)), approvals: [] };
  }
  const r = await api.GET("/v1/approvals", { params: { query: { status: "all", page_size: 200 } } });
  if (r.error) throw r.error;
  return { changes: [], approvals: (r.data.items ?? []).filter((a) => a.object_type === "connector" && (a.object_key === c.id || a.object_key === c.connector)) };
}

function Chip({ of }: { of: { label: string; tone: string; icon: typeof Check } }) {
  return (
    <span className={`fy-chip fy-chip--${of.tone}`}>
      <Icon of={of.icon} />
      {of.label}
    </span>
  );
}

function Card({ title, aside, children, foot }: { title: string; aside?: ReactNode; children: ReactNode; foot?: ReactNode }) {
  const id = useId();
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          {title}
        </h2>
        {aside}
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">{children}</div>
      {foot && <div className="fy-card__foot">{foot}</div>}
    </section>
  );
}

const lifecycle = ["Install", "Configure", "Test", "Enable"] as const;
const reached: Record<Connector["state"], number> = { installed: 1, configured: 2, tested: 3, enabled: 4, paused: 4, disabled: 3, uninstalled: 0 };

// Lifecycle is the stepper (install → configure → test → enable) and what pausing, disabling and uninstalling do.
function Lifecycle({ c, entry }: { c: Connector; entry?: CatalogueEntry }) {
  const done = reached[c.state];
  const cap = entry?.capabilities?.[0];
  return (
    <Card title="Lifecycle" aside={<span className="fy-small fy-secondary">Every step is audited and needs a second person.</span>}>
      <ol className="fy-steps fy-steps--wrap" aria-label="Lifecycle">
        {lifecycle.map((s, i) => (
          <li key={s} className={i < done ? "fy-step fy-step--done" : "fy-step"} aria-current={i === done ? "step" : undefined}>
            <span className="fy-step__n">{i < done ? <Icon of={Check} /> : i + 1}</span>
            {i === 3 && (c.state === "paused" || c.state === "disabled") ? stateChip[c.state].label : s}
          </li>
        ))}
      </ol>
      <dl className="fy-dl">
        <div>
          <dt>Pause</dt>
          <dd>Stops new work at once and holds its queue; resume picks up where it stopped. One person with step-up pauses; the others are told.</dd>
        </div>
        <div>
          <dt>Disable</dt>
          <dd>
            {cap?.on_disable === "cancel" ? "Also cancels its queued work." : "Holds its queued work until it is enabled again."} Work already handed to the provider{" "}
            {cap?.in_flight === "finish" ? "finishes" : "is reconciled"}, never dropped.
          </dd>
        </div>
        <div>
          <dt>Uninstall</dt>
          <dd>Stops it for good and deletes its secrets; its audit history stays.</dd>
        </div>
      </dl>
    </Card>
  );
}

function TestResult({ c }: { c: Connector }) {
  if (!c.test) return <p className="fy-secondary">Not tested yet.</p>;
  return (
    <div className={`fy-banner fy-banner--${c.test.passed ? "good" : "critical"}`}>
      <Icon of={c.test.passed ? CircleCheck : CircleX} size={20} />
      <div className="fy-banner__text">
        <span className="fy-strong">
          {c.test.passed ? "Passed" : "Failed"} {dateTime(c.test.at)}
        </span>
        <span className="fy-small">{c.test.reason}</span>
      </div>
    </div>
  );
}

// ConfigurationForm is the form the manifest's configuration schema describes: a text field for a string, a choice
// for an enum, a number, a switch, and JSON for a nested value. Secrets are never part of it (FR-INT-20).
function ConfigurationForm({ c, entry, busy, onSave }: { c: Connector; entry: CatalogueEntry; busy: boolean; onSave: (configuration: Record<string, unknown>) => void }) {
  const id = useId();
  const schema = entry.configuration as { properties?: Record<string, Property>; required?: string[] };
  const props = Object.entries(schema.properties ?? {});
  const required = schema.required ?? [];
  const initial = Object.fromEntries(
    props.map(([k, p]) => {
      const v = c.configuration[k];
      if (p.type === "boolean") return [k, v === true];
      if (v === undefined || v === null) return [k, ""];
      return [k, typeof v === "object" ? JSON.stringify(v, null, 2) : String(v)];
    }),
  ) as Record<string, string | boolean>;
  const [values, setValues] = useState(initial);
  const [bad, setBad] = useState<Record<string, string>>({});
  const save = helpText("connector.configure");
  const submit = () => {
    const out: Record<string, unknown> = {};
    const errs: Record<string, string> = {};
    for (const [k, p] of props) {
      const v = values[k] ?? "";
      if (typeof v === "boolean") out[k] = v;
      else if (v.trim() === "") continue;
      else if (p.type === "object" || p.type === "array") {
        try {
          out[k] = JSON.parse(v);
        } catch {
          errs[k] = `${p.title ?? k} is not valid JSON. Check the brackets, quotes and commas, then save again.`;
        }
      } else if (p.type === "integer") out[k] = Number.parseInt(v, 10);
      else out[k] = v.trim();
    }
    setBad(errs);
    if (Object.keys(errs).length === 0) onSave(out);
  };
  return (
    <>
      {props.length === 0 && <p className="fy-secondary">This connector has nothing to configure.</p>}
      {props.map(([k, p]) => {
        const v = values[k] ?? "";
        const fid = `${id}-${k}`;
        const label = (
          <>
            {p.title ?? k}
            {required.includes(k) && (
              <span className="fy-req" aria-hidden="true">
                {" "}
                *
              </span>
            )}
          </>
        );
        const help = (
          <span className="fy-help" id={`${fid}-help`}>
            {p.description}
            {p.default !== undefined && typeof p.default !== "object" ? ` Leave it empty for ${String(p.default)}.` : ""}
          </span>
        );
        const err = bad[k] && (
          <span className="fy-error" id={`${fid}-err`}>
            {bad[k]}
          </span>
        );
        const describedBy = bad[k] ? `${fid}-help ${fid}-err` : `${fid}-help`;
        if (typeof v === "boolean")
          return (
            <div key={k} className="fy-row fy-row--top gap-3">
              <button className="fy-switch" type="button" role="switch" id={fid} aria-checked={v} aria-labelledby={`${fid}-l`} aria-describedby={`${fid}-help`} onClick={() => setValues({ ...values, [k]: !v })} />
              <span className="fy-stack gap-0.5">
                <span className="fy-strong" id={`${fid}-l`}>
                  {label}
                </span>
                {help}
              </span>
            </div>
          );
        return (
          <div key={k} className="fy-field">
            <label className="fy-label" htmlFor={fid}>
              {label}
            </label>
            {p.enum ? (
              <select className="fy-select" id={fid} value={v} aria-describedby={describedBy} onChange={(e) => setValues({ ...values, [k]: e.target.value })}>
                <option value="">Choose…</option>
                {p.enum.map((o) => (
                  <option key={o} value={o}>
                    {o}
                  </option>
                ))}
              </select>
            ) : p.type === "object" || p.type === "array" ? (
              <textarea className="fy-textarea fy-mono" id={fid} value={v} aria-invalid={Boolean(bad[k])} aria-describedby={describedBy} onChange={(e) => setValues({ ...values, [k]: e.target.value })} />
            ) : (
              <input
                className={p.type === "string" ? "fy-input fy-mono" : "fy-input"}
                id={fid}
                type={p.type === "integer" || p.type === "number" ? "number" : "text"}
                value={v}
                placeholder={typeof p.default === "string" ? p.default : undefined}
                required={required.includes(k)}
                aria-describedby={describedBy}
                onChange={(e) => setValues({ ...values, [k]: e.target.value })}
              />
            )}
            {help}
            {err}
          </div>
        );
      })}
      <div className="fy-row fy-row--wrap gap-2">
        <span className="fy-small fy-secondary fy-grow">Saving asks a second person to approve the change; a changed configuration is tested again before it is enabled.</span>
        <Button variant="primary" onClick={submit} disabled={busy || props.length === 0} title={save.tooltip}>
          {save.label}
        </Button>
      </div>
    </>
  );
}

// Secret is one write-only secret: whether it is set and when it was replaced, and a field to set a new value, which
// goes straight to Secret Manager and is never shown again (FR-INT-20).
function Secret({ info, state, busy, onSet }: { info: components["schemas"]["ConnectorSecretInfo"]; state?: components["schemas"]["ConnectorSecretState"]; busy: boolean; onSet: (value: string) => Promise<boolean> }) {
  const id = useId();
  const [value, setValue] = useState("");
  const set = helpText("connector.secret");
  return (
    <div className="fy-field">
      <label className="fy-label" htmlFor={id}>
        {info.title} <span className="fy-badge fy-badge--quiet">Write-only</span>
      </label>
      <span className="fy-row fy-row--wrap gap-2">
        <input className="fy-input fy-mono min-w-0 flex-[1_1_200px]" id={id} type="password" autoComplete="new-password" value={value} aria-describedby={`${id}-help`} onChange={(e) => setValue(e.target.value)} />
        <Button
          onClick={async () => {
            if (await onSet(value)) setValue("");
          }}
          disabled={busy || !value}
          title={set.tooltip}
          aria-label={`${state?.set ? "Replace" : set.label}: ${info.title}`}
        >
          <Icon of={KeyRound} />
          {state?.set ? "Replace" : set.label}
        </Button>
      </span>
      <span className="fy-help" id={`${id}-help`}>
        {info.description} {state?.set ? `Set ${state.replaced_at ? dateTime(state.replaced_at) : ""}; never shown again.` : "Not set yet."}
        {info.rotation_overlap_hours > 0 && ` A replaced value stays valid for ${info.rotation_overlap_hours} ${info.rotation_overlap_hours === 1 ? "hour" : "hours"}.`}
      </span>
    </div>
  );
}

function Manifest({ entry, c }: { entry: CatalogueEntry; c: Connector }) {
  const classes = helpText("connector.data-classes");
  return (
    <Card title="Manifest" aside={<span className="fy-badge fy-badge--quiet">{c.manifest_version}</span>}>
      <dl className="fy-dl">
        <div>
          <dt>Publisher</dt>
          <dd>{entry.publisher}</dd>
        </div>
        <div>
          <dt>Capability</dt>
          <dd>{(entry.capabilities ?? []).map((k) => kindLabel(k.kind)).join(", ")}</dd>
        </div>
        <div>
          <dt title={classes.tooltip}>{classes.label}</dt>
          <dd>{dataClassesLabel(entry.data_classes)}</dd>
        </div>
        <div>
          <dt>Processing region</dt>
          <dd>{regionsLabel(entry.regions)}</dd>
        </div>
        <div>
          <dt>Destinations</dt>
          <dd className="fy-mono">{(entry.destinations ?? []).map((d) => d.host ?? `the ${d.configured} you set`).join(", ")}</dd>
        </div>
        <div>
          <dt>Acts when</dt>
          <dd>{(entry.capabilities ?? []).map((k) => k.trigger_rule).join(" ")}</dd>
        </div>
        <div>
          <dt>Secrets</dt>
          <dd>{(entry.secrets ?? []).map((s) => s.title).join(", ") || "None"}</dd>
        </div>
        <div>
          <dt>Local substitute</dt>
          <dd>{entry.local_substitute}</dd>
        </div>
      </dl>
      {(entry.webhooks ?? []).length > 0 && (
        <ul className="fy-list rounded-md border border-border" aria-label="Webhooks">
          {(entry.webhooks ?? []).map((w) => (
            <li key={w.path} className="fy-list__item">
              <span className="fy-list__icon">
                <Icon of={Webhook} />
              </span>
              <span className="fy-list__main">
                <span className="fy-list__title fy-mono">{w.path}</span>
                <span className="fy-list__meta whitespace-normal">
                  {(w.events ?? []).join(", ")} · {w.registered_by === "operator" ? "set by a person in the provider's dashboard" : "registered by the connector"}
                </span>
              </span>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

// Upgrade shows what the release's manifest changes from the one consented to (FR-INT-26): new access needs consent
// again, and the connector runs under the old consent until then.
function Upgrade({ c, busy, onConsent }: { c: Connector; busy: boolean; onConsent: () => void }) {
  const consent = helpText("connector.consent");
  const lines = (label: string, sign: "+" | "−", ch: components["schemas"]["ConsentChange"]) =>
    [
      ...(ch.data_classes ?? []).map((d) => `${label} data class: ${dataClassesLabel([d])}`),
      ...(ch.destinations ?? []).map((d) => `${label} destination: ${d.host ?? d.configured ?? ""}`),
      ...(ch.scopes ?? []).map((s) => `${label} scope: ${s}`),
      ...(ch.webhooks ?? []).map((w) => `${label} webhook: ${w}`),
    ].map((t) => ({ sign, t }));
  const diff = [...lines("New", "+", c.added), ...lines("No longer", "−", c.removed)];
  return (
    <Card
      title={`Upgrade ${c.manifest_version} to ${c.release_version}`}
      aside={c.upgrade_widens ? <span className="fy-chip fy-chip--warning">Needs consent</span> : <span className="fy-chip fy-chip--neutral">Asks nothing new</span>}
      foot={
        <>
          <span className="fy-small fy-secondary fy-grow">{c.upgrade_widens ? "It runs under the old consent until a second person approves yours." : "Consenting moves it to the new manifest."}</span>
          <Button variant="primary" onClick={onConsent} disabled={busy} title={consent.tooltip}>
            <Icon of={RefreshCw} />
            {consent.label}
          </Button>
        </>
      }
    >
      <p className="fy-small fy-secondary">Manifest changes. Lines with a plus are new; lines with a minus go away.</p>
      {diff.length === 0 ? (
        <p>Nothing it sends, reaches or receives changes.</p>
      ) : (
        <ul className="fy-stack fy-stack--1 fy-mono fy-small" aria-label="Manifest changes">
          {diff.map((d) => (
            <li key={`${d.sign}${d.t}`}>
              <span aria-hidden="true">{d.sign} </span>
              <span className="fy-sr">{d.sign === "+" ? "Added: " : "Removed: "}</span>
              {d.t}
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

const purposes: Record<string, string> = { notifications: "Notifications", invoices: "Invoices and quotes", reminders: "Reminders", security: "Security" };
const reasons: Record<string, string> = { hard_bounce: "Hard bounce", complaint: "Complaint: marked as spam", suppressed: "Suppressed by the provider" };
const dmarc: Record<string, string> = {
  reject: "Rejects mail that fails",
  quarantine: "Quarantines mail that fails",
  none: "Only reports: receivers still deliver fake mail. Set it to quarantine.",
  invalid: "Not a valid DMARC record",
  missing: "No DMARC record",
  failed: "Could not be looked up; it is checked again within the hour",
};

// Email is the outbound-email part of the page, through the organisation's existing email operations (FR-INT-21):
// senders by purpose, the domains' checks, the failover order and the suppression list.
function Email({ c }: { c: Connector }) {
  const queries = useQueryClient();
  const id = useId();
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [editing, setEditing] = useState<components["schemas"]["EmailSender"] | null>(null);
  const [address, setAddress] = useState("");
  const [name, setName] = useState("");
  const change = helpText("email.sender");
  const release = helpText("email.release");
  const settings = useQuery({
    queryKey: ["email-settings"],
    queryFn: async () => {
      const r = await api.GET("/v1/organisation/email");
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const suppressions = useQuery({
    queryKey: ["email-suppressions"],
    queryFn: async () => {
      const r = await api.GET("/v1/organisation/email/suppressions", { params: { query: { page_size: 200 } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  const saveSender = async () => {
    if (!editing) return;
    setProblem(null);
    const body = address.trim() ? { address: address.trim(), ...(name.trim() ? { display_name: name.trim() } : {}) } : {};
    const r = await api.PUT("/v1/organisation/email/senders/{purpose}", { params: { path: { purpose: editing.purpose }, header: { "If-Match": editing.etag, "Idempotency-Key": newKey() } }, body });
    if (r.error) {
      setProblem(problemOf(r.error));
      return;
    }
    setEditing(null);
    await queries.invalidateQueries({ queryKey: ["email-settings"] });
  };
  const releaseIt = async (s: components["schemas"]["EmailSuppression"]) => {
    setProblem(null);
    const r = await api.POST("/v1/organisation/email/suppressions/{suppression_id}/release", { params: { path: { suppression_id: s.id }, header: { "Idempotency-Key": newKey() } } });
    if (r.error) setProblem(problemOf(r.error));
    await queries.invalidateQueries({ queryKey: ["email-suppressions"] });
  };
  const providers = settings.data?.providers ?? [];
  return (
    <>
      {problem && <Refusal problem={problem} title="The email setting was not changed" />}
      {settings.isError && <Refusal problem={problemOf(settings.error)} title="The email settings could not be read" />}
      {settings.data && (
        <>
          <Card title="Senders by purpose">
            <div className="fy-tablewrap">
              <table className="fy-table fy-table--cards">
                <thead>
                  <tr>
                    <th scope="col">Purpose</th>
                    <th scope="col">From</th>
                    <th scope="col">
                      <span className="fy-sr">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {(settings.data.senders ?? []).map((s) => (
                    <tr key={s.purpose}>
                      <th scope="row" data-card="title">
                        {purposes[s.purpose] ?? s.purpose}
                      </th>
                      <td data-card="meta" className="fy-mono whitespace-normal">
                        {s.from}
                        {s.platform && <span className="fy-small fy-secondary"> (the platform's)</span>}
                      </td>
                      <td>
                        <Button
                          size="sm"
                          variant="ghost"
                          title={change.tooltip}
                          aria-label={`${change.label}: ${purposes[s.purpose] ?? s.purpose}`}
                          onClick={() => {
                            setAddress(s.address ?? "");
                            setName(s.display_name ?? "");
                            setEditing(s);
                          }}
                        >
                          {change.label}
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Card>
          <Card title="Domain authentication" aside={<span className="fy-small fy-secondary">Checked within the hour and every night</span>}>
            {(settings.data.domain_checks ?? []).length === 0 && <p className="fy-secondary">No sending domain yet: the deployment's operators add one.</p>}
            {(settings.data.domain_checks ?? []).map((d) => (
              <div key={d.domain} className="fy-stack fy-stack--2">
                <span className="fy-row fy-row--wrap gap-2">
                  <span className="fy-strong fy-mono">{d.domain}</span>
                  <Chip of={d.ok ? { label: "Pass", tone: "good", icon: CircleCheck } : { label: "Needs attention", tone: "warning", icon: TriangleAlert }} />
                  <span className="fy-small fy-secondary">checked {dateTime(d.checked_at)}</span>
                </span>
                <ul className="fy-list rounded-md border border-border" aria-label={`Records of ${d.domain}`}>
                  {(d.records ?? []).map((rec) => (
                    <li key={`${rec.type}-${rec.name}`} className="fy-list__item">
                      <span className="fy-list__main">
                        <span className="fy-list__title fy-mono whitespace-normal">
                          {rec.type} {rec.name}
                        </span>
                        <span className="fy-list__meta whitespace-normal">
                          {rec.status === "ok" ? "There as asked" : rec.status === "missing" ? "Missing: add it at your DNS host" : rec.status === "wrong" ? `Holds another value${rec.found ? `: ${rec.found}` : ""}` : "The lookup failed; it is checked again"}
                        </span>
                      </span>
                      <Chip of={rec.status === "ok" ? { label: "Pass", tone: "good", icon: CircleCheck } : { label: rec.status === "failed" ? "Unknown" : "Fail", tone: rec.status === "failed" ? "neutral" : "critical", icon: CircleX }} />
                    </li>
                  ))}
                  <li className="fy-list__item">
                    <span className="fy-list__main">
                      <span className="fy-list__title">DMARC</span>
                      <span className="fy-list__meta whitespace-normal">{dmarc[d.dmarc]}</span>
                    </span>
                    <Chip of={d.dmarc === "reject" || d.dmarc === "quarantine" ? { label: "Pass", tone: "good", icon: CircleCheck } : { label: "Weak", tone: "warning", icon: TriangleAlert }} />
                  </li>
                </ul>
              </div>
            ))}
          </Card>
          <Card title="Failover order">
            {providers.length === 0 ? (
              <p className="fy-secondary">No provider is enabled, so no email is sent.</p>
            ) : (
              <ol className="fy-list rounded-md border border-border" aria-label="Failover order">
                {providers.map((p, i) => (
                  <li key={p.name} className="fy-list__item">
                    <span className="fy-list__icon fy-num">{i + 1}</span>
                    <span className="fy-list__main">
                      <span className="fy-list__title">{p.name === c.connector ? c.name : p.name}</span>
                      <span className="fy-list__meta">{p.role === "primary" ? "Primary" : "Failover"}</span>
                    </span>
                  </li>
                ))}
              </ol>
            )}
            <p className="fy-small fy-secondary">Fyniq fails over only for sends it knows the provider did not accept. A send with an unknown result is checked first, so nobody gets an email twice.</p>
          </Card>
        </>
      )}
      <Card title="Suppression list" aside={<span className="fy-badge fy-badge--quiet">{(suppressions.data ?? []).filter((s) => !s.released_at).length}</span>}>
        {suppressions.isError && <Refusal problem={problemOf(suppressions.error)} title="The suppression list could not be read" />}
        {suppressions.data && suppressions.data.length === 0 && <p className="fy-secondary">No address is suppressed.</p>}
        {(suppressions.data ?? []).length > 0 && (
          <div className="fy-tablewrap">
            <table className="fy-table fy-table--cards">
              <thead>
                <tr>
                  <th scope="col">Address</th>
                  <th scope="col">Reason</th>
                  <th scope="col">Since</th>
                  <th scope="col">
                    <span className="fy-sr">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {(suppressions.data ?? []).map((s) => (
                  <tr key={s.id}>
                    <td data-card="title" className="fy-mono whitespace-normal">
                      {s.address}
                    </td>
                    <td data-card="meta">{reasons[s.reason] ?? s.reason}</td>
                    <td data-card="meta">{date(s.suppressed_at)}</td>
                    <td>
                      {s.released_at ? (
                        <span className="fy-small fy-secondary">Released {date(s.released_at)}</span>
                      ) : (
                        <Button size="sm" variant="ghost" onClick={() => releaseIt(s)} title={release.tooltip} aria-label={`${release.label}: ${s.address}`}>
                          {release.label}
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <p className="fy-small fy-secondary">Fyniq doesn't send to these addresses. Fix the contact's email first, then release it here.</p>
      </Card>
      <Dialog
        open={editing !== null}
        onOpenChange={(o) => !o && setEditing(null)}
        eyebrow="Senders by purpose"
        title={editing ? `${change.label}: ${purposes[editing.purpose] ?? editing.purpose}` : change.label}
        footer={
          <>
            <Button variant="ghost" onClick={() => setEditing(null)}>
              Cancel
            </Button>
            <Button variant="primary" onClick={saveSender}>
              Save
            </Button>
          </>
        }
      >
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-addr`}>
            Address
          </label>
          <input className="fy-input fy-mono" id={`${id}-addr`} value={address} aria-describedby={`${id}-addr-help`} onChange={(e) => setAddress(e.target.value)} />
          <span className="fy-help" id={`${id}-addr-help`}>
            An address on {(settings.data?.domains ?? []).join(", ") || "a sending domain"}. Leave it empty to use the platform's sender{settings.data?.platform_sender ? `, ${settings.data.platform_sender}` : ""}.
          </span>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-name`}>
            Name shown with it
          </label>
          <input className="fy-input" id={`${id}-name`} value={name} disabled={!address.trim()} onChange={(e) => setName(e.target.value)} />
        </div>
      </Dialog>
    </>
  );
}

// Activity is the connector's recent work: for outbound email, the messages it sent.
function Activity({ c, kind }: { c: Connector; kind?: string }) {
  const messages = useQuery({
    queryKey: ["email-messages", c.connector],
    enabled: kind === "outbound-email",
    queryFn: async () => {
      const r = await api.GET("/v1/organisation/email/messages", { params: { query: { page_size: 100 } } });
      if (r.error) throw r.error;
      return (r.data.items ?? []).filter((m) => m.provider === c.connector || m.status === "queued");
    },
  });
  if (kind !== "outbound-email") return <Card title="Activity">{<p className="fy-secondary">Its activity shows here once it does work in this organisation.</p>}</Card>;
  const list = messages.data ?? [];
  const count = (s: string) => list.filter((m) => m.status === s).length;
  return (
    <Card title="Activity">
      {messages.isError && <Refusal problem={problemOf(messages.error)} title="The messages could not be listed" />}
      <dl className="fy-dl fy-dl--3">
        <div>
          <dt>Queued</dt>
          <dd className="fy-num">{count("queued")}</dd>
        </div>
        <div>
          <dt>Accepted</dt>
          <dd className="fy-num">{count("accepted")}</dd>
        </div>
        <div>
          <dt>Failed</dt>
          <dd className="fy-num">{count("failed")}</dd>
        </div>
      </dl>
      {list.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards">
            <thead>
              <tr>
                <th scope="col">Message</th>
                <th scope="col">To</th>
                <th scope="col">Time</th>
                <th scope="col">Status</th>
              </tr>
            </thead>
            <tbody>
              {list.slice(0, 20).map((m) => (
                <tr key={m.id}>
                  <td data-card="title" className="whitespace-normal">
                    {m.subject}
                  </td>
                  <td data-card="meta" className="fy-mono">
                    {m.recipient}
                  </td>
                  <td data-card="meta" className="whitespace-nowrap">
                    {dateTime(m.accepted_at ?? m.created_at)}
                  </td>
                  <td data-card="status">{m.status === "accepted" ? "Accepted" : m.status === "queued" ? "Queued" : m.status === "failed" ? "Failed" : "Unknown"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}

// Audit is the connector's history: when it was installed and last changed, and every request about it with its
// decision. The full audit trail, with before and after values, is in the audit trail.
function Audit({ scope, c }: { scope: Scope; c: Connector }) {
  const h = useQuery({ queryKey: ["connector-history", scope, c.id], queryFn: () => history(scope, c) });
  const rows = [
    ...(h.data?.changes ?? []).map((ch) => ({ id: ch.id, title: ch.summary, at: ch.requested_at, meta: `${ch.status}${ch.reason ? `: ${ch.reason}` : ""}${ch.decided_at ? `, ${dateTime(ch.decided_at)}` : ""}` })),
    ...(h.data?.approvals ?? []).map((a) => ({ id: a.id, title: a.summary, at: a.requested_at, meta: `${a.status} · asked by ${a.requested_by_name}${a.decided_at ? `, decided ${dateTime(a.decided_at)}` : ""}` })),
  ].sort((a, b) => b.at.localeCompare(a.at));
  return (
    <Card title="Audit">
      {h.isError && <Refusal problem={problemOf(h.error)} title="The history could not be read" />}
      <ol className="fy-timeline" aria-label="History">
        {rows.map((r) => (
          <li key={r.id} className="fy-timeline__item">
            <span className="fy-timeline__dot" aria-hidden="true" />
            <span className="fy-stack gap-0.5">
              <span className="fy-strong">{r.title}</span>
              <span className="fy-small fy-secondary">
                {dateTime(r.at)} · {r.meta}
              </span>
            </span>
          </li>
        ))}
        {c.test && (
          <li className="fy-timeline__item">
            <span className={c.test.passed ? "fy-timeline__dot fy-timeline__dot--good" : "fy-timeline__dot"} aria-hidden="true" />
            <span className="fy-stack gap-0.5">
              <span className="fy-strong">Test {c.test.passed ? "passed" : `failed: ${c.test.reason}`}</span>
              <span className="fy-small fy-secondary">{dateTime(c.test.at)}</span>
            </span>
          </li>
        )}
        <li className="fy-timeline__item">
          <span className="fy-timeline__dot" aria-hidden="true" />
          <span className="fy-stack gap-0.5">
            <span className="fy-strong">Installed</span>
            <span className="fy-small fy-secondary">{dateTime(c.installed_at)}</span>
          </span>
        </li>
      </ol>
    </Card>
  );
}

type Tab = "overview" | "configuration" | "email" | "upgrade" | "activity" | "audit";

// IntegrationDetail is Settings › Integrations › a connector (docs/design/screens ConnectorDetail; FR-INT-20, FR-INT-26,
// FR-INT-27): its lifecycle, configuration and write-only secrets, its test, health and manifest, an upgrade's diff to
// consent to, and for email its senders, domains, failover order and suppressions. Each step goes to the API, which
// asks a second person for every step but a pause and a test; the page shows what waits for them.
export function IntegrationDetail() {
  const { connector: id = "" } = useParams({ strict: false }) as { connector?: string };
  const search = useSearch({ strict: false }) as { scope?: string };
  const scope: Scope = search.scope === "installation" ? "installation" : "organisation";
  const queries = useQueryClient();
  const confirm = useConfirm();
  const tabsId = useId();
  const [tab, setTab] = useState<Tab>("overview");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<{ problem: ApiProblem; title: string } | null>(null);
  const [asked, setAsked] = useState<Asked | null>(null);
  const [uninstalling, setUninstalling] = useState(false);
  const [position, setPosition] = useState("");
  const q = useQuery({ queryKey: ["connector", scope, id], queryFn: () => readConnector(scope, id) });
  const c = q.data?.connector;
  const entryQ = useQuery({ queryKey: ["connector-catalogue", c?.connector], enabled: Boolean(c), queryFn: () => catalogueEntry(c?.connector ?? "") });
  const entry = entryQ.data;
  const waiting = useQuery({
    queryKey: ["connector-waiting", scope, id],
    enabled: Boolean(c),
    queryFn: async () => {
      if (scope === "installation") {
        const r = await api.GET("/v1/installation/changes", { params: { query: { page_size: 200 } } });
        if (r.error) throw r.error;
        return { changes: (r.data.items ?? []).filter((ch) => ch.status === "pending" && ch.connector_id === id), approvals: [] as Approval[] };
      }
      const r = await api.GET("/v1/approvals", { params: { query: { status: "pending", page_size: 200 } } });
      if (r.error) throw r.error;
      return { changes: [] as InstallationChange[], approvals: (r.data.items ?? []).filter((a) => a.object_type === "connector" && a.object_key === id) };
    },
  });
  const test = helpText("connector.test");
  const words: Record<Step, ReturnType<typeof helpText>> = {
    enable: helpText("connector.enable"),
    pause: helpText("connector.pause"),
    resume: helpText("connector.resume"),
    disable: helpText("connector.disable"),
    uninstall: helpText("connector.uninstall"),
    consent: helpText("connector.consent"),
    order: helpText("connector.order"),
  };

  const refresh = () => Promise.all(["connector", "connectors", "connector-waiting", "connector-history", "installation-changes", "approvals"].map((k) => queries.invalidateQueries({ queryKey: [k] })));

  // send runs one write over the connector's ETag, with step-up when the API asks for it. A 202 is the request a second
  // person decides; a 200 is the connector as it is now.
  const send = async (title: string, call: (etag: string, key: string) => Promise<Answer>): Promise<boolean> => {
    if (!q.data) return false;
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const etag = q.data.etag;
    const r = await guarded(confirm, () => call(etag, key));
    setBusy(false);
    if (r.error || r.data === undefined) {
      setProblem({ problem: problemOf(r.error), title });
      return false;
    }
    if (r.response.status === 202) setAsked(scope === "installation" ? { kind: "change", change: r.data as InstallationChange } : { kind: "approval", approval: r.data as Approval });
    await refresh();
    return true;
  };
  const path = { connector_id: id };
  const step = (s: Step, extra: { position?: number } = {}) =>
    send(`${words[s].label} did not go through`, (etag, key) => {
      const params = { path, header: { "If-Match": etag, "Idempotency-Key": key } };
      const body = { step: s, ...extra };
      return scope === "installation" ? api.POST("/v1/installation/connectors/{connector_id}/steps", { params, body }) : api.POST("/v1/organisation/connectors/{connector_id}/steps", { params, body });
    });
  const runTest = () =>
    send("The test did not run", (etag, key) => {
      const params = { path, header: { "If-Match": etag, "Idempotency-Key": key } };
      return scope === "installation" ? api.POST("/v1/installation/connectors/{connector_id}/test", { params }) : api.POST("/v1/organisation/connectors/{connector_id}/test", { params });
    });
  const configure = (configuration: Record<string, unknown>) =>
    send("The configuration was not saved", (etag, key) => {
      const params = { path, header: { "If-Match": etag, "Idempotency-Key": key } };
      return scope === "installation"
        ? api.PUT("/v1/installation/connectors/{connector_id}/configuration", { params, body: { configuration } })
        : api.PUT("/v1/organisation/connectors/{connector_id}/configuration", { params, body: { configuration } });
    });
  const setSecret = (secret: string, value: string) =>
    send("The secret was not set", (etag, key) => {
      const params = { path: { connector_id: id, secret }, header: { "If-Match": etag, "Idempotency-Key": key } };
      return scope === "installation"
        ? api.PUT("/v1/installation/connectors/{connector_id}/secrets/{secret}", { params, body: { value } })
        : api.PUT("/v1/organisation/connectors/{connector_id}/secrets/{secret}", { params, body: { value } });
    });

  const kind = entry?.capabilities?.[0]?.kind;
  const up = c ? upgradeOf(c) : "none";
  const tabs: [Tab, string][] = [
    ["overview", "Overview"],
    ["configuration", "Configuration"],
    ...(kind === "outbound-email" ? ([["email", "Senders and domain"]] as [Tab, string][]) : []),
    ...(up !== "none" ? ([["upgrade", "Upgrade"]] as [Tab, string][]) : []),
    ["activity", "Activity"],
    ["audit", "Audit"],
  ];
  const pending = waiting.data;
  const live = c && c.state !== "uninstalled";
  const testable = c && ["configured", "tested", "enabled", "paused", "disabled"].includes(c.state);

  return (
    <div className="fy-page fy-enter">
      <Link className="fy-btn fy-btn--ghost fy-btn--sm -ms-2 self-start" to="/settings/integrations">
        <Icon of={ChevronLeft} />
        Integrations
      </Link>
      {q.isError && <Refusal problem={problemOf(q.error)} title="The connector could not be shown" />}
      {q.isPending && <p className="fy-secondary">Loading the connector…</p>}
      {c && (
        <>
          <div className="fy-pagehead">
            <div className="fy-pagehead__text">
              <span className="fy-eyebrow">Settings · Integrations</span>
              <h1 className="fy-h1">{c.name}</h1>
              <span className="fy-row fy-row--wrap gap-2">
                <Chip of={stateChip[c.state]} />
                <Chip of={healthChip[c.health]} />
                {kind === "outbound-email" && c.state === "enabled" && <span className="fy-badge fy-badge--quiet">{c.position === 1 ? "Primary for email" : `Failover ${c.position - 1} for email`}</span>}
              </span>
              <p className="fy-secondary">
                {kind ? kindLabel(kind) : c.connector}
                {entry ? ` by ${entry.publisher}` : ""} · connector {c.manifest_version} · {c.installation ? "installation-wide, managed by the Super admins" : "this organisation's"}
              </p>
            </div>
            {live && (
              <div className="fy-pagehead__actions">
                {c.state === "enabled" && (
                  <Button onClick={() => step("pause")} disabled={busy} title={words.pause.tooltip}>
                    <Icon of={Pause} />
                    {words.pause.label}
                  </Button>
                )}
                {testable && (
                  <Button variant="primary" onClick={runTest} disabled={busy} title={test.tooltip}>
                    <Icon of={FlaskConical} />
                    {test.label}
                  </Button>
                )}
              </div>
            )}
          </div>
          <ReviewBanner screen="connector-detail" />
          {asked && <AskedBanner asked={asked} />}
          {!asked &&
            (pending?.changes ?? []).map((ch) => (
              <div key={ch.id} className="fy-banner fy-banner--info" role="status">
                <div className="fy-banner__text">
                  <span className="fy-strong">{ch.summary} is waiting for a second approver</span>
                  <span className="fy-small">Asked {dateTime(ch.requested_at)}; it expires {dateTime(ch.expires_at)}. Nothing changes until then.</span>
                  <ChangeDecision change={ch} onDecided={refresh} />
                </div>
              </div>
            ))}
          {!asked && (pending?.approvals ?? []).map((a) => <AskedBanner key={a.id} asked={{ kind: "approval", approval: a }} />)}
          {c.state === "uninstalled" && (c.webhooks_to_remove ?? []).length > 0 && (
            <div className="fy-banner fy-banner--warning" role="status">
              <Icon of={TriangleAlert} size={20} />
              <div className="fy-banner__text">
                <span className="fy-strong">Remove its webhooks at the provider</span>
                <span className="fy-small">
                  It is uninstalled, and its secrets are deleted. Remove these in the provider's dashboard: {(c.webhooks_to_remove ?? []).map((w) => w.path).join(", ")}.
                </span>
              </div>
            </div>
          )}
          {problem && <Refusal problem={problem.problem} title={problem.title} />}
          <div className="fy-tabs" role="tablist" aria-label="Connector views">
            {tabs.map(([k, label]) => (
              <button key={k} type="button" role="tab" id={`${tabsId}-tab-${k}`} aria-selected={tab === k} aria-controls={`${tabsId}-panel-${k}`} onClick={() => setTab(k)}>
                {label}
              </button>
            ))}
          </div>
          <div role="tabpanel" id={`${tabsId}-panel-${tab}`} aria-labelledby={`${tabsId}-tab-${tab}`} className="fy-stack fy-stack--6">
            {tab === "overview" && (
              <div className="fy-split fy-split--wide-aside">
                <div className="fy-stack fy-stack--6">
                  <Lifecycle c={c} entry={entry} />
                  <Card title="Connection test" aside={c.test && <Chip of={c.test.passed ? { label: "Passed", tone: "good", icon: CircleCheck } : { label: "Failed", tone: "critical", icon: CircleX }} />}>
                    <TestResult c={c} />
                    <p className="fy-small fy-secondary">{entry?.test_action} A test shows pass or fail with the reason only, never what the provider sent back.</p>
                  </Card>
                  {live && (
                    <Card title="Turn it on or off">
                      <div className="fy-row fy-row--wrap gap-2">
                        {(c.state === "tested" || c.state === "disabled") && (
                          <Button variant="primary" onClick={() => step("enable")} disabled={busy} title={words.enable.tooltip}>
                            <Icon of={Power} />
                            {words.enable.label}
                          </Button>
                        )}
                        {c.state === "paused" && (
                          <Button variant="primary" onClick={() => step("resume")} disabled={busy} title={words.resume.tooltip}>
                            <Icon of={Play} />
                            {words.resume.label}
                          </Button>
                        )}
                        {c.state === "enabled" && (
                          <Button onClick={() => step("pause")} disabled={busy} title={words.pause.tooltip}>
                            <Icon of={Pause} />
                            {words.pause.label}
                          </Button>
                        )}
                        {(c.state === "enabled" || c.state === "paused") && (
                          <Button onClick={() => step("disable")} disabled={busy} title={words.disable.tooltip}>
                            <Icon of={Power} />
                            {words.disable.label}
                          </Button>
                        )}
                        <Button variant="destructive" onClick={() => setUninstalling(true)} disabled={busy} title={words.uninstall.tooltip}>
                          <Icon of={Trash2} />
                          {words.uninstall.label}
                        </Button>
                      </div>
                      <p className="fy-small fy-secondary">
                        Each action asks for your passkey. A pause takes effect at once and tells the others; every other step waits for a second person's approval.
                      </p>
                    </Card>
                  )}
                </div>
                <aside className="fy-stack fy-stack--6" aria-label="Health and manifest">
                  <Card title="Health" aside={<Chip of={healthChip[c.health]} />}>
                    <p className="fy-small">{c.health === "ok" ? "Enabled, and its last test passed." : c.health === "failing" ? "Its last test failed. Fix what the reason says, then test it again." : "Not enabled, or not tested yet."}</p>
                    {entry && <p className="fy-small fy-secondary">{entry.health_check}</p>}
                  </Card>
                  {kind === "outbound-email" && live && (
                    <Card title="Place among email providers">
                      <div className="fy-field">
                        <label className="fy-label" htmlFor={`${tabsId}-pos`}>
                          Place
                        </label>
                        <span className="fy-row gap-2">
                          <input className="fy-input w-24" id={`${tabsId}-pos`} type="number" min={1} max={1000} value={position || String(c.position)} aria-describedby={`${tabsId}-pos-help`} onChange={(e) => setPosition(e.target.value)} />
                          <Button onClick={() => step("order", { position: Number.parseInt(position || String(c.position), 10) })} disabled={busy || !position || position === String(c.position)} title={words.order.tooltip}>
                            <Icon of={RefreshCw} />
                            {words.order.label}
                          </Button>
                        </span>
                        <span className="fy-help" id={`${tabsId}-pos-help`}>
                          1 is the primary; the next takes over only what the ones before it refused.
                        </span>
                      </div>
                    </Card>
                  )}
                  {entry && <Manifest entry={entry} c={c} />}
                </aside>
              </div>
            )}
            {tab === "configuration" && entry && live && (
              <>
                <Card title="Configuration" aside={<span className="fy-small fy-secondary">From the manifest schema</span>}>
                  <ConfigurationForm key={c.version} c={c} entry={entry} busy={busy} onSave={configure} />
                </Card>
                <Card title="Secrets" aside={<span className="fy-small fy-secondary">Kept in Secret Manager; never shown again</span>}>
                  {(entry.secrets ?? []).length === 0 && <p className="fy-secondary">This connector needs no secret.</p>}
                  {(entry.secrets ?? []).map((s) => (
                    <Secret key={s.name} info={s} state={(c.secrets ?? []).find((x) => x.name === s.name)} busy={busy} onSet={(v) => setSecret(s.name, v)} />
                  ))}
                </Card>
              </>
            )}
            {tab === "email" && <Email c={c} />}
            {tab === "upgrade" && <Upgrade c={c} busy={busy} onConsent={() => step("consent")} />}
            {tab === "activity" && <Activity c={c} kind={kind} />}
            {tab === "audit" && <Audit scope={scope} c={c} />}
          </div>
          <Dialog
            open={uninstalling}
            onOpenChange={(o) => !o && setUninstalling(false)}
            dismissible={!busy}
            eyebrow="Uninstall"
            title={`${words.uninstall.label} ${c.name}`}
            footer={
              <>
                <Button variant="ghost" onClick={() => setUninstalling(false)} disabled={busy}>
                  Cancel
                </Button>
                <Button
                  variant="destructive"
                  disabled={busy}
                  onClick={async () => {
                    if (await step("uninstall")) setUninstalling(false);
                  }}
                >
                  {words.uninstall.label}
                </Button>
              </>
            }
          >
            <p>Once approved, it stops at once and its secrets are deleted. Its audit history stays.</p>
            {(entry?.webhooks ?? []).some((w) => w.registered_by === "operator") && (
              <p className="fy-small fy-secondary">Afterwards, remove its webhooks in the provider's dashboard; this page lists them.</p>
            )}
          </Dialog>
        </>
      )}
    </div>
  );
}
