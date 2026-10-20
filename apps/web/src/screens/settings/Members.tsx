import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CalendarClock, Check, Copy, Hourglass, Mail, Plus, TriangleAlert, UserPlus, Users, X } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { roleLabel, type Approval } from "../approvals/words";
import { SettingsPage } from "./SettingsPage";

type Member = components["schemas"]["Member"];
type Invitation = components["schemas"]["MemberInvitation"];
type Conflict = components["schemas"]["TeamModeConflict"];

// The roles a member can hold (PRD-001 §6.2; internal/domain/member Assignable), in the order of
// §7.1's table, with what each mainly does.
export const assignable: [string, string][] = [
  ["owner", "Approves; always at least one"],
  ["administrator", "Users, roles, policies and settings"],
  ["controller", "Approves and keeps the defaults"],
  ["accountant", "Prepares and posts journals"],
  ["bookkeeper", "Records the day-to-day"],
  ["ar_specialist", "Invoices and collections"],
  ["ap_specialist", "Bills, prepared for approval"],
  ["payment_releaser", "Releases approved payments"],
  ["payroll_administrator", "Pay runs"],
  ["approver", "Approves within a limit"],
  ["employee", "Their own claims"],
  ["auditor", "Reads everything, changes nothing"],
  ["tax_agent", "Tax returns"],
  ["board_viewer", "Board reports"],
  ["company_secretary", "Board and ACRA matters"],
  ["dpo", "Personal data requests"],
];

async function listAll<T>(get: (cursor?: string) => Promise<{ items?: T[] | null; next_cursor?: string }>): Promise<T[]> {
  const out: T[] = [];
  let cursor: string | undefined;
  do {
    const page = await get(cursor);
    out.push(...(page.items ?? []));
    cursor = page.next_cursor;
  } while (cursor);
  return out;
}

async function members() {
  return listAll<Member>(async (cursor) => {
    const { data, error } = await api.GET("/v1/members", { params: { query: { page_size: 200, cursor } } });
    if (error) throw error;
    return data;
  });
}

async function invitations() {
  return listAll<Invitation>(async (cursor) => {
    const { data, error } = await api.GET("/v1/members/invitations", { params: { query: { page_size: 200, cursor } } });
    if (error) throw error;
    return data;
  });
}

async function waiting() {
  const { data, error } = await api.GET("/v1/approvals", { params: { query: { status: "pending", page_size: 200 } } });
  if (error) throw error;
  return (data.items ?? []).filter((a) => a.object_type === "membership" || a.object_type === "member_invitation");
}

async function conflicts() {
  const { data, error } = await api.GET("/v1/team-mode");
  if (error) return { blocks: false, conflicts: [] as Conflict[] }; // a role without V on team mode still sees the members
  return { blocks: data.blocks_conflicts, conflicts: data.conflicts ?? [] };
}

// endOfDay is the end of a chosen day in Singapore, as the API's date-time: the role lasts that day.
function endOfDay(day: string): string | undefined {
  return day ? `${day}T23:59:59+08:00` : undefined;
}

function Roles({ roles }: { roles: { role: string; expires_at?: string }[] }) {
  return (
    <span className="fy-row fy-row--wrap gap-1">
      {roles.map((r) => (
        <span key={r.role} className="fy-tag">
          {roleLabel(r.role)}
          {r.expires_at ? ` to ${date(r.expires_at)}` : ""}
        </span>
      ))}
    </span>
  );
}

function RolePicker({ picked, onChange, legend, describedBy }: { picked: Set<string>; onChange: (s: Set<string>) => void; legend: string; describedBy?: string }) {
  return (
    <fieldset className="fy-fieldset" aria-describedby={describedBy}>
      <legend className="fy-label">{legend}</legend>
      <div className="grid gap-2 medium:grid-cols-2">
        {assignable.map(([r, about]) => (
          <label key={r} className="fy-row fy-row--top gap-2">
            <input
              type="checkbox"
              className="mt-1"
              checked={picked.has(r)}
              onChange={() => {
                const n = new Set(picked);
                if (n.has(r)) n.delete(r);
                else n.add(r);
                onChange(n);
              }}
            />
            <span className="fy-stack gap-0">
              <span className="fy-strong">{roleLabel(r)}</span>
              <span className="fy-small fy-secondary">{about}</span>
            </span>
          </label>
        ))}
      </div>
    </fieldset>
  );
}

