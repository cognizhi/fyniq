import { useQuery, useQueryClient } from "@tanstack/react-query";
import { BadgeCheck, Bot, CircleHelp, Lock, Plug, ShieldCheck, TriangleAlert } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { Refusal } from "../components/Refusal";
import { Button } from "../components/ui/button";
import { helpText } from "../i18n";
import { dateTime } from "../format/date";
import { useSignedIn } from "../session";
import { useConfirm } from "../stepup";
import { Icon, problemOf, type ApiProblem } from "../ui";

type Screen = components["schemas"]["ConsentScreen"];

// CodeStepUp is the OAuth flow's "confirm it's you first" (AUTH-FLOW-08): an AI app's consent needs a second factor passed just now.
const CodeStepUp = "AUTH-FLOW-08";

// leave is how the page sends the browser on: the OAuth server carries the app's authorisation on from the address the API returns.
export const leave = { to: (url: string) => window.location.assign(url) };

async function read(challenge: string): Promise<Screen> {
  const { data, error } = await api.GET("/v1/oauth/consent", { params: { query: { consent_challenge: challenge } } });
  if (error) throw error;
  return data;
}

// OAuthConsent is the screen an app's request to connect lands on (docs/design/screens OAuthConsent; AI-37, AI-35, AI-36): who asks
// and whether Fyniq verified them, what each scope reaches, how long it lasts, and Allow or Deny. The person may keep fewer areas
// than the app asked for, never more. Hydra sends the browser here with the consent_challenge; the answer is the address that carries
// the app's authorisation on.
export function OAuthConsent() {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const { session } = useSignedIn();
  const challenge = new URLSearchParams(window.location.search).get("consent_challenge") ?? "";
  const q = useQuery({ queryKey: ["oauth-consent", challenge], queryFn: () => read(challenge), enabled: challenge !== "", retry: false });
  const [off, setOff] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("oauth.consent");

  const answer = async (decision: "accept" | "deny", scopes?: string[]) => {
    setBusy(true);
    setProblem(null);
    const r = await api.POST("/v1/oauth/consent", { body: { consent_challenge: challenge, decision, ...(scopes ? { scopes } : {}) } });
    if (r.error) {
      setBusy(false);
      setProblem(problemOf(r.error));
      return;
    }
    leave.to(r.data.redirect_to);
  };
  const confirmItsYou = async () => {
    if (await confirm()) await queries.invalidateQueries({ queryKey: ["oauth-consent", challenge] });
  };

  if (challenge === "") {
    return (
      <div className="mx-auto max-w-xl fy-stack fy-stack--3">
        <h1 className="fy-h2">{h.label}</h1>
        <p className="fy-secondary">This page opens from an app that asks to connect, such as Claude Code. Start from the app, and it brings you here.</p>
      </div>
    );
  }
  if (q.error) {
    const p = problemOf(q.error);
    return (
      <div className="mx-auto max-w-xl fy-stack fy-stack--3">
        <h1 className="fy-h2">{h.label}</h1>
        <Refusal problem={p} title="This request cannot be shown" />
      </div>
    );
  }
  const v = q.data;
  if (!v) return <p className="mx-auto max-w-xl fy-secondary">Loading the request.</p>;

  const reaches = new Map((v.data_categories ?? []).map((c) => [c.scope, c.reaches]));
  const granted = v.decision.scopes;
  const readable = granted.filter((s) => s !== "openid");
  const kept = readable.filter((s) => !off.includes(s));
  const refusedScopes = v.requested_scopes.filter((s) => !granted.includes(s));
  const stepUp = v.decision.code === CodeStepUp;
  const refused = v.decision.problem !== undefined && v.decision.problem !== "";
  const name = v.client.name;
  const toggle = (s: string) => setOff((o) => (o.includes(s) ? o.filter((x) => x !== s) : [...o, s]));

  return (
    <div className="mx-auto max-w-xl fy-stack fy-stack--4">
      <section className="fy-card" aria-labelledby={`${id}-title`}>
        <div className="fy-card__body fy-stack fy-stack--3">
          <div className="fy-row fy-row--top gap-3">
            <span className="fy-list__icon fy-list__icon--brand">
              <Icon of={Plug} size={24} />
            </span>
            <div className="fy-stack fy-stack--1 fy-grow">
              <h1 className="fy-h2" id={`${id}-title`}>
                {name} wants to connect to Fyniq
              </h1>
              {v.client.verified ? (
                <span className="fy-small fy-secondary">
                  <Icon of={BadgeCheck} /> {v.client.publisher}, verified publisher
                </span>
              ) : (
                <span className="fy-small fy-secondary">
                  <Icon of={CircleHelp} /> Fyniq has not verified who publishes this app
                </span>
              )}
            </div>
          </div>
          <div className="fy-chipset">
            <span className={`fy-chip ${v.client.verified ? "fy-chip--good" : "fy-chip--warning"}`}>
              <Icon of={v.client.verified ? BadgeCheck : CircleHelp} />
              {v.client.verified ? "Verified publisher" : "Not verified"}
            </span>
            {v.client.proposal_only && (
              <span className="fy-chip fy-chip--neutral">
                <Icon of={Bot} />
                Agent: proposal-only
              </span>
            )}
          </div>
          <div className="fy-banner fy-banner--info">
            <Icon of={ShieldCheck} size={20} />
            <div className="fy-banner__text">
              <span className="fy-strong">It can read and suggest. It cannot change your books.</span>
              <span className="fy-small fy-secondary">
                Its suggestions wait for a person to approve them here in Fyniq. Approving never happens inside {name}.
              </span>
            </div>
          </div>
          <dl className="fy-dl">
            {session.stepped_up_until && (
              <div>
                <dt>You confirmed it's you</dt>
                <dd>
                  {{ passkey: "Passkey", sign_in: "Sign-in", recovery: "Recovery code" }[session.step_up_method ?? "sign_in"]}, good until {dateTime(session.stepped_up_until)}
                </dd>
              </div>
            )}
            {v.client.redirect_uris && v.client.redirect_uris.length > 0 && (
              <div>
                <dt>Sends you back to</dt>
                <dd className="fy-mono break-all">{v.client.redirect_uris.join(", ")}</dd>
                <dd className="fy-help">Check this address: it registered itself, so Fyniq cannot vouch for it.</dd>
              </div>
            )}
          </dl>
        </div>
      </section>

      {refused && (
        <div className="fy-banner fy-banner--warning" role="alert">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">{stepUp ? "Confirm it's you first" : `${name} cannot connect yet`}</span>
            <span className="fy-small">{v.decision.problem}</span>
            {stepUp && (
              <Button className="self-start" onClick={confirmItsYou}>
                <Icon of={ShieldCheck} />
                Confirm it's you
              </Button>
            )}
          </div>
        </div>
      )}

      <section className="fy-card" aria-labelledby={`${id}-scopes`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-scopes`}>
            What {name} asks for
          </h2>
          <span className="fy-badge fy-badge--quiet">
            {granted.length} of {v.requested_scopes.length} can be allowed
          </span>
        </div>
        <ul className="fy-list" aria-label="What it asks for">
          {granted.map((s) =>
            s === "openid" ? (
              <li className="fy-list__item" key={s}>
                <input type="checkbox" checked disabled aria-label="Who you are, so the app acts as you" />
                <span className="fy-list__main">
                  <span className="fy-list__title">{reaches.get(s) ?? "Who you are, so the app acts as you"}</span>
                  <span className="fy-help">Always needed.</span>
                </span>
              </li>
            ) : (
              <li className="fy-list__item" key={s}>
                <label className="fy-row gap-3">
                  <input type="checkbox" checked={!off.includes(s)} onChange={() => toggle(s)} aria-label={reaches.get(s) ?? s} />
                  <span className="fy-list__main">
                    <span className="fy-list__title">{reaches.get(s) ?? s}</span>
                  </span>
                </label>
              </li>
            ),
          )}
          {refusedScopes.map((s) => (
            <li className="fy-list__item" key={s}>
              <span className="fy-list__main">
                <span className="fy-list__title">{reaches.get(s) ?? s}</span>
                <span className="fy-help">Cannot be allowed: it is not on offer to apps, or an Owner has not granted it.</span>
              </span>
              <span className="fy-chip fy-chip--neutral">
                <Icon of={Lock} />
                Not granted
              </span>
            </li>
          ))}
        </ul>
      </section>

      {v.expiry && (
        <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-labelledby={`${id}-limits`}>
          <h2 className="fy-h3" id={`${id}-limits`}>
            Data and time limits
          </h2>
          <ul className="fy-stack fy-stack--1 list-disc pl-6">
            <li>It sees only what you may see, and never an NRIC, a full bank account number or a secret.</li>
            <li>You allow it for {v.expiry.consent_valid_days} days, then allow it again. Its access lasts {v.expiry.access_token_minutes} minutes at a time.</li>
            <li>You can disconnect it at any time under Settings, Connected apps.</li>
          </ul>
        </section>
      )}

      {problem && <Refusal problem={problem} title="That was not recorded" />}
      <div className="fy-row fy-row--wrap gap-2">
        <Button variant="primary" disabled={busy || refused || kept.length === 0} onClick={() => answer("accept", ["openid", ...kept])}>
          Allow {name}
        </Button>
        <Button variant="secondary" disabled={busy} onClick={() => answer("deny")}>
          Deny
        </Button>
      </div>
    </div>
  );
}
