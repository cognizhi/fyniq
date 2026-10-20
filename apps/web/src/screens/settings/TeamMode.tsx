import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, Hourglass, ShieldCheck, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent, type KeyboardEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { roleLabel, type Approval } from "../approvals/words";
import { SettingsPage } from "./SettingsPage";

type Team = components["schemas"]["TeamMode"];
type Mode = Team["mode"];
type Conflict = components["schemas"]["TeamModeConflict"];

const modes: { mode: Mode; label: string; about: string; available: boolean }[] = [
  { mode: "founder", label: "Founder", about: "A small team of founders. One person may hold roles that conflict; another founder approves every high-risk change, and reviews the log each week.", available: true },
  { mode: "standard", label: "Standard", about: "A team with finance staff. Roles that conflict are blocked, unless a reason is given and another person approves it.", available: true },
  { mode: "enterprise", label: "Enterprise", about: "Larger companies: approval matrices per entity and department, single sign-on and access reviews.", available: false },
];

async function getTeam() {
  const r = await api.GET("/v1/team-mode");
  if (r.error) throw r.error;
  return { team: r.data, etag: etagOf(r.response) || `"${r.data.version}"` };
}

async function switchWaiting() {
  const { data, error } = await api.GET("/v1/approvals", { params: { query: { status: "pending", page_size: 200 } } });
  if (error) throw error;
  return (data.items ?? []).find((a) => a.object_type === "team_mode") ?? null; // a query never answers undefined
}

// ModeCards is the mode as a radio group of cards: arrow keys move between the modes available.
function ModeCards({ current, chosen, onChoose }: { current: Mode; chosen: Mode; onChoose: (m: Mode) => void }) {
  const open = modes.filter((m) => m.available).map((m) => m.mode);
  const move = (e: KeyboardEvent<HTMLButtonElement>, m: Mode) => {
    const step = e.key === "ArrowRight" || e.key === "ArrowDown" ? 1 : e.key === "ArrowLeft" || e.key === "ArrowUp" ? -1 : 0;
    if (!step) return;
    e.preventDefault();
    const next = open[(open.indexOf(m) + step + open.length) % open.length] ?? m;
    onChoose(next);
    (e.currentTarget.parentElement?.querySelector(`[data-mode="${next}"]`) as HTMLElement | null)?.focus();
  };
  return (
    <div className="fy-row fy-row--wrap fy-row--top items-stretch gap-3" role="radiogroup" aria-labelledby="modes-title">
      {modes.map((m) => (
        <button
          key={m.mode}
          type="button"
          role="radio"
          data-mode={m.mode}
          className="fy-choice w-auto flex-[1_1_220px]"
          aria-checked={chosen === m.mode}
          aria-disabled={!m.available || undefined}
          tabIndex={chosen === m.mode ? 0 : -1}
          onClick={() => m.available && onChoose(m.mode)}
          onKeyDown={(e) => move(e, m.mode)}
        >
          <span className="fy-radio" aria-hidden="true" />
          <span className="fy-stack fy-stack--2">
            <span className="fy-row fy-row--wrap">
              <span className="fy-strong">{m.label}</span>
              {current === m.mode && (
                <span className="fy-chip fy-chip--info">
                  <Icon of={CircleCheck} />
                  Current
                </span>
              )}
              {!m.available && <span className="fy-tag">Not available yet</span>}
            </span>
            <span className="fy-small fy-secondary">{m.about}</span>
          </span>
        </button>
      ))}
    </div>
  );
}

function ConflictList({ conflicts, blocks }: { conflicts: Conflict[]; blocks: boolean }) {
  if (conflicts.length === 0) return <p className="fy-small fy-secondary">Nobody's roles conflict now.</p>;
  return (
    <ul className="fy-list rounded-md border border-border" aria-label="Role conflicts">
      {conflicts.map((c) => (
        <li key={`${c.member_id ?? c.invitation_id}-${c.conflict}`} className="fy-list__item">
          <span className="fy-list__icon fy-list__icon--warning">
            <Icon of={TriangleAlert} size={20} />
          </span>
          <span className="fy-list__main">
            <span className="fy-list__title whitespace-normal">
              {c.display_name}
              {c.invitation_id ? " (invitation)" : ""}
            </span>
            <span className="fy-list__meta whitespace-normal">
              {c.roles?.map(roleLabel).join(" and ")} · {c.conflict}
            </span>
          </span>
          {c.overridden ? (
            <span className="fy-chip fy-chip--neutral">Kept with a reason</span>
          ) : blocks ? (
            <span className="fy-chip fy-chip--warning">
              <Icon of={TriangleAlert} />
              Blocked
            </span>
          ) : (
            <span className="fy-chip fy-chip--neutral">Allowed in Founder mode</span>
          )}
        </li>
      ))}
    </ul>
  );
}

