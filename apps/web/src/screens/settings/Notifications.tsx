import { useQuery, useQueryClient } from "@tanstack/react-query";
import { BadgeCheck, CircleCheck, Lock, PencilLine, RotateCcw, Send } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { SettingsPage } from "./SettingsPage";

type TypePreference = components["schemas"]["NotificationTypePreference"];
type Cadence = TypePreference["email"];

// The groups of types, in the order the page shows them.
const groups: [TypePreference["category"], string][] = [
  ["approvals", "Approvals"],
  ["operations", "Operations"],
  ["email", "Email-in and delivery"],
  ["security", "Security: always on"],
];

const cadences: [Cadence, string][] = [
  ["immediate", "Immediately"],
  ["daily", "Daily digest"],
  ["weekly", "Weekly digest"],
  ["off", "Not by email"],
];

const cadenceText = (c: Cadence) => cadences.find(([k]) => k === c)?.[1] ?? c;

async function readPreferences() {
  const { data, error } = await api.GET("/v1/notification-preferences");
  if (error) throw error;
  return data;
}

function Row({ t, onSaved }: { t: TypePreference; onSaved: () => void }) {
  const id = useId();
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const reset = helpText("notification.preference");
  const save = async (email: Cadence) => {
    setBusy(true);
    setProblem(null);
    const r = await api.PUT("/v1/notification-preferences/{type}", { params: { path: { type: t.type }, header: { "If-Match": t.etag, "Idempotency-Key": newKey() } }, body: { email } });
    setBusy(false);
    if (r.error) setProblem(problemOf(r.error));
    else onSaved();
  };
  const changed = !t.locked && t.email !== t.recommended;
  return (
    <li className="fy-stack fy-stack--2 border-t border-border px-4 py-3">
      <div className="fy-row fy-row--wrap">
        <span className="fy-strong" id={`${id}-label`}>
          {t.label}
        </span>
        {t.locked ? (
          <span className="fy-chip fy-chip--neutral">
            <Icon of={Lock} />
            Always on
          </span>
        ) : changed ? (
          <span className="fy-chip fy-chip--neutral">
            <Icon of={PencilLine} />
            Changed from recommended
          </span>
        ) : (
          <span className="fy-chip fy-chip--info">
            <Icon of={BadgeCheck} />
            Recommended
          </span>
        )}
      </div>
      <div className="fy-row fy-row--wrap gap-x-4 gap-y-2">
        <span className="fy-row gap-1.5">
          <Icon of={Lock} />
          <span className="fy-small">In the app, always</span>
        </span>
        {t.locked ? (
          <span className="fy-row gap-1.5">
            <Icon of={Lock} />
            <span className="fy-small">{t.email === "off" ? "Not by email here" : `Email, ${cadenceText(t.email).toLowerCase()}`}</span>
          </span>
        ) : (
          <span className="fy-row gap-2">
            <label className="fy-small" htmlFor={`${id}-email`}>
              Email
            </label>
            <select className="fy-select h-8 w-auto text-sm" id={`${id}-email`} value={t.email} disabled={busy} aria-describedby={changed ? `${id}-rec` : undefined}
              onChange={(e) => save(e.target.value as Cadence)}>
              {cadences.map(([k, label]) => (
                <option key={k} value={k}>
                  {label}
                </option>
              ))}
            </select>
          </span>
        )}
        {changed && (
          <Button variant="ghost" size="sm" onClick={() => save(t.recommended)} disabled={busy} title={reset.tooltip}>
            <Icon of={RotateCcw} />
            Reset to recommended
          </Button>
        )}
      </div>
      {changed && (
        <span className="fy-small fy-muted" id={`${id}-rec`}>
          Recommended: {cadenceText(t.recommended).toLowerCase()}
        </span>
      )}
      {t.note && <span className="fy-small fy-muted">{t.note}</span>}
      {problem && <Refusal problem={problem} title={`${t.label} was not changed`} />}
    </li>
  );
}

