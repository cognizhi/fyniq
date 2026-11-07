import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, CirclePause, Globe, Hourglass, Link2, Lock, ShieldCheck, Users } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { ChangeDecision } from "./Integrations";
import { SettingsPage } from "./SettingsPage";

type Method = components["schemas"]["SignInMethod"];

async function listMethods(): Promise<Method[]> {
  const { data, error } = await api.GET("/v1/installation/sign-in-methods", { params: { query: { page_size: 100 } } });
  if (error) throw error;
  return data.items ?? [];
}

// domainsOf reads the domains a Super admin typed, separated by commas, spaces or new lines.
export function domainsOf(text: string): string[] {
  return Array.from(new Set(text.split(/[\s,;]+/).map((d) => d.trim().toLowerCase()).filter(Boolean)));
}

// Status is a provider's state in words, with its icon and chip (never colour alone).
function Status({ m }: { m: Method }) {
  if (m.pending)
    return (
      <span className="fy-chip fy-chip--warning">
        <Icon of={Hourglass} />
        Waits for the other Super admin
      </span>
    );
  if (!m.enabled)
    return (
      <span className="fy-chip fy-chip--neutral">
        <Icon of={CirclePause} />
        Off
      </span>
    );
  if (m.last_for_super_admin)
    return (
      <span className="fy-chip fy-chip--neutral">
        <Icon of={Lock} />
        Last way in for a Super admin
      </span>
    );
  return (
    <span className="fy-chip fy-chip--good">
      <Icon of={CircleCheck} />
      On
    </span>
  );
}