// Waiting says a change waits for another Owner, with the way to its request (FM-1).
function Waiting({ a }: { a: Pick<Approval, "id" | "summary"> }) {
  return (
    <div className="fy-banner fy-banner--info" role="status">
      <Icon of={Hourglass} size={20} />
      <div className="fy-banner__text">
        <span className="fy-strong">Waiting for another Owner's approval</span>
        <span className="fy-small">
          {a.summary}. Nothing changes until it is approved.{" "}
          <Link to="/approvals/$approvalId" params={{ approvalId: a.id }}>
            View the request
          </Link>
        </span>
      </div>
    </div>
  );
}

function CodeOnce({ code, waits }: { code: string; waits: boolean }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="fy-stack fy-stack--2">
      <div className="fy-banner fy-banner--good" role="status">
        <Icon of={Check} size={20} />
        <div className="fy-banner__text">
          <span className="fy-strong">{waits ? "Invitation made: it waits for another Owner's approval" : "Invitation made"}</span>
          <span className="fy-small">
            This code is shown once. Give it to the person in person or on a call. It works once, within 7 days{waits ? ", after the invitation is approved" : ""}.
          </span>
        </div>
      </div>
      <div className="fy-row fy-row--wrap gap-2">
        <code className="fy-mono text-lg break-all">{code}</code>
        <Button
          size="sm"
          onClick={() =>
            void navigator.clipboard.writeText(code).then(
              () => setCopied(true),
              () => setCopied(false),
            )
          }
        >
          <Icon of={copied ? Check : Copy} />
          {copied ? "Copied" : "Copy the code"}
        </Button>
      </div>
    </div>
  );
}

