import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, CircleX, Hourglass, KeyRound, Pause, Play, Plus, RefreshCw, RotateCw, ShieldCheck, Webhook } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { SettingsPage } from "./SettingsPage";

type Endpoint = components["schemas"]["WebhookEndpoint"];
type Delivery = components["schemas"]["WebhookDelivery"];
type Answer = { data?: unknown; error?: unknown; response: Response };

// The event types an endpoint may ask for (FR-INT-12; the API's own allow-list answers any other with WEBHOOK-03): operational and approval events, carrying IDs and
// times, never a name, amount or title.
export const eventTypes = [
  "fyniq.approval.decided.v1",
  "fyniq.approval.requested.v1",
  "fyniq.bills.due.v1",
  "fyniq.cash.below_buffer.v1",
  "fyniq.cash.buffer_recovered.v1",
  "fyniq.deadline.overdue.v1",
  "fyniq.deadline.reminder.v1",
  "fyniq.jobs.failed.v1",
  "fyniq.kpi.anomaly.v1",
  "fyniq.ledger.posted.v1",
  "fyniq.period.closed.v1",
  "fyniq.period.reopened.v1",
  "fyniq.recurring.made.v1",
  "fyniq.reports.export_ready.v1",
  "fyniq.reports.scheduled.v1",
] as const;

const states: Record<Endpoint["state"], { label: string; tone: string; icon: typeof Webhook }> = {
  pending: { label: "Not verified", tone: "warning", icon: Hourglass },
  active: { label: "Active", tone: "good", icon: CircleCheck },
  paused: { label: "Paused", tone: "serious", icon: Pause },
  disabled: { label: "Disabled", tone: "neutral", icon: CircleX },
};

async function listEndpoints(): Promise<Endpoint[]> {
  const r = await api.GET("/v1/webhook-endpoints", { params: { query: { page_size: 200 } } });
  if (r.error) throw r.error;
  return r.data.items ?? [];
}

async function listDead(endpoint: string): Promise<Delivery[]> {
  const r = await api.GET("/v1/webhook-endpoints/{endpoint_id}/deliveries", { params: { path: { endpoint_id: endpoint }, query: { state: "dead", page_size: 100 } } });
  if (r.error) throw r.error;
  return r.data.items ?? [];
}

function StateChip({ state }: { state: Endpoint["state"] }) {
  const s = states[state];
  return (
    <span className={`fy-chip fy-chip--${s.tone}`}>
      <Icon of={s.icon} />
      {s.label}
    </span>
  );
}

