import { useQuery } from "@tanstack/react-query";
import { CircleCheck, TriangleAlert } from "lucide-react";
import { api } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf } from "../../ui";
import { SettingsPage } from "./SettingsPage";

// The super-admin console (FR-ADM-07; docs/design/screens SuperAdminConsole): the platform's health across every organisation,
// and the installation's Super admins. For a Super admin only; counts, times and names, never an organisation's record.

async function health() {
  const { data, error } = await api.GET("/v1/installation/health");
  if (error) throw error;
  return data;
}

async function superAdmins() {
  const { data, error } = await api.GET("/v1/installation/super-admins");
  if (error) throw error;
  return data;
}

export function AdminConsole() {
  const h = useQuery({ queryKey: ["installation-health"], queryFn: health, refetchInterval: 60_000 });
  const s = useQuery({ queryKey: ["installation-super-admins"], queryFn: superAdmins });
  const title = helpText("settings.console");
  return (
    <SettingsPage title={title.label} screen="super-admin-console" lead={title.tooltip}>
      <section className="fy-card" aria-labelledby="health-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="health-title">
            Platform health
          </h2>
          {h.data &&
            (h.data.status === "ok" ? (
              <span className="fy-chip fy-chip--good">
                <Icon of={CircleCheck} />
                Healthy
              </span>
            ) : (
              <span className="fy-chip fy-chip--serious">
                <Icon of={TriangleAlert} />
                Degraded
              </span>
            ))}
        </div>
        <div className="fy-card__body fy-stack fy-stack--3">
          {h.isError && <Refusal problem={problemOf(h.error)} title="The platform's health could not be read" />}
          {h.isPending && <p className="fy-secondary">Reading the platform's health…</p>}
          {h.data && (
            <>
              {h.data.reasons.length > 0 && (
                <ul className="fy-list" aria-label="Why it is degraded">
                  {h.data.reasons.map((r) => (
                    <li key={r} className="fy-list__item">
                      <span className="fy-list__icon fy-list__icon--warning">
                        <Icon of={TriangleAlert} size={20} />
                      </span>
                      <span className="fy-list__main">
                        <span className="fy-list__title whitespace-normal">{r}</span>
                      </span>
                    </li>
                  ))}
                </ul>
              )}
              <dl className="fy-dl">
                <div>
                  <dt>Background work waiting</dt>
                  <dd className="fy-num">{h.data.outbox.pending}</dd>
                </div>
                <div>
                  <dt>Background work that failed</dt>
                  <dd className="fy-num">{h.data.outbox.dead}</dd>
                </div>
                {h.data.outbox.oldest_pending && (
                  <div>
                    <dt>Oldest work waiting since</dt>
                    <dd>{dateTime(h.data.outbox.oldest_pending)}</dd>
                  </div>
                )}
              </dl>
              <table className="fy-table" aria-label="Platform jobs">
                <thead>
                  <tr>
                    <th scope="col">Job</th>
                    <th scope="col">Last ran</th>
                    <th scope="col">Reported</th>
                  </tr>
                </thead>
                <tbody>
                  {h.data.jobs.length === 0 && (
                    <tr>
                      <td colSpan={3}>No platform job has run yet.</td>
                    </tr>
                  )}
                  {h.data.jobs.map((j) => (
                    <tr key={j.name}>
                      <th scope="row" className="fy-mono">
                        {j.name}
                      </th>
                      <td>{dateTime(j.last_beat)}</td>
                      <td>{j.detail ?? ""}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          )}
        </div>
      </section>
      <section className="fy-card" aria-labelledby="admins-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="admins-title">
            Super admins
          </h2>
        </div>
        <div className="fy-card__body fy-stack fy-stack--3">
          {s.isError && <Refusal problem={problemOf(s.error)} title="The Super admins could not be listed" />}
          {s.data?.warning && (
            <p className="fy-banner fy-banner--warning" role="note">
              <Icon of={TriangleAlert} size={20} />
              {s.data.warning}
            </p>
          )}
          {s.data && (
            <ul className="fy-list" aria-labelledby="admins-title">
              {s.data.items.map((a) => (
                <li key={a.user_id} className="fy-list__item">
                  <span className="fy-list__main">
                    <span className="fy-list__title">{a.display_name}</span>
                    <span className="fy-list__meta">A Super admin since {date(a.added_at)}</span>
                  </span>
                </li>
              ))}
            </ul>
          )}
          <p className="fy-small fy-secondary">The last Super admin is only replaced, never removed; a second one keeps the installation recoverable.</p>
        </div>
      </section>
    </SettingsPage>
  );
}