function Invite({ open, onClose }: { open: boolean; onClose: () => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const nameId = useId();
  const endId = useId();
  const [name, setName] = useState("");
  const [roles, setRoles] = useState<Set<string>>(new Set());
  const [ends, setEnds] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [errors, setErrors] = useState<{ name?: string; roles?: string; ends?: string }>({});
  const [made, setMade] = useState<{ code: string; waits: boolean } | null>(null);

  const close = () => {
    setName("");
    setRoles(new Set());
    setEnds("");
    setProblem(null);
    setErrors({});
    setMade(null);
    onClose();
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const errs: typeof errors = {};
    if (!name.trim() || name.trim().length > 200) errs.name = "Enter the person's name, up to 200 characters.";
    if (roles.size === 0) errs.roles = "Choose at least one role.";
    if (ends && roles.has("owner")) errs.ends = "The Owner role never ends: leave the date empty, or invite the Owner separately.";
    setErrors(errs);
    if (Object.keys(errs).length) return;
    setBusy(true);
    setProblem(null);
    const body = { display_name: name.trim(), roles: assignable.map(([r]) => r).filter((r) => roles.has(r)), ...(ends ? { roles_expire_at: endOfDay(ends) } : {}) };
    const out = await guarded(confirm, () => api.POST("/v1/members/invitations", { body }));
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    const waits = out.response.status === 202;
    const code = "code" in out.data ? out.data.code : (out.data as Approval).invitation_code;
    setMade({ code: code ?? "", waits });
    await Promise.all(["invitations", "approvals", "team-mode"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && close()}
      dismissible={!busy}
      wide
      eyebrow="Members and roles"
      title={made ? "Give them this code" : "Invite someone"}
      icon={
        <span className="fy-list__icon fy-list__icon--brand">
          <Icon of={UserPlus} size={20} />
        </span>
      }
    >
      {made ? (
        <>
          <CodeOnce code={made.code} waits={made.waits} />
          <div className="fy-row justify-end">
            <Button variant="primary" onClick={close}>
              Done
            </Button>
          </div>
        </>
      ) : (
        <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
          {problem && <Refusal problem={problem} title="The invitation was not made" />}
          <div className="fy-field">
            <label className="fy-label" htmlFor={nameId}>
              Name <span className="fy-req">*</span>
            </label>
            <input className="fy-input" id={nameId} value={name} maxLength={200} autoComplete="off" onChange={(e) => setName(e.target.value)} aria-invalid={errors.name ? true : undefined} aria-describedby={errors.name ? `${nameId}-error` : undefined} />
            {errors.name && (
              <span className="fy-error" id={`${nameId}-error`}>
                {errors.name}
              </span>
            )}
          </div>
          <RolePicker picked={roles} onChange={setRoles} legend="Roles" describedBy={errors.roles ? `${nameId}-roles-error` : undefined} />
          {errors.roles && (
            <span className="fy-error" id={`${nameId}-roles-error`}>
              {errors.roles}
            </span>
          )}
          <div className="fy-field">
            <label className="fy-label" htmlFor={endId}>
              Roles end on (optional)
            </label>
            <input className="fy-input max-w-60" id={endId} type="date" value={ends} onChange={(e) => setEnds(e.target.value)} aria-invalid={errors.ends ? true : undefined} aria-describedby={`${endId}-help${errors.ends ? ` ${endId}-error` : ""}`} />
            <span className="fy-help" id={`${endId}-help`}>
              For someone from outside the company, such as an auditor. Leave it empty for a member who stays.
            </span>
            {errors.ends && (
              <span className="fy-error" id={`${endId}-error`}>
                {errors.ends}
              </span>
            )}
          </div>
          <div className="fy-row fy-row--wrap justify-end">
            <Button variant="ghost" type="button" onClick={close} disabled={busy}>
              Cancel
            </Button>
            <Button variant="primary" type="submit" disabled={busy}>
              <Icon of={Mail} />
              {busy ? "Making the invitation…" : "Create invitation"}
            </Button>
          </div>
        </form>
      )}
    </Dialog>
  );
}

function MemberRoles({ member, conflict, blocks, onClose }: { member: Member | null; conflict?: Conflict; blocks: boolean; onClose: () => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const roleId = useId();
  const endId = useId();
  const whyId = useId();
  const [role, setRole] = useState("");
  const [ends, setEnds] = useState("");
  const [why, setWhy] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [waits, setWaits] = useState<Approval | null>(null);
  const [done, setDone] = useState("");
  const add = helpText("member.add-role");
  const end = helpText("member.end-role");
  const held = new Set((member?.roles ?? []).map((r) => r.role));

  const close = () => {
    setRole("");
    setEnds("");
    setWhy("");
    setProblem(null);
    setWaits(null);
    setDone("");
    onClose();
  };

  const after = async (out: { data?: unknown; error?: unknown; response: Response }, what: string) => {
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    if (out.response.status === 202) setWaits(out.data as Approval);
    else setDone(what);
    setRole("");
    setEnds("");
    setWhy("");
    await Promise.all(["members", "approvals", "team-mode", "shell"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
  };

  const give = async (e: FormEvent) => {
    e.preventDefault();
    if (!member || !role) return;
    setBusy(true);
    setProblem(null);
    setDone("");
    const key = newKey();
    const body = { role, ...(ends ? { expires_at: endOfDay(ends) } : {}), ...(why.trim() ? { sod_override_reason: why.trim() } : {}) };
    const out = await guarded(confirm, () =>
      api.POST("/v1/members/{member_id}/roles", { params: { path: { member_id: member.id }, header: { "Idempotency-Key": key } }, body }),
    );
    setBusy(false);
    await after(out, `${roleLabel(role)} added.`);
  };

  const remove = async (r: string) => {
    if (!member) return;
    setBusy(true);
    setProblem(null);
    setDone("");
    const out = await guarded(confirm, () => api.DELETE("/v1/members/{member_id}/roles/{role}", { params: { path: { member_id: member.id, role: r } } }));
    setBusy(false);
    await after(out, `${roleLabel(r)} ended.`);
  };

  return (
    <Dialog
      open={member !== null}
      onOpenChange={(o) => !o && close()}
      dismissible={!busy}
      wide
      eyebrow="Members and roles"
      title={member ? `${member.display_name}'s roles` : "Roles"}
      description="A change takes effect on their next page. Once the company has a second member, it waits for another Owner's approval."
    >
      {problem && <Refusal problem={problem} title="The role was not changed" />}
      {waits && <Waiting a={waits} />}
      <p className="fy-small" role="status">
        {done}
      </p>
      {conflict && (
        <div className={blocks ? "fy-banner fy-banner--warning" : "fy-banner fy-banner--info"} role="note">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">Role conflict ({conflict.conflict})</span>
            <span className="fy-small">
              {conflict.roles?.map(roleLabel).join(" and ")} together let one person administer users and post or approve transactions.
              {blocks ? (conflict.overridden ? " Kept with a reason." : " This team mode blocks it unless overridden with a reason.") : " Founder mode allows it, with the weekly review."}
            </span>
          </div>
        </div>
      )}
      <ul className="fy-list rounded-md border border-border" aria-label="Their roles">
        {(member?.roles ?? []).map((r) => (
          <li key={r.role} className="fy-list__item">
            <span className="fy-list__main">
              <span className="fy-list__title">{roleLabel(r.role)}</span>
              <span className="fy-list__meta">{r.expires_at ? `Ends ${date(r.expires_at)}` : "Until it is ended"}</span>
            </span>
            <Button variant="ghost" size="sm" onClick={() => void remove(r.role)} disabled={busy} aria-label={`${end.label}: ${roleLabel(r.role)}`} title={end.tooltip}>
              <Icon of={X} />
              {end.label}
            </Button>
          </li>
        ))}
      </ul>
      <form className="fy-stack fy-stack--3" onSubmit={give} noValidate>
        <div className="fy-form fy-form--2">
          <div className="fy-field">
            <label className="fy-label" htmlFor={roleId}>
              Role to add
            </label>
            <select className="fy-select" id={roleId} value={role} onChange={(e) => setRole(e.target.value)}>
              <option value="">Choose a role</option>
              {assignable
                .filter(([r]) => !held.has(r))
                .map(([r, about]) => (
                  <option key={r} value={r}>
                    {roleLabel(r)}: {about}
                  </option>
                ))}
            </select>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={endId}>
              Ends on (optional)
            </label>
            <input className="fy-input" id={endId} type="date" value={ends} onChange={(e) => setEnds(e.target.value)} aria-describedby={`${endId}-help`} />
            <span className="fy-help" id={`${endId}-help`}>
              For someone from outside the company. Never on the Owner role.
            </span>
          </div>
        </div>
        {blocks && (
          <div className="fy-field">
            <label className="fy-label" htmlFor={whyId}>
              Why they keep a conflict (only if this role makes one)
            </label>
            <textarea className="fy-textarea" id={whyId} value={why} maxLength={500} onChange={(e) => setWhy(e.target.value)} aria-describedby={`${whyId}-help`} />
            <span className="fy-help" id={`${whyId}-help`}>
              Standard mode blocks a role that lets one person administer users and post or approve transactions, unless you give a reason the approver accepts.
            </span>
          </div>
        )}
        <div className="fy-row justify-end">
          <Button variant="primary" type="submit" disabled={busy || !role} title={add.tooltip}>
            <Icon of={Plus} />
            {add.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function WithdrawInvitation({ target, onClose }: { target: Invitation | null; onClose: () => void }) {
  const queries = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("member.withdraw-invitation");
  const run = async () => {
    if (!target) return;
    setBusy(true);
    setProblem(null);
    const out = await api.DELETE("/v1/members/invitations/{invitation_id}", { params: { path: { invitation_id: target.id } } });
    setBusy(false);
    if (out.error) {
      setProblem(problemOf(out.error));
      return;
    }
    await Promise.all(["invitations", "team-mode"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
    onClose();
  };
  return (
    <Dialog
      open={target !== null}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={target ? `Withdraw ${target.display_name}'s invitation?` : "Withdraw the invitation?"}
      description="Its code stops working at once. You can invite them again later."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Keep it
          </Button>
          <Button variant="destructive" onClick={run} disabled={busy}>
            <Icon of={X} />
            {h.label}
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The invitation was not withdrawn" />}
    </Dialog>
  );
}

// Members is Settings › Members and roles (docs/design/screens UsersRoles; FR-ADM-01, FM-1, §6.3):
// everyone in the company with their roles and any role conflict, the invitations not yet used, and
// the role changes waiting for another Owner. Inviting and changing roles ask the API, which decides
// whether the change is made at once (the founder bootstrap) or waits for approval.
export function Members() {
  const { session } = useSignedIn();
  const list = useQuery({ queryKey: ["members"], queryFn: members });
  const invites = useQuery({ queryKey: ["invitations"], queryFn: invitations });
  const pending = useQuery({ queryKey: ["approvals", "members"], queryFn: waiting });
  const team = useQuery({ queryKey: ["team-mode", "conflicts"], queryFn: conflicts });
  const [inviting, setInviting] = useState(false);
  const [editing, setEditing] = useState<Member | null>(null);
  const [withdrawing, setWithdrawing] = useState<Invitation | null>(null);
  const invite = helpText("member.invite");
  const change = helpText("member.change-roles");
  const withdraw = helpText("member.withdraw-invitation");
  const people = list.data ?? [];
  const conflictOf = (m: Member) => team.data?.conflicts.find((c) => c.member_id === m.id);
  const alone = people.length <= 1;

  return (
    <SettingsPage
      title="Members and roles" screen="users-roles"
      lead={
        list.data
          ? `${people.length} ${people.length === 1 ? "person" : "people"}. ${alone ? "Until a second person joins, your role changes are made at once and logged for review." : "Role changes wait for another Owner's approval."}`
          : undefined
      }
      actions={
        <Button variant="primary" onClick={() => setInviting(true)} title={invite.tooltip}>
          <Icon of={UserPlus} />
          {invite.label}
        </Button>
      }
    >
      {(pending.data ?? []).map((a) => (
        <Waiting key={a.id} a={a} />
      ))}
      {list.isError && <Refusal problem={problemOf(list.error)} title="The members could not be listed" />}
      <section className="fy-card" aria-labelledby="people-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="people-title">
            People
          </h2>
          <span className="fy-badge fy-badge--quiet">{people.length}</span>
        </div>
        {list.isPending && <p className="fy-card__body fy-secondary">Loading the members…</p>}
        {list.data && (
          <ul className="fy-list fy-stagger" aria-labelledby="people-title">
            {people.map((m) => {
              const c = conflictOf(m);
              const you = m.user_id === session.user_id;
              return (
                <li key={m.id} className="fy-list__item">
                  <span className="fy-avatar fy-avatar--sm" aria-hidden="true">
                    {m.display_name
                      .split(/\s+/)
                      .map((w) => w[0])
                      .join("")
                      .slice(0, 2)
                      .toUpperCase()}
                  </span>
                  <span className="fy-list__main">
                    <span className="fy-list__title whitespace-normal">
                      {m.display_name}
                      {you ? " (you)" : ""}
                    </span>
                    <Roles roles={m.roles ?? []} />
                  </span>
                  {c && (
                    <span className={team.data?.blocks && !c.overridden ? "fy-chip fy-chip--warning" : "fy-chip fy-chip--neutral"} title={`${c.conflict}: ${c.roles?.map(roleLabel).join(" and ")}`}>
                      <Icon of={TriangleAlert} />
                      Role conflict
                    </span>
                  )}
                  <Button size="sm" onClick={() => setEditing(m)} aria-label={`${change.label}: ${m.display_name}`} title={change.tooltip}>
                    <Icon of={Users} />
                    <span className="fy-from-medium">{change.label}</span>
                  </Button>
                </li>
              );
            })}
          </ul>
        )}
      </section>
      <section className="fy-card" aria-labelledby="invites-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="invites-title">
            Invitations not yet used
          </h2>
          <span className="fy-badge fy-badge--quiet">{invites.data?.length ?? 0}</span>
        </div>
        {invites.isError && <Refusal problem={problemOf(invites.error)} title="The invitations could not be listed" />}
        {invites.data && invites.data.length === 0 && <p className="fy-card__body fy-small fy-secondary">None. An invitation's code works once, within 7 days.</p>}
        {invites.data && invites.data.length > 0 && (
          <ul className="fy-list" aria-labelledby="invites-title">
            {invites.data.map((i) => (
              <li key={i.id} className="fy-list__item">
                <span className="fy-list__icon">
                  <Icon of={Mail} size={20} />
                </span>
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">{i.display_name}</span>
                  <Roles roles={(i.roles ?? []).map((role) => ({ role, expires_at: i.roles_expire_at }))} />
                </span>
                <span className="fy-chip fy-chip--neutral">
                  <Icon of={CalendarClock} />
                  Until {date(i.expires_at)}
                </span>
                <Button variant="ghost" size="sm" onClick={() => setWithdrawing(i)} aria-label={`${withdraw.label}: ${i.display_name}`} title={withdraw.tooltip}>
                  <Icon of={X} />
                  <span className="fy-from-medium">{withdraw.label}</span>
                </Button>
              </li>
            ))}
          </ul>
        )}
      </section>
      <Invite open={inviting} onClose={() => setInviting(false)} />
      <MemberRoles member={editing ? (people.find((p) => p.id === editing.id) ?? editing) : null} conflict={editing ? conflictOf(editing) : undefined} blocks={team.data?.blocks ?? false} onClose={() => setEditing(null)} />
      <WithdrawInvitation target={withdrawing} onClose={() => setWithdrawing(null)} />
    </SettingsPage>
  );
}