// Webhooks is Settings › Integrations › Webhooks (docs/design/screens Webhooks; FR-INT-12): the endpoints events are sent to, each verified before it is active, a signing
// secret shown once, and the dead letters a person replays. An Administrator runs them with step-up; others with a view read. Everything is off until an Owner turns on
// "Webhooks and events" with a second approver, and the API says so in plain words when it is not.
export function Webhooks() {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const endpoints = useQuery({ queryKey: ["webhooks"], queryFn: listEndpoints });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [adding, setAdding] = useState(false);
  const [secret, setSecret] = useState<{ label: string; value: string } | null>(null);
  const [deadFor, setDeadFor] = useState<Endpoint | null>(null);
  const [form, setForm] = useState<{ label: string; url: string; picked: string[] }>({ label: "", url: "", picked: [] });
  const h = helpText("settings.webhooks");
  const refresh = () => queries.invalidateQueries({ queryKey: ["webhooks"] });

  // run is one step-up guarded write: the problem is shown, and on success the list reads again.
  const run = async (call: () => Promise<Answer>): Promise<Answer | null> => {
    setBusy(true);
    setProblem(null);
    const r = await guarded<Answer>(confirm, call);
    setBusy(false);
    if (r.error) {
      setProblem(problemOf(r.error));
      return null;
    }
    await refresh();
    return r;
  };
  const etag = (e: Endpoint) => `"${e.version}"`;
  const add = async () => {
    const r = await run(() => api.POST("/v1/webhook-endpoints", { body: { label: form.label.trim(), url: form.url.trim(), event_types: form.picked } }));
    const created = r?.data as { secret?: string } | undefined;
    if (created?.secret) {
      setSecret({ label: form.label.trim(), value: created.secret });
      setAdding(false);
      setForm({ label: "", url: "", picked: [] });
    }
  };
  const verify = (e: Endpoint) => run(() => api.POST("/v1/webhook-endpoints/{endpoint_id}/verify", { params: { path: { endpoint_id: e.id }, header: { "If-Match": etag(e) } } }));
  const setState = (e: Endpoint, state: "active" | "paused" | "disabled") =>
    run(() =>
      api.PATCH("/v1/webhook-endpoints/{endpoint_id}", {
        params: { path: { endpoint_id: e.id }, header: { "If-Match": etag(e), "Idempotency-Key": newKey() } },
        body: { state },
      }),
    );
  const roll = async (e: Endpoint) => {
    const r = await run(() => api.POST("/v1/webhook-endpoints/{endpoint_id}/roll-secret", { params: { path: { endpoint_id: e.id }, header: { "If-Match": etag(e) } } }));
    const rolled = r?.data as { secret?: string } | undefined;
    if (rolled?.secret) setSecret({ label: e.label, value: rolled.secret });
  };

  const list = endpoints.data ?? [];
  return (
    <SettingsPage title={h.label} lead={h.tooltip} screen="webhooks">
      {problem && !adding && <Refusal problem={problem} title="That was not changed" />}
      {endpoints.error && <Refusal problem={problemOf(endpoints.error)} title="The endpoints could not be read" />}
      <section className="fy-card" aria-labelledby={`${id}-endpoints`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-endpoints`}>
            Endpoints
          </h2>
          <Button size="sm" variant="primary" onClick={() => setAdding(true)} disabled={busy} title={helpText("webhooks.add").tooltip}>
            <Icon of={Plus} />
            {helpText("webhooks.add").label}
          </Button>
        </div>
        <div className="fy-card__body">
          {endpoints.isSuccess && list.length === 0 && <p className="fy-secondary">No endpoint yet. Add one and Fyniq sends it signed messages about the events you choose.</p>}
          <ul className="fy-list">
            {list.map((e) => (
              <li key={e.id} className="fy-list__item fy-stack fy-stack--2">
                <div className="fy-row fy-row--wrap gap-2">
                  <span className="fy-list__title">{e.label}</span>
                  <StateChip state={e.state} />
                  <span className="fy-small fy-secondary fy-mono">{e.url}</span>
                </div>
                <span className="fy-small fy-secondary">
                  {e.event_types.length} event type{e.event_types.length === 1 ? "" : "s"} · secret ending {e.secret_hint}
                  {e.verified_at ? ` · verified ${dateTime(e.verified_at)}` : ""}
                  {e.failure_run > 0 ? ` · ${e.failure_run} failed in a row` : ""}
                  {e.paused_reason ? ` · ${e.paused_reason}` : ""}
                </span>
                <div className="fy-row fy-row--wrap gap-2">
                  {e.state === "pending" && (
                    <Button size="sm" variant="primary" onClick={() => verify(e)} disabled={busy} title={helpText("webhooks.verify").tooltip} aria-label={`${helpText("webhooks.verify").label}: ${e.label}`}>
                      <Icon of={ShieldCheck} />
                      {helpText("webhooks.verify").label}
                    </Button>
                  )}
                  {e.state === "active" && (
                    <Button size="sm" variant="secondary" onClick={() => setState(e, "paused")} disabled={busy} aria-label={`Pause: ${e.label}`}>
                      <Icon of={Pause} />
                      Pause
                    </Button>
                  )}
                  {e.state === "paused" && (
                    <Button size="sm" variant="secondary" onClick={() => setState(e, "active")} disabled={busy} aria-label={`Resume: ${e.label}`}>
                      <Icon of={Play} />
                      Resume
                    </Button>
                  )}
                  {e.state !== "disabled" && (
                    <Button size="sm" variant="ghost" onClick={() => setState(e, "disabled")} disabled={busy} aria-label={`Disable: ${e.label}`}>
                      <Icon of={CircleX} />
                      Disable
                    </Button>
                  )}
                  <Button size="sm" variant="ghost" onClick={() => roll(e)} disabled={busy} title={helpText("webhooks.roll-secret").tooltip} aria-label={`${helpText("webhooks.roll-secret").label}: ${e.label}`}>
                    <Icon of={KeyRound} />
                    {helpText("webhooks.roll-secret").label}
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => setDeadFor(e)} title={helpText("webhooks.dead-letters").tooltip} aria-label={`${helpText("webhooks.dead-letters").label}: ${e.label}`}>
                    <Icon of={RotateCw} />
                    {helpText("webhooks.dead-letters").label}
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        </div>
      </section>

      <Dialog
        open={adding}
        onOpenChange={(o) => !o && setAdding(false)}
        dismissible={!busy}
        wide
        eyebrow="Webhooks"
        title={helpText("webhooks.add").label}
        description="Fyniq sends signed CloudEvents with IDs and times, never names, amounts or titles. The endpoint receives nothing until you verify it."
        footer={
          <>
            <Button variant="ghost" onClick={() => setAdding(false)} disabled={busy}>
              Cancel
            </Button>
            <Button variant="primary" onClick={add} disabled={busy || !form.label.trim() || !form.url.trim() || form.picked.length === 0}>
              {helpText("webhooks.add").label}
            </Button>
          </>
        }
      >
        {problem && <Refusal problem={problem} title="The endpoint was not added" />}
        <div className="fy-stack fy-stack--3">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-label`}>
              Name
            </label>
            <input className="fy-input" id={`${id}-label`} value={form.label} maxLength={100} onChange={(ev) => setForm({ ...form, label: ev.target.value })} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-url`}>
              Address
            </label>
            <input className="fy-input" id={`${id}-url`} type="url" value={form.url} placeholder="https://hooks.example.com/fyniq" onChange={(ev) => setForm({ ...form, url: ev.target.value })} aria-describedby={`${id}-url-help`} />
            <span className="fy-help" id={`${id}-url-help`}>
              An https address on a public host. Fyniq never sends to a private address or follows a redirect.
            </span>
          </div>
          <fieldset className="fy-field">
            <legend className="fy-label">Event types</legend>
            {eventTypes.map((t) => (
              <label key={t} className="fy-row gap-2">
                <input
                  type="checkbox"
                  checked={form.picked.includes(t)}
                  onChange={(ev) => setForm({ ...form, picked: ev.target.checked ? [...form.picked, t] : form.picked.filter((x) => x !== t) })}
                />
                <span className="fy-mono fy-small">{t}</span>
              </label>
            ))}
          </fieldset>
        </div>
      </Dialog>

      <Dialog
        open={secret !== null}
        onOpenChange={(o) => !o && setSecret(null)}
        eyebrow="Webhooks"
        title="Copy the signing secret now"
        description={secret ? `For ${secret.label}. Fyniq keeps it sealed and cannot show it again: roll the secret to get a new one.` : undefined}
        footer={
          <Button variant="primary" onClick={() => setSecret(null)}>
            I have copied it
          </Button>
        }
      >
        {secret && (
          <p className="fy-mono fy-selectable" aria-label="Signing secret">
            {secret.value}
          </p>
        )}
      </Dialog>

      <DeadLetters endpoint={deadFor} onClose={() => setDeadFor(null)} />
    </SettingsPage>
  );
}

// DeadLetters is an endpoint's dead-letter list (FR-INT-12): the deliveries whose three days of retries ran out, each replayed by a person with the same webhook-id.
function DeadLetters({ endpoint, onClose }: { endpoint: Endpoint | null; onClose: () => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const dead = useQuery({ queryKey: ["webhooks", "dead", endpoint?.id], queryFn: () => listDead(endpoint!.id), enabled: endpoint !== null });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const replay = async (d: Delivery) => {
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const r = await guarded<Answer>(confirm, () => api.POST("/v1/webhook-deliveries/{delivery_id}/replay", { params: { path: { delivery_id: d.id }, header: { "Idempotency-Key": key } } }));
    setBusy(false);
    if (r.error) setProblem(problemOf(r.error));
    else await queries.invalidateQueries({ queryKey: ["webhooks"] });
  };
  return (
    <Dialog open={endpoint !== null} onOpenChange={(o) => !o && onClose()} wide eyebrow="Webhooks" title={`${helpText("webhooks.dead-letters").label}: ${endpoint?.label ?? ""}`}
      description="Events this endpoint never accepted in three days of tries. Replay sends one again now, with the same webhook-id."
      footer={
        <Button variant="ghost" onClick={onClose}>
          Close
        </Button>
      }
    >
      {problem && <Refusal problem={problem} title="The delivery was not replayed" />}
      {dead.isSuccess && (dead.data?.length ?? 0) === 0 && <p className="fy-secondary">Nothing is waiting: every event reached this endpoint.</p>}
      <ul className="fy-list">
        {(dead.data ?? []).map((d) => (
          <li key={d.id} className="fy-list__item fy-row fy-row--wrap gap-2">
            <span className="fy-mono fy-small">{d.event_type}</span>
            <span className="fy-small fy-secondary">
              {d.attempts} tries{d.last_status ? `, last answer ${d.last_status}` : ""}
              {d.last_error ? `: ${d.last_error}` : ""} · {dateTime(d.created_at)}
            </span>
            <Button size="sm" variant="secondary" onClick={() => replay(d)} disabled={busy} aria-label={`Replay ${d.event_type} of ${dateTime(d.created_at)}`}>
              <Icon of={RefreshCw} />
              Replay
            </Button>
          </li>
        ))}
      </ul>
    </Dialog>
  );
}