function QuietHours({ from, to, etag, onSaved }: { from?: string; to?: string; etag: string; onSaved: () => void }) {
  const id = useId();
  const [on, setOn] = useState(Boolean(from));
  const [start, setStart] = useState(from ?? "22:00");
  const [end, setEnd] = useState(to ?? "07:00");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const h = helpText("notification.quiet-hours");
  useEffect(() => {
    setOn(Boolean(from));
    if (from) setStart(from);
    if (to) setEnd(to);
  }, [from, to]);
  const save = async (body: { from?: string; to?: string }) => {
    setBusy(true);
    setProblem(null);
    const r = await api.PUT("/v1/notification-quiet-hours", { params: { header: { "If-Match": etag, "Idempotency-Key": newKey() } }, body });
    setBusy(false);
    if (r.error) setProblem(problemOf(r.error));
    else onSaved();
  };
  return (
    <section className="fy-card" aria-labelledby={`${id}-title`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-title`}>
          {h.label}
        </h2>
        <span className="fy-small fy-muted">Singapore time</span>
      </div>
      <div className="fy-row fy-row--wrap fy-row--top gap-4 p-4">
        <div className="fy-stack fy-stack--2 flex-[1_1_220px]">
          <span className="fy-label" id={`${id}-switch`}>
            Hold email during quiet hours
          </span>
          <span className="fy-help" id={`${id}-help`}>
            Email that would go at once waits until they end. Security notices still go at once, and nothing waits in the app.
          </span>
          {on && (
            <div className="fy-row fy-row--wrap gap-2">
              <label className="fy-label" htmlFor={`${id}-from`}>
                From
              </label>
              <input className="fy-input fy-num w-28" type="time" id={`${id}-from`} value={start} onChange={(e) => setStart(e.target.value)} />
              <label className="fy-label" htmlFor={`${id}-to`}>
                until
              </label>
              <input className="fy-input fy-num w-28" type="time" id={`${id}-to`} value={end} onChange={(e) => setEnd(e.target.value)} />
              <Button size="sm" onClick={() => save({ from: start, to: end })} disabled={busy}>
                Save quiet hours
              </Button>
            </div>
          )}
        </div>
        <button className="fy-switch" type="button" role="switch" aria-checked={on} aria-labelledby={`${id}-switch`} aria-describedby={`${id}-help`} disabled={busy}
          onClick={() => {
            if (on && from) void save({});
            setOn(!on);
          }} />
      </div>
      {problem && (
        <div className="px-4 pb-4">
          <Refusal problem={problem} title="Quiet hours were not saved" />
        </div>
      )}
    </section>
  );
}

// Notifications is Settings › Notifications, each person's own (FR-NTF-02; docs/design/screens NotificationPreferences):
// how each type goes by email, against the recommendation (security and approvals at once, the rest in the daily
// digest at 08:00 Singapore time), with security notices always on; quiet hours; and a test notification to check the
// channels. In the app every notification always shows. Chat arrives with its connection (FR-NTF-04, P2).
export function Notifications() {
  const queries = useQueryClient();
  const q = useQuery({ queryKey: ["notification-preferences"], queryFn: readPreferences });
  const [sent, setSent] = useState<string | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const test = helpText("notification.test");
  const saved = () => queries.invalidateQueries({ queryKey: ["notification-preferences"] });
  const sendTest = async () => {
    setProblem(null);
    const r = await api.POST("/v1/notifications/test-messages", { params: { header: { "Idempotency-Key": newKey() } } });
    if (r.error) {
      setProblem(problemOf(r.error));
      return;
    }
    setSent(
      r.data.email === "off"
        ? "It shows in your notifications in a moment. It is not emailed: you have no verified sign-in email, or you turned test notifications off."
        : `It shows in your notifications in a moment, and goes by email ${r.data.email === "immediate" ? "at once" : `in your ${r.data.email} digest`}.`,
    );
    await queries.invalidateQueries({ queryKey: ["notifications"] });
  };
  const types = q.data?.types ?? [];
  return (
    <SettingsPage title="Notifications" screen="notification-preferences"
      lead="Choose how each kind of notification reaches you by email. In the app they always show; security notices can't be turned off."
      actions={
        <Button variant="secondary" onClick={sendTest} title={test.tooltip}>
          <Icon of={Send} />
          {test.label}
        </Button>
      }>
      {sent && (
        <div className="fy-banner fy-banner--good" role="status">
          <Icon of={CircleCheck} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">Test notification sent</span>
            <span className="fy-small">{sent}</span>
          </div>
        </div>
      )}
      {problem && <Refusal problem={problem} title="The test notification was not sent" />}
      {q.isError && <Refusal problem={problemOf(q.error)} title="Your preferences could not be read" />}
      {q.isPending && <p className="fy-secondary">Loading your preferences…</p>}
      {q.data && (
        <>
          {groups.map(([category, title]) => {
            const mine = types.filter((t) => t.category === category);
            return mine.length === 0 ? null : (
              <section key={category} className="fy-card" aria-labelledby={`grp-${category}`}>
                <div className="fy-card__head">
                  <h2 className="fy-h3" id={`grp-${category}`}>
                    {title}
                  </h2>
                </div>
                <ul aria-labelledby={`grp-${category}`}>
                  {mine.map((t) => (
                    <Row key={t.type} t={t} onSaved={saved} />
                  ))}
                </ul>
              </section>
            );
          })}
          <QuietHours from={q.data.quiet_hours?.from} to={q.data.quiet_hours?.to} etag={q.data.quiet_hours_etag} onSaved={saved} />
        </>
      )}
    </SettingsPage>
  );
}
