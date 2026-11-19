import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { BadgeCheck, CircleHelp, Link2Off, ShieldCheck, ShieldOff } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { SettingsPage } from "./SettingsPage";

type View = components["schemas"]["ConnectedAppsView"];
type Registration = components["schemas"]["MCPRegistration"];
type Call = components["schemas"]["MCPCallRow"];

async function readApps(): Promise<View> {
  const { data, error } = await api.GET("/v1/connected-apps");
  if (error) throw error;
  return data;
}

// What the person may see of everyone's activity or the waiting registrations is the API's to decide: a refusal is a quiet empty list.
async function readRegistrations(): Promise<Registration[]> {
  const { data, error } = await api.GET("/v1/connected-apps/registrations");
  return error ? [] : data.registrations;
}

async function readActivity(): Promise<Call[]> {
  const { data, error } = await api.GET("/v1/connected-apps/activity", { params: { query: { all: false } } });
  return error ? [] : data.calls;
}

// ConnectedApps is Settings › Connected apps (docs/design/screens ConnectedApps; AI-44, AI-35, AI-90): the AI apps that work for
// the person, what the organisation allows, what the person has allowed each, the registrations waiting for an Owner, and what the
// apps did. Switching it on, allowing an app and opening registration are the organisation's settings, each with a second approver.
export function ConnectedApps() {
  const id = useId();
  const queries = useQueryClient();
  const apps = useQuery({ queryKey: ["connected-apps"], queryFn: readApps });
  const waiting = useQuery({ queryKey: ["connected-apps", "registrations"], queryFn: readRegistrations });
  const activity = useQuery({ queryKey: ["connected-apps", "activity"], queryFn: readActivity });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const h = helpText("settings.connected-apps");
  const refresh = () => queries.invalidateQueries({ queryKey: ["connected-apps"] });

  const opt = async (optedIn: boolean, etag: string) => {
    setBusy(true);
    setProblem(null);
    const r = await api.PUT("/v1/me/mcp", { params: { header: { "If-Match": etag, "Idempotency-Key": newKey() } }, body: { opted_in: optedIn } });
    setBusy(false);
    if (r.error) setProblem(problemOf(r.error));
    else await refresh();
  };
  const disconnect = async (client: string) => {
    setBusy(true);
    setProblem(null);
    const r = await api.DELETE("/v1/connected-apps/connections/{client_id}", { params: { path: { client_id: client } } });
    setBusy(false);
    if (r.error) setProblem(problemOf(r.error));
    else await refresh();
  };
  const deny = async (client: string) => {
    setBusy(true);
    setProblem(null);
    const r = await api.POST("/v1/connected-apps/registrations/{client_id}/deny", { params: { path: { client_id: client } } });
    setBusy(false);
    if (r.error) setProblem(problemOf(r.error));
    else await refresh();
  };

  const v = apps.data;
  const access = v?.access;
  return (
    <SettingsPage title={h.label} lead={h.tooltip} screen="connected-apps">
      {problem && <Refusal problem={problem} title="That was not changed" />}
      {access && (
        <section className="fy-card" aria-labelledby={`${id}-access`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-access`}>
              Your access
            </h2>
            {access.can_connect ? (
              <span className="fy-chip fy-chip--success">
                <Icon of={ShieldCheck} />
                AI apps can connect as you
              </span>
            ) : (
              <span className="fy-chip fy-chip--neutral">
                <Icon of={ShieldOff} />
                AI apps cannot connect as you
              </span>
            )}
          </div>
          <div className="fy-card__body fy-stack fy-stack--3">
            <p>
              AI apps, such as Claude Code or Claude Desktop, can read your company's books as you, and only what you may see. They can propose changes, which a
              person approves in Fyniq; they cannot approve, pay or export anything.
            </p>
            <ul className="fy-stack fy-stack--1 list-disc pl-6">
              <li>
                {access.organisation_on ? "An Owner has turned AI apps on for the organisation." : "An Owner has not turned AI apps on for the organisation."}
              </li>
              <li>{access.apps.length > 0 ? `Apps the organisation allows: ${access.apps.join(", ")}.` : "The organisation has not allowed any app yet."}</li>
              <li>{access.opted_in ? "You have allowed AI apps to work for you." : "You have not allowed AI apps to work for you."}</li>
            </ul>
            <div className="fy-row fy-row--wrap gap-2">
              <Button variant={access.opted_in ? "secondary" : "primary"} disabled={busy} onClick={() => opt(!access.opted_in, access.etag)}>
                {access.opted_in ? "Stop AI apps working for me" : "Allow AI apps to work for me"}
              </Button>
              <Link to="/settings/organisation" className="fy-link fy-small">
                Organisation settings: turn AI apps on, allow an app
              </Link>
            </div>
          </div>
        </section>
      )}
      {v && (
        <section className="fy-card" aria-labelledby={`${id}-apps`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-apps`}>
              Apps
            </h2>
          </div>
          <ul className="fy-list" aria-label="Apps">
            {v.apps.map((a) => (
              <li className="fy-list__item" key={a.id}>
                <span className="fy-list__main">
                  <span className="fy-list__title">{a.name}</span>
                  <span className="fy-help">{a.verified ? `Published by ${a.publisher}` : "Registered itself: Fyniq has not verified who publishes it"}</span>
                </span>
                {a.verified ? (
                  <span className="fy-chip fy-chip--info">
                    <Icon of={BadgeCheck} />
                    Verified
                  </span>
                ) : (
                  <span className="fy-chip fy-chip--warning">
                    <Icon of={CircleHelp} />
                    Not verified
                  </span>
                )}
                <span className={`fy-chip ${a.allowed ? "fy-chip--success" : "fy-chip--neutral"}`}>{a.allowed ? "Allowed" : "Not allowed"}</span>
              </li>
            ))}
          </ul>
        </section>
      )}
      {v && (
        <section className="fy-card" aria-labelledby={`${id}-connections`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-connections`}>
              What you have allowed
            </h2>
          </div>
          {v.connections.length === 0 ? (
            <p className="fy-card__body fy-secondary">No app is connected as you.</p>
          ) : (
            <ul className="fy-list" aria-label="Your connections">
              {v.connections.map((c) => (
                <li className="fy-list__item" key={c.client_id}>
                  <span className="fy-list__main">
                    <span className="fy-list__title">{c.name}</span>
                    <span className="fy-help">
                      Allowed {date(c.granted_at)}, renew by {date(c.expires_at)}. Access: {c.scopes.join(", ")}
                    </span>
                  </span>
                  <Button variant="secondary" size="sm" disabled={busy} onClick={() => disconnect(c.client_id)} aria-label={`Disconnect ${c.name}`}>
                    <Icon of={Link2Off} />
                    Disconnect
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}
      {(waiting.data?.length ?? 0) > 0 && (
        <section className="fy-card" aria-labelledby={`${id}-waiting`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-waiting`}>
              Waiting for an Owner
            </h2>
          </div>
          <div className="fy-card__body fy-stack fy-stack--2">
            <p className="fy-secondary">
              These apps registered themselves. Fyniq has not verified them: look at where each one sends you to sign in. An Owner approves one by adding its ID to the apps that may connect, in the
              organisation's settings; or denies it here.
            </p>
          </div>
          <ul className="fy-list" aria-label="Registrations waiting for an Owner">
            {waiting.data?.map((r) => (
              <li className="fy-list__item" key={r.id}>
                <span className="fy-list__main">
                  <span className="fy-list__title">{r.name}</span>
                  <span className="fy-help">
                    ID <span className="fy-mono">{r.id}</span>, sends you to {r.redirect_uris.join(", ")}, registered {date(r.registered_at)}
                  </span>
                </span>
                <Button variant="secondary" size="sm" disabled={busy} onClick={() => deny(r.id)} aria-label={`Deny ${r.name}`}>
                  Deny
                </Button>
              </li>
            ))}
          </ul>
        </section>
      )}
      {(activity.data?.length ?? 0) > 0 && (
        <section className="fy-card" aria-labelledby={`${id}-activity`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-activity`}>
              What the apps did
            </h2>
          </div>
          <ul className="fy-list" aria-label="Recent activity">
            {activity.data?.slice(0, 20).map((c) => (
              <li className="fy-list__item" key={c.id}>
                <span className="fy-list__main">
                  <span className="fy-list__title">
                    {c.app} read with {c.tool}
                  </span>
                  <span className="fy-help">{c.outcome === "read" ? "Read" : `Refused or limited (${c.code})`}, {date(c.at)}</span>
                </span>
              </li>
            ))}
          </ul>
        </section>
      )}
    </SettingsPage>
  );
}
