import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CalendarClock, Check, CircleCheck, CircleX, Info, Megaphone, RadioTower, TriangleAlert, type LucideIcon } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { roleLabel, roleNames } from "../approvals/words";
import { SettingsPage } from "./SettingsPage";

type Announcement = components["schemas"]["Announcement"];
type Severity = Announcement["severity"];
type Scope = "organisation" | "installation";
type Show = "all" | Announcement["status"];

const severities: [Severity, string, LucideIcon][] = [
  ["info", "Info", Info],
  ["warning", "Warning", TriangleAlert],
  ["critical", "Critical", CircleX],
];

const statusChip: Record<Announcement["status"], { label: string; tone: string; icon: LucideIcon }> = {
  live: { label: "Live", tone: "good", icon: RadioTower },
  scheduled: { label: "Scheduled", tone: "info", icon: CalendarClock },
  ended: { label: "Ended", tone: "neutral", icon: CircleCheck },
};

// sgt is a datetime-local value, read as Singapore time, as an instant; "" for none.
function sgt(local: string): string {
  return local ? new Date(`${local}:00+08:00`).toISOString() : "";
}

async function listAnnouncements(scope: Scope) {
  const r =
    scope === "installation"
      ? await api.GET("/v1/installation/announcements", { params: { query: { page_size: 200 } } })
      : await api.GET("/v1/announcements", { params: { query: { page_size: 200 } } });
  if (r.error) throw r.error;
  return r.data.items ?? [];
}

const MAX = 300;