function SwitchForm({ team, etag, to, onAsked }: { team: Team; etag: string; to: Mode; onAsked: (a: Approval) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const formId = useId();
  const [reasons, setReasons] = useState<Record<string, string>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const ask = helpText("team-mode.switch");
  const label = modes.find((m) => m.mode === to)?.label ?? to;
  const blocking = to === "standard";
  const members = blocking ? (team.conflicts ?? []).filter((c) => c.member_id) : [];
  const invites = blocking ? (team.conflicts ?? []).filter((c) => c.invitation_id) : [];
  const keyOf = (c: Conflict) => `${c.member_id}:${c.conflict}`;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const errs: Record<string, string> = {};
    for (const c of members) {
      const r = (reasons[keyOf(c)] ?? "").trim();
      if (!r || r.length > 500) errs[keyOf(c)] = `Give the reason ${c.display_name} keeps both roles, in up to 500 characters, or end one of their roles first.`;
    }
    setErrors(errs);
    if (Object.keys(errs).length || invites.length) return;
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const body = { mode: to, ...(members.length ? { overrides: members.map((c) => ({ member_id: c.member_id ?? "", conflict: c.conflict, reason: (reasons[keyOf(c)] ?? "").trim() })) } : {}) };
    const out = await guarded(confirm, () => api.PATCH("/v1/team-mode", { params: { header: { "If-Match": etag, "Idempotency-Key": key } }, body }));
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    await Promise.all(["team-mode", "approvals"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
    onAsked(out.data as unknown as Approval);
  };

  return (
    <section className="fy-card" aria-labelledby={`${formId}-title`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${formId}-title`}>
          Switch to {label} mode
        </h2>
      </div>
      <form className="fy-card__body fy-stack fy-stack--3" onSubmit={submit} noValidate>
        {problem && <Refusal problem={problem} title="The switch was not asked for" />}
        <p className="fy-small fy-secondary">
          The switch waits for an Owner who did not ask for it. When it is approved, every other change still waiting for approval is cancelled, because it was asked for under the old mode.
        </p>
        {invites.length > 0 && (
          <div className="fy-banner fy-banner--warning" role="alert">
            <Icon of={TriangleAlert} size={20} />
            <div className="fy-banner__text">
              <span className="fy-strong">Withdraw {invites.length === 1 ? "an invitation" : `${invites.length} invitations`} first</span>
              <span className="fy-small">
                {invites.map((c) => c.display_name).join(", ")}: an invitation cannot give roles that conflict in {label} mode. Withdraw it in <Link to="/settings/members">Members and roles</Link>, and invite again with one role.
              </span>
            </div>
          </div>
        )}
        {members.map((c) => {
          const id = `${formId}-${c.member_id}`;
          const err = errors[keyOf(c)];
          return (
            <div key={keyOf(c)} className="fy-field">
              <label className="fy-label" htmlFor={id}>
                Why {c.display_name} keeps {c.roles?.map(roleLabel).join(" and ")} <span className="fy-req">*</span>
              </label>
              <textarea
                className="fy-textarea"
                id={id}
                value={reasons[keyOf(c)] ?? ""}
                maxLength={500}
                onChange={(e) => setReasons({ ...reasons, [keyOf(c)]: e.target.value })}
                aria-invalid={err ? true : undefined}
                aria-describedby={`${id}-help${err ? ` ${id}-error` : ""}`}
              />
              <span className="fy-help" id={`${id}-help`}>
                {c.conflict}: the approver accepts this reason, and it is kept with the mode. Or end one of the roles in Members and roles first.
              </span>
              {err && (
                <span className="fy-error" id={`${id}-error`}>
                  {err}
                </span>
              )}
            </div>
          );
        })}
        <div className="fy-row justify-end">
          <Button variant="primary" type="submit" disabled={busy || invites.length > 0} title={ask.tooltip}>
            <Icon of={ShieldCheck} />
            {busy ? "Asking…" : `Ask to switch to ${label}`}
          </Button>
        </div>
      </form>
    </section>
  );
}

// TeamMode is Settings › Team mode (docs/design/screens TeamMode; §6.3, §6.4, FR-ADM-04, rev 0.42): the
// mode, the conflicts members' roles hold now, the reasons kept for them, and the switch, which an
// Owner who did not ask for it approves. Founder mode's other controls (the close attestation, the
// accountant's review, break-glass) join this page as they are built.
export function TeamMode() {
  const q = useQuery({ queryKey: ["team-mode"], queryFn: getTeam });
  const pending = useQuery({ queryKey: ["approvals", "team-mode"], queryFn: switchWaiting });
  const [chosen, setChosen] = useState<Mode | null>(null);
  const [asked, setAsked] = useState<Approval | null>(null);
  const team = q.data?.team;
  const waiting = asked ?? pending.data;
  const to = chosen ?? team?.mode ?? "founder";

  return (
    <SettingsPage title="Team mode" lead="How strictly duties are separated, and the safeguards that make up for a small team.">
      {q.isError && <Refusal problem={problemOf(q.error)} title="The team mode could not be shown" />}
      {q.isPending && <p className="fy-secondary">Loading the team mode…</p>}
      {waiting && (
        <div className="fy-banner fy-banner--info" role="status">
          <Icon of={Hourglass} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">Waiting for approval</span>
            <span className="fy-small">
              {waiting.summary}. The mode has not changed yet.{" "}
              <Link to="/approvals/$approvalId" params={{ approvalId: waiting.id }}>
                View the request
              </Link>
            </span>
          </div>
        </div>
      )}
      {team && q.data && (
        <>
          <section className="fy-stack fy-stack--3" aria-labelledby="modes-title">
            <h2 className="fy-h2" id="modes-title">
              Mode
            </h2>
            <ModeCards current={team.mode} chosen={to} onChoose={setChosen} />
          </section>
          {to !== team.mode && !waiting && (
            <SwitchForm
              team={team}
              etag={q.data.etag}
              to={to}
              onAsked={(a) => {
                setAsked(a);
                setChosen(null);
              }}
            />
          )}
          <section className="fy-card" aria-labelledby="conflicts-title">
            <div className="fy-card__head">
              <h2 className="fy-h3" id="conflicts-title">
                Roles that conflict now
              </h2>
              <span className="fy-badge fy-badge--quiet">{team.conflicts?.length ?? 0}</span>
            </div>
            <div className="fy-card__body fy-stack fy-stack--2">
              <p className="fy-small fy-secondary">The Administrator role together with a role that posts, approves or releases money (SOD-6). The Owner role alone is not a conflict.</p>
              <ConflictList conflicts={team.conflicts ?? []} blocks={team.blocks_conflicts} />
            </div>
          </section>
          {(team.overrides ?? []).length > 0 && (
            <section className="fy-card" aria-labelledby="overrides-title">
              <div className="fy-card__head">
                <h2 className="fy-h3" id="overrides-title">
                  Reasons kept in this mode
                </h2>
              </div>
              <ul className="fy-list" aria-labelledby="overrides-title">
                {(team.overrides ?? []).map((o) => (
                  <li key={`${o.user_id}-${o.conflict}`} className="fy-list__item">
                    <span className="fy-list__main">
                      <span className="fy-list__title whitespace-normal">
                        {o.display_name} · {o.conflict}
                      </span>
                      <span className="fy-list__meta whitespace-normal">
                        "{o.reason}" · accepted {date(o.created_at)}
                      </span>
                    </span>
                    <Link className="fy-small" to="/approvals/$approvalId" params={{ approvalId: o.approval_id }}>
                      The approval
                    </Link>
                  </li>
                ))}
              </ul>
            </section>
          )}
          <section className="fy-card" aria-labelledby="history-title">
            <div className="fy-card__head">
              <h2 className="fy-h3" id="history-title">
                History
              </h2>
            </div>
            {(team.history ?? []).length === 0 ? (
              <p className="fy-card__body fy-small fy-secondary">Founder mode since the company was set up; it has never been switched.</p>
            ) : (
              <ol className="fy-list" aria-labelledby="history-title">
                {(team.history ?? []).map((h) => (
                  <li key={h.version} className="fy-list__item">
                    <span className="fy-list__main">
                      <span className="fy-list__title">
                        {modes.find((m) => m.mode === h.mode)?.label ?? h.mode} mode, version {h.version}
                      </span>
                      <span className="fy-list__meta">Since {dateTime(h.created_at)}</span>
                    </span>
                    <Link className="fy-small" to="/approvals/$approvalId" params={{ approvalId: h.approval_id }}>
                      The approval
                    </Link>
                  </li>
                ))}
              </ol>
            )}
          </section>
        </>
      )}
    </SettingsPage>
  );
}
