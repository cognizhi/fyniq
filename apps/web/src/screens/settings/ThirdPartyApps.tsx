import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, CircleHelp, Hourglass, Plus, Trash2 } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type App = components["schemas"]["ThirdPartyApp"];
type Answer = { data?: unknown; error?: unknown; response: Response };

async function readApps(): Promise<App[]> {
  const { data, error } = await api.GET("/v1/connected-apps/third-party-apps");
  if (error || !data) throw error;
  return data.apps;
}

const statuses: Record<App["status"], string> = {
  pending: "Waiting for an Owner",
  approved: "Approved by an Owner",
  denied: "Denied",
  expired: "Expired",
  removed: "Removed",
};

const blank = { name: "", publisher: "", publisher_url: "", redirects: "", purpose: "", processing_location: "", outside_singapore: false };

// The AI apps of other publishers an Administrator registers by hand (AI-46; p2-api-third-parties 5.1): the registration is never automatic and nothing is
// fetched from the publisher. The app cannot connect until an Owner adds it to the apps that may connect (a second approver), and it is read-only, as every app is.
// Shown to the roles that may register (Administrator) and to those who may view them (Owner, Controller); only the Administrator sees the buttons.
export function ThirdPartyApps({ labelledBy }: { labelledBy?: string }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const { session } = useSignedIn();
  const apps = useQuery({ queryKey: ["connected-apps", "third-party"], queryFn: readApps });
  const [adding, setAdding] = useState(false);
  const [form, setForm] = useState(blank);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const registers = (session.roles ?? []).includes("administrator");
  if (apps.isError || !apps.data) return null;
  if (apps.data.length === 0 && !registers) return null;

  const run = async (call: () => Promise<Answer>): Promise<boolean> => {
    setBusy(true);
    setProblem(null);
    const r = await guarded<Answer>(confirm, call);
    setBusy(false);
    if (r.error) {
      setProblem(problemOf(r.error));
      return false;
    }
    await queries.invalidateQueries({ queryKey: ["connected-apps"] });
    return true;
  };
  const redirects = form.redirects
    .split(/[\s,]+/)
    .map((u) => u.trim())
    .filter(Boolean);
  const ready = form.name.trim() && form.publisher.trim() && form.purpose.trim() && form.processing_location.trim() && redirects.length > 0 && redirects.length <= 5;
  const register = async () => {
    const ok = await run(() =>
      api.POST("/v1/connected-apps/third-party-apps", {
        body: {
          name: form.name.trim(),
          publisher: form.publisher.trim(),
          ...(form.publisher_url.trim() ? { publisher_url: form.publisher_url.trim() } : {}),
          redirect_uris: redirects,
          purpose: form.purpose.trim(),
          processing_location: form.processing_location.trim(),
          outside_singapore: form.outside_singapore,
        },
      }),
    );
    if (ok) {
      setAdding(false);
      setForm(blank);
    }
  };
  const remove = (a: App) => run(() => api.DELETE("/v1/connected-apps/third-party-apps/{client_id}", { params: { path: { client_id: a.client_id } } }));

  return (
    <section className="fy-card fy-expert-only" aria-labelledby={labelledBy ?? `${id}-third`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={labelledBy ?? `${id}-third`} title={helpText("mcp.third-party").tooltip}>
          {helpText("mcp.third-party").label}
        </h2>
        {registers && (
          <Button size="sm" variant="secondary" onClick={() => setAdding(true)} disabled={busy} title={helpText("mcp.register-third-party").tooltip}>
            <Icon of={Plus} />
            {helpText("mcp.register-third-party").label}
          </Button>
        )}
      </div>
      <div className="fy-card__body fy-stack fy-stack--2">
        <p className="fy-secondary">
          AI apps from other publishers, registered by hand by an Administrator. Fyniq has not verified any of them and fetches nothing from the publisher: an Owner adds one to the apps that may connect, with a
          second approver, before anyone can connect it. It reads only what the person could read, and proposes changes a person approves.
        </p>
        {problem && !adding && <Refusal problem={problem} title="That was not changed" />}
      </div>
      {apps.data.length === 0 ? (
        <p className="fy-card__body fy-small fy-muted">No app of another publisher is registered.</p>
      ) : (
        <ul className="fy-list" aria-label="Third-party apps">
          {apps.data.map((a) => (
            <li className="fy-list__item" key={a.client_id}>
              <span className="fy-list__main">
                <span className="fy-list__title">{a.name}</span>
                <span className="fy-help">
                  Published by {a.publisher} as stated at registration, processing in {a.processing_location}
                  {a.outside_singapore ? " (outside Singapore: a transfer to assess under the PDPA)" : ""}. For: {a.purpose}. Signs people in at {a.redirect_uris.join(", ")}. Registered {date(a.registered_at)}.
                </span>
                <span className="fy-help">
                  ID <span className="fy-mono">{a.client_id}</span>
                </span>
              </span>
              <span className={`fy-chip ${a.status === "approved" ? "fy-chip--success" : a.status === "pending" ? "fy-chip--warning" : "fy-chip--neutral"}`}>
                <Icon of={a.status === "approved" ? CircleCheck : a.status === "pending" ? Hourglass : CircleHelp} />
                {statuses[a.status]}
              </span>
              {registers && a.status !== "removed" && (
                <Button variant="secondary" size="sm" disabled={busy} onClick={() => remove(a)} aria-label={`Remove ${a.name}`} title={helpText("mcp.remove-third-party").tooltip}>
                  <Icon of={Trash2} />
                  Remove
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      <Dialog
        open={adding}
        onOpenChange={(o) => !o && setAdding(false)}
        dismissible={!busy}
        wide
        eyebrow="Connected apps"
        title={helpText("mcp.register-third-party").label}
        description="Say who publishes the app and where it signs people in. Fyniq checks the name and the addresses but does not contact the publisher. The app cannot connect until an Owner allows it."
        footer={
          <>
            <Button variant="ghost" onClick={() => setAdding(false)} disabled={busy}>
              Cancel
            </Button>
            <Button variant="primary" onClick={register} disabled={busy || !ready}>
              {helpText("mcp.register-third-party").label}
            </Button>
          </>
        }
      >
        {problem && <Refusal problem={problem} title="The app was not registered" />}
        <div className="fy-stack fy-stack--3">
          <Field id={`${id}-name`} label="App name" value={form.name} max={100} onChange={(v) => setForm({ ...form, name: v })} help="In plain letters and digits. It cannot pass for Fyniq or a known brand." />
          <Field id={`${id}-publisher`} label="Publisher" value={form.publisher} max={100} onChange={(v) => setForm({ ...form, publisher: v })} help="Who publishes it, as you have checked yourself: Fyniq does not." />
          <Field id={`${id}-url`} label="Publisher's website (optional)" value={form.publisher_url} max={2048} onChange={(v) => setForm({ ...form, publisher_url: v })} help="An https address shown to the Owner; Fyniq never fetches it." />
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-redirects`}>
              Sign-in addresses
            </label>
            <textarea className="fy-input" id={`${id}-redirects`} rows={2} value={form.redirects} placeholder="https://app.example/oauth/callback" onChange={(ev) => setForm({ ...form, redirects: ev.target.value })} aria-describedby={`${id}-redirects-help`} />
            <span className="fy-help" id={`${id}-redirects-help`}>
              One to five exact https addresses (or loopback ones), separated by spaces or commas. No wildcard, query or fragment.
            </span>
          </div>
          <Field id={`${id}-purpose`} label="What the organisation uses it for" value={form.purpose} max={300} onChange={(v) => setForm({ ...form, purpose: v })} help="Goes into the data inventory." />
          <Field id={`${id}-where`} label="Where the publisher processes what it reads" value={form.processing_location} max={100} onChange={(v) => setForm({ ...form, processing_location: v })} help="A country or region, such as Singapore or United States." />
          <label className="fy-row gap-2">
            <input type="checkbox" checked={form.outside_singapore} onChange={(ev) => setForm({ ...form, outside_singapore: ev.target.checked })} />
            <span>It processes outside Singapore (a transfer to assess under the PDPA before an Owner allows it)</span>
          </label>
        </div>
      </Dialog>
    </section>
  );
}

function Field({ id, label, value, max, onChange, help }: { id: string; label: string; value: string; max: number; onChange: (v: string) => void; help: string }) {
  return (
    <div className="fy-field">
      <label className="fy-label" htmlFor={id}>
        {label}
      </label>
      <input className="fy-input" id={id} value={value} maxLength={max} onChange={(ev) => onChange(ev.target.value)} aria-describedby={`${id}-help`} />
      <span className="fy-help" id={`${id}-help`}>
        {help}
      </span>
    </div>
  );
}