function Editor({ scope, onPublished }: { scope: Scope; onPublished: (a: Announcement) => void }) {
  const id = useId();
  const [message, setMessage] = useState("");
  const [link, setLink] = useState("");
  const [severity, setSeverity] = useState<Severity>("info");
  const [everyone, setEveryone] = useState(true);
  const [roles, setRoles] = useState<string[]>([]);
  const [starts, setStarts] = useState("");
  const [ends, setEnds] = useState("");
  const [dismissible, setDismissible] = useState(true);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const publish = helpText("announcement.publish");
  const canDismiss = severity !== "critical" && dismissible;
  const publishIt = async () => {
    setBusy(true);
    setProblem(null);
    const body = {
      message,
      severity,
      ends_at: sgt(ends),
      dismissible: canDismiss,
      ...(link.trim() ? { link: link.trim() } : {}),
      ...(starts ? { starts_at: sgt(starts) } : {}),
      ...(!everyone && roles.length > 0 ? { roles } : {}),
    } as components["schemas"]["AnnouncementRequest"];
    const params = { header: { "Idempotency-Key": newKey() } };
    const r = scope === "installation" ? await api.POST("/v1/installation/announcements", { params, body }) : await api.POST("/v1/announcements", { params, body });
    setBusy(false);
    if (r.error) {
      setProblem(problemOf(r.error));
      return;
    }
    setMessage("");
    setLink("");
    setStarts("");
    setEnds("");
    onPublished(r.data);
  };
  return (
    <div className="fy-split fy-split--wide-aside">
      <section className="fy-card" aria-labelledby={`${id}-title`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-title`}>
            New announcement
          </h2>
        </div>
        <div className="fy-card__body fy-stack">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-msg`}>
              Message <span className="fy-req" aria-hidden="true">*</span>
            </label>
            <textarea className="fy-textarea" id={`${id}-msg`} value={message} maxLength={MAX} required aria-describedby={`${id}-msg-help`} onChange={(e) => setMessage(e.target.value)} />
            <span className="fy-help" id={`${id}-msg-help`}>
              {message.length} of {MAX} characters. Plain text: one idea, what to do and by when.
            </span>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-link`}>
              Link
            </label>
            <input className="fy-input fy-mono" id={`${id}-link`} value={link} placeholder="/help/… or https://…" aria-describedby={`${id}-link-help`} onChange={(e) => setLink(e.target.value)} />
            <span className="fy-help" id={`${id}-link-help`}>
              A page in Fyniq, such as /help/organisation-settings, or an address that starts with https://. Outside links open in a new tab.
            </span>
          </div>
          <div className="fy-field">
            <span className="fy-label" id={`${id}-sev`}>
              Severity
            </span>
            <div className="fy-segmented self-start" role="radiogroup" aria-labelledby={`${id}-sev`}>
              {severities.map(([s, label, icon]) => (
                <button key={s} type="button" role="radio" aria-checked={severity === s} onClick={() => setSeverity(s)}>
                  <Icon of={icon} /> {label}
                </button>
              ))}
            </div>
            <span className="fy-help">Info and warning are announced politely to screen readers; only critical interrupts, and it can't be dismissed.</span>
          </div>
          <fieldset className="fy-fieldset">
            <legend className="fy-label">Audience</legend>
            <div className="fy-segmented self-start" role="radiogroup" aria-label="Audience">
              <button type="button" role="radio" aria-checked={everyone} onClick={() => setEveryone(true)}>
                Everyone
              </button>
              <button type="button" role="radio" aria-checked={!everyone} onClick={() => setEveryone(false)}>
                Chosen roles
              </button>
            </div>
            {!everyone && (
              <div className="fy-chipset" role="group" aria-label="Roles">
                {roleNames.map((r) => {
                  const on = roles.includes(r);
                  return (
                    <button key={r} className="fy-filter" type="button" aria-pressed={on} onClick={() => setRoles(on ? roles.filter((x) => x !== r) : [...roles, r])}>
                      {on && <Icon of={Check} />}
                      {roleLabel(r)}
                    </button>
                  );
                })}
              </div>
            )}
          </fieldset>
          <div className="fy-form fy-form--2">
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-start`}>
                Starts
              </label>
              <input className="fy-input" type="datetime-local" id={`${id}-start`} value={starts} aria-describedby={`${id}-start-help`} onChange={(e) => setStarts(e.target.value)} />
              <span className="fy-help" id={`${id}-start-help`}>
                Singapore time. Leave it empty to start now.
              </span>
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-end`}>
                Ends <span className="fy-req" aria-hidden="true">*</span>
              </label>
              <input className="fy-input" type="datetime-local" id={`${id}-end`} value={ends} required onChange={(e) => setEnds(e.target.value)} />
            </div>
          </div>
          <div className="fy-row fy-row--top gap-3">
            <button className="fy-switch" type="button" role="switch" aria-checked={canDismiss} aria-labelledby={`${id}-dis`} aria-describedby={`${id}-dis-help`}
              disabled={severity === "critical"} onClick={() => setDismissible(!dismissible)} />
            <span className="fy-stack gap-0.5">
              <span className="fy-strong" id={`${id}-dis`}>
                People can dismiss it
              </span>
              <span className="fy-small fy-secondary" id={`${id}-dis-help`}>
                Each person's dismissal is kept for them on every device. A critical banner can't be dismissed.
              </span>
            </span>
          </div>
          {problem && <Refusal problem={problem} title="The announcement was not published" />}
        </div>
        <div className="fy-card__foot">
          <span className="fy-small fy-secondary fy-grow">Publishing and ending an announcement are recorded in the audit trail.</span>
          <Button onClick={publishIt} disabled={busy || !message.trim() || !ends} title={publish.tooltip}>
            <Icon of={Megaphone} />
            {starts ? "Schedule" : publish.label}
          </Button>
        </div>
      </section>
      <aside className="fy-stack fy-stack--6" aria-label="Preview">
        <section className="fy-card" aria-labelledby={`${id}-pv`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-pv`}>
              Preview
            </h2>
          </div>
          <div className="fy-card__body fy-stack fy-stack--3">
            <div className="overflow-hidden rounded-md border border-border" aria-hidden="true">
              <div className={`fy-banner fy-banner--${severity} fy-banner--strip`}>
                <Icon of={severities.find(([s]) => s === severity)?.[2] ?? Info} size={20} />
                <div className="fy-banner__text">
                  <span className="fy-small">
                    {message || "Your message"} {link && <span className="underline">Details</span>}
                  </span>
                </div>
              </div>
            </div>
            <p className="fy-small fy-muted">It sits under the header on every page{scope === "installation" ? " of every organisation" : ""}, and never covers the field someone is working in.</p>
          </div>
        </section>
      </aside>
    </div>
  );
}