// Change is the dialog that turns a provider on or off, or sets its domains, after confirming it's you.
function Change({ m, turning, onClose, onDone }: { m: Method | null; turning: "off" | "on" | "domains"; onClose: () => void; onDone: (said: string) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const [domains, setDomains] = useState((m?.allowed_domains ?? []).join(", "));
  const [endSessions, setEndSessions] = useState(true);
  const [join, setJoin] = useState(Boolean(m?.join_by_domain));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("sign-in-provider.change");
  if (!m) return null;
  const enabled = turning === "on" ? true : turning === "off" ? false : m.enabled;
  const title = turning === "off" ? `Turn ${m.name} off?` : turning === "on" ? `Turn ${m.name} on?` : `Email domains for ${m.name}`;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const allowed = turning === "domains" ? domainsOf(domains) : (m.allowed_domains ?? []);
    const body = { enabled, allowed_domains: allowed, join_by_domain: turning === "domains" ? join && allowed.length > 0 : m.join_by_domain, end_sessions: turning === "off" && endSessions };
    const out = await guarded(confirm, () =>
      api.PATCH("/v1/installation/sign-in-methods/{provider}", {
        params: { path: { provider: m.provider }, header: { "If-Match": `"${m.version}"`, "Idempotency-Key": key } },
        body,
      }),
    );
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    await queries.invalidateQueries({ queryKey: ["sign-in-methods"] });
    onDone(out.response.status === 202 ? `The change of ${m.name} waits for the other Super admin's approval.` : turning === "off" ? `${m.name} is off.` : turning === "on" ? `${m.name} is on.` : `${m.name}'s email domains are saved.`);
    onClose();
  };

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={title}
      description={
        turning === "off"
          ? "Nobody can sign in with it from now on; its settings are kept. Fyniq refuses it if a Super admin signs in with it alone."
          : turning === "on"
            ? "People can sign in with it again, with Fyniq's second step as always."
            : "Only accounts whose verified email ends with one of these domains can sign in with it. Leave it empty for any domain."
      }
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant={turning === "off" ? "destructive" : "primary"} type="submit" form={id} disabled={busy}>
            {turning === "off" ? <Icon of={CirclePause} /> : <Icon of={CircleCheck} />}
            {h.label}
          </Button>
        </>
      }
    >
      <form id={id} className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        {problem && <Refusal problem={problem} title="Nothing changed" />}
        {turning === "domains" && (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-domains`}>
              Allowed email domains
            </label>
            <textarea className="fy-textarea fy-mono" id={`${id}-domains`} value={domains} onChange={(e) => setDomains(e.target.value)} aria-describedby={`${id}-domains-help`} />
            <span className="fy-help" id={`${id}-domains-help`}>
              Such as cognizhi.com, separated by commas. Up to 20.
            </span>
          </div>
        )}
        {turning === "domains" && (
          <label className="fy-row fy-row--top gap-2">
            <input type="checkbox" checked={join} onChange={(e) => setJoin(e.target.checked)} />
            <span className="fy-stack fy-stack--2" style={{ gap: 2 }}>
              <span className="fy-strong">Let people with these domains ask to join</span>
              <span className="fy-small fy-secondary">Only for a domain an organisation claimed; an Owner approves each. Off by default: an invitation is needed.</span>
            </span>
          </label>
        )}
        {turning === "off" && (
          <label className="fy-row fy-row--top gap-2">
            <input type="checkbox" checked={endSessions} onChange={(e) => setEndSessions(e.target.checked)} />
            <span className="fy-stack fy-stack--2" style={{ gap: 2 }}>
              <span className="fy-strong">Also sign out everyone signed in with it</span>
              <span className="fy-small fy-secondary">Their sessions end at once. Leave it off to let them finish what they are doing.</span>
            </span>
          </label>
        )}
      </form>
    </Dialog>
  );
}

// SignInProviders is Settings › Sign-in providers (docs/design/screens SignInProviders; FR-ADM-08, PRD-001 rev 0.197): a
// Super admin turns each provider the deployment configures on or off and sets the email domains it accepts. Providers
// are added in the deployment's configuration; adding them here comes in P2 or P3.
export function SignInProviders() {
  const { session } = useSignedIn();
  const queries = useQueryClient();
  const methods = useQuery({ queryKey: ["sign-in-methods"], queryFn: listMethods, enabled: Boolean(session.super_admin) });
  const [target, setTarget] = useState<{ m: Method; turning: "off" | "on" | "domains" } | null>(null);
  const [status, setStatus] = useState("");
  const domains = helpText("sign-in-provider.domains");
  const list = methods.data ?? [];
  return (
    <SettingsPage title="Sign-in providers" screen="sign-in-providers" lead="Choose how people sign in to every organisation on this installation.">
      <div className="fy-banner fy-banner--info">
        <Icon of={ShieldCheck} size={20} />
        <div className="fy-banner__text">
          <span className="fy-strong">Fyniq's own second step is always required</span>
          <span className="fy-small fy-secondary">Whichever provider someone uses, they still confirm with a passkey or an authenticator code.</span>
        </div>
      </div>
      {!session.super_admin && <p className="fy-secondary">Only a Super admin sees and changes the sign-in providers.</p>}
      <p className="fy-small fy-secondary" role="status">
        {status}
      </p>
      {methods.isError && <Refusal problem={problemOf(methods.error)} title="The sign-in providers could not be listed" />}
      {methods.isPending && session.super_admin && <p className="fy-secondary">Loading the sign-in providers…</p>}
      {list.length > 0 && (
        <section className="fy-card" aria-labelledby="providers-title">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="providers-title">
              Providers
            </h2>
            <span className="fy-small fy-muted">
              {list.filter((m) => m.enabled).length} of {list.length} on · sign-ins in the last 30 days
            </span>
          </div>
          <ul className="fy-list" aria-labelledby="providers-title">
            {list.map((m) => {
              const locked = m.enabled && m.last_for_super_admin;
              return (
                <li key={m.provider} className="fy-list__item">
                  <span className="fy-list__icon">
                    <Icon of={Globe} size={20} />
                  </span>
                  <span className="fy-list__main">
                    <span className="fy-list__title whitespace-normal">{m.name}</span>
                    <span className="fy-list__meta whitespace-normal">
                      <span className="fy-num">{m.sign_ins}</span> sign-ins ·{" "}
                      {(m.allowed_domains ?? []).length ? `only ${(m.allowed_domains ?? []).join(", ")}` : "any email domain"}
                      {m.join_by_domain ? " · people may ask to join by domain" : ""}
                      {m.changed_at ? ` · changed ${dateTime(m.changed_at)}` : ""}
                    </span>
                  </span>
                  <Status m={m} />
                  {m.pending && <ChangeDecision change={m.pending} onDecided={() => void queries.invalidateQueries({ queryKey: ["sign-in-methods"] })} />}
                  <Button variant="ghost" size="sm" disabled={Boolean(m.pending)} onClick={() => setTarget({ m, turning: "domains" })} aria-label={`${domains.label} of ${m.name}`} title={domains.tooltip}>
                    <Icon of={Users} />
                    <span className="fy-from-medium">{domains.label}</span>
                  </Button>
                  <button
                    className="fy-switch"
                    type="button"
                    role="switch"
                    aria-checked={m.enabled}
                    aria-disabled={locked || Boolean(m.pending) ? true : undefined}
                    aria-label={`${m.name} sign-in, ${m.enabled ? "on" : "off"}${locked ? ". Can't be turned off: a Super admin signs in with it alone" : ""}`}
                    onClick={() => !locked && !m.pending && setTarget({ m, turning: m.enabled ? "off" : "on" })}
                  />
                </li>
              );
            })}
          </ul>
          <div className="fy-card__foot fy-row--wrap">
            <span className="fy-small fy-secondary fy-grow">Turning a provider off refuses new sign-ins at once and keeps its settings. Providers are added in the deployment's configuration.</span>
          </div>
        </section>
      )}
      <section className="fy-card" aria-labelledby="rules-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="rules-title">
            Rules that always apply
          </h2>
        </div>
        <ul className="fy-list">
          <li className="fy-list__item">
            <span className="fy-list__icon">
              <Icon of={Lock} size={20} />
            </span>
            <span className="fy-list__main">
              <span className="fy-list__title whitespace-normal">The last way in stays on</span>
              <span className="fy-list__meta whitespace-normal">Fyniq refuses to turn off, or narrow the domains of, the only provider a Super admin can sign in with.</span>
            </span>
          </li>
          <li className="fy-list__item">
            <span className="fy-list__icon">
              <Icon of={Link2} size={20} />
            </span>
            <span className="fy-list__main">
              <span className="fy-list__title whitespace-normal">Linking needs a second factor</span>
              <span className="fy-list__meta whitespace-normal">An account at a provider joins a person only when they sign in with it from their profile; a matching email never links.</span>
            </span>
          </li>
          <li className="fy-list__item">
            <span className="fy-list__icon">
              <Icon of={Users} size={20} />
            </span>
            <span className="fy-list__main">
              <span className="fy-list__title whitespace-normal">Two people for each change</span>
              <span className="fy-list__meta whitespace-normal">With two Super admins every change here waits for the other's approval, here or under Settings › Integrations.</span>
            </span>
          </li>
        </ul>
      </section>
      {target && <Change m={target.m} turning={target.turning} onClose={() => setTarget(null)} onDone={setStatus} />}
    </SettingsPage>
  );
}