// Announcements is Settings › Announcements (FR-NTF-03; docs/design/screens Announcements): the Owner or
// Administrator publishes a banner to the organisation, or to chosen roles, now or later, until its end; a Super admin
// also to the whole installation. Every announcement is listed with its status, and one that is live or scheduled is
// ended early. A banner never changes once published: end it and publish another.
export function Announcements() {
  const { session } = useSignedIn();
  const queries = useQueryClient();
  const [scope, setScope] = useState<Scope>("organisation");
  const [show, setShow] = useState<Show>("all");
  const [published, setPublished] = useState<Announcement | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const list = useQuery({ queryKey: ["announcements", scope], queryFn: () => listAnnouncements(scope) });
  const end = helpText("announcement.withdraw");
  const roles = session.roles ?? [];
  const mayPublish = scope === "installation" ? Boolean(session.super_admin) : roles.includes("owner") || roles.includes("administrator");
  const withdraw = async (a: Announcement) => {
    setProblem(null);
    const params = { path: { announcement_id: a.id }, header: { "Idempotency-Key": newKey() } };
    const r = scope === "installation" ? await api.POST("/v1/installation/announcements/{announcement_id}/withdraw", { params }) : await api.POST("/v1/announcements/{announcement_id}/withdraw", { params });
    if (r.error) setProblem(problemOf(r.error));
    await Promise.all(["announcements", "banners"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
  };
  const items = (list.data ?? []).filter((a) => show === "all" || a.status === show);
  return (
    <SettingsPage title="Announcements" screen="announcements" lead="Put a banner under the header on every page, for the people who need to see it.">
      {session.super_admin && (
        <div className="fy-field">
          <span className="fy-label" id="ann-scope">
            Show to
          </span>
          <div className="fy-segmented self-start" role="radiogroup" aria-labelledby="ann-scope">
            <button type="button" role="radio" aria-checked={scope === "organisation"} onClick={() => setScope("organisation")}>
              This organisation
            </button>
            <button type="button" role="radio" aria-checked={scope === "installation"} onClick={() => setScope("installation")}>
              Whole installation
            </button>
          </div>
          <span className="fy-help">The whole installation is for Super admins, for example a maintenance window.</span>
        </div>
      )}
      {published && (
        <div className="fy-banner fy-banner--good" role="status">
          <Icon of={CircleCheck} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">{published.status === "scheduled" ? "Announcement scheduled" : "Announcement published"}</span>
            <span className="fy-small">
              {published.status === "scheduled" ? `It shows from ${dateTime(published.starts_at)}` : "It shows now"} until {dateTime(published.ends_at)}.
            </span>
          </div>
        </div>
      )}
      {mayPublish && (
        <Editor key={scope} scope={scope} onPublished={(a) => {
            setPublished(a);
            void Promise.all(["announcements", "banners"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
          }} />
      )}
      <section className="fy-card" aria-labelledby="ann-all">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="ann-all">
            All announcements
          </h2>
          <div className="fy-chipset" role="group" aria-label="Show">
            {(["all", "live", "scheduled", "ended"] as const).map((s) => (
              <button key={s} className="fy-filter" type="button" aria-pressed={show === s} onClick={() => setShow(s)}>
                {s === "all" ? "All" : statusChip[s].label}
              </button>
            ))}
          </div>
        </div>
        {problem && (
          <div className="px-4">
            <Refusal problem={problem} title="The announcement was not ended" />
          </div>
        )}
        {list.isError && <Refusal problem={problemOf(list.error)} title="The announcements could not be listed" />}
        {list.isPending && <p className="fy-secondary p-4">Loading the announcements…</p>}
        {list.data && items.length === 0 && <p className="fy-secondary p-4">No announcements {show === "all" ? "yet" : `${statusChip[show].label.toLowerCase()} now`}.</p>}
        {items.length > 0 && (
          <div className="fy-tablewrap">
            <table className="fy-table fy-table--cards">
              <thead>
                <tr>
                  <th scope="col">Message</th>
                  <th scope="col">Severity</th>
                  <th scope="col">Audience</th>
                  <th scope="col">Shows</th>
                  <th scope="col">Status</th>
                  <th scope="col">
                    <span className="fy-sr">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {items.map((a) => {
                  const c = statusChip[a.status];
                  const sev = severities.find(([s]) => s === a.severity);
                  return (
                    <tr key={a.id}>
                      <td data-card="title" className="whitespace-normal">
                        {a.message}
                      </td>
                      <td data-card="meta">
                        <span className="fy-row gap-1">
                          {sev && <Icon of={sev[2]} />}
                          {sev?.[1] ?? a.severity}
                        </span>
                      </td>
                      <td data-card="meta">{(a.roles ?? []).length === 0 ? "Everyone" : (a.roles ?? []).map(roleLabel).join(", ")}</td>
                      <td data-card="meta" className="whitespace-nowrap">
                        {dateTime(a.starts_at)} → {dateTime(a.withdrawn_at ?? a.ends_at)}
                      </td>
                      <td data-card="status">
                        <span className={`fy-chip fy-chip--${c.tone}`}>
                          <Icon of={c.icon} />
                          {c.label}
                        </span>
                      </td>
                      <td>
                        {a.status !== "ended" && mayPublish && (
                          <Button variant="ghost" size="sm" onClick={() => withdraw(a)} aria-label={`${end.label}: ${a.message}`} title={end.tooltip}>
                            {end.label}
                          </Button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </SettingsPage>
  );
}
