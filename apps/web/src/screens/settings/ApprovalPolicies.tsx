import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Hourglass, Lock, Pencil, Plus, Stamp, Trash2 } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { objectLabel, roleLabel, stepWho, type Approval } from "../approvals/words";
import { assignable } from "./Members";
import { SettingsPage } from "./SettingsPage";

type Policy = components["schemas"]["ApprovalPolicy"];
type Member = components["schemas"]["Member"];
type Who = "anyone" | "roles" | "users";
interface Step {
  quorum: number;
  who: Who;
  roles: string[];
  users: string[];
}

async function listPolicies() {
  const { data, error } = await api.GET("/v1/approval-policies", { params: { query: { page_size: 100 } } });
  if (error) throw error;
  return data.items ?? [];
}

async function waitingChanges() {
  const { data, error } = await api.GET("/v1/approvals", { params: { query: { status: "pending", page_size: 200 } } });
  if (error) throw error;
  return (data.items ?? []).filter((a) => a.object_type === "approval_policy");
}

async function listMembers() {
  const { data, error } = await api.GET("/v1/members", { params: { query: { page_size: 200 } } });
  if (error) return [] as Member[];
  return data.items ?? [];
}

function stepsOf(p: { steps?: Policy["steps"] }): Step[] {
  return (p.steps ?? []).map((s) => ({
    quorum: s.quorum,
    who: (s.users ?? []).length ? "users" : (s.roles ?? []).length ? "roles" : "anyone",
    roles: s.roles ?? [],
    users: s.users ?? [],
  }));
}

function StepEditor({ n, step, members, onChange, onRemove }: { n: number; step: Step; members: Member[]; onChange: (s: Step) => void; onRemove?: () => void }) {
  const id = useId();
  const toggle = (list: string[], v: string) => (list.includes(v) ? list.filter((x) => x !== v) : [...list, v]);
  return (
    <div className="fy-card fy-card--flat fy-card--pad">
      <fieldset className="fy-fieldset">
        <legend className="fy-strong">Step {n}</legend>
        <div className="fy-form fy-form--2">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-q`}>
              Approvals needed
            </label>
            <select className="fy-select" id={`${id}-q`} value={step.quorum} onChange={(e) => onChange({ ...step, quorum: Number(e.target.value) })}>
              {[1, 2, 3, 4, 5].map((q) => (
                <option key={q} value={q}>
                  {q}
                </option>
              ))}
            </select>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-w`}>
              Who may approve
            </label>
            <select className="fy-select" id={`${id}-w`} value={step.who} onChange={(e) => onChange({ ...step, who: e.target.value as Who })}>
              <option value="anyone">Anyone who may approve this kind of change</option>
              <option value="roles">Only people with some roles</option>
              <option value="users">Only some named members</option>
            </select>
          </div>
        </div>
        {step.who === "roles" && (
          <div className="grid gap-2 medium:grid-cols-3" role="group" aria-label={`Step ${n}: roles`}>
            {assignable.map(([r]) => (
              <label key={r} className="fy-row gap-2">
                <input type="checkbox" checked={step.roles.includes(r)} onChange={() => onChange({ ...step, roles: toggle(step.roles, r) })} />
                {roleLabel(r)}
              </label>
            ))}
          </div>
        )}
        {step.who === "users" && (
          <div className="grid gap-2 medium:grid-cols-2" role="group" aria-label={`Step ${n}: members`}>
            {members.map((m) => (
              <label key={m.user_id} className="fy-row gap-2">
                <input type="checkbox" checked={step.users.includes(m.user_id)} onChange={() => onChange({ ...step, users: toggle(step.users, m.user_id) })} />
                {m.display_name}
              </label>
            ))}
          </div>
        )}
        {onRemove && (
          <div>
            <Button variant="ghost" size="sm" type="button" onClick={onRemove}>
              <Icon of={Trash2} />
              Remove step {n}
            </Button>
          </div>
        )}
      </fieldset>
    </div>
  );
}

function ChangePolicy({ policy, onClose, onAsked }: { policy: Policy | null; onClose: () => void; onAsked: (a: Approval) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const detail = useQuery({
    queryKey: ["approval-policy", policy?.object_type],
    enabled: policy !== null,
    queryFn: async () => {
      const r = await api.GET("/v1/approval-policies/{object_type}", { params: { path: { object_type: policy?.object_type ?? "organisation_setting" } } });
      if (r.error) throw r.error;
      return { policy: r.data, etag: etagOf(r.response) || `"${r.data.number}"` };
    },
  });
  const members = useQuery({ queryKey: ["members", "policies"], queryFn: listMembers, enabled: policy !== null });
  const [steps, setSteps] = useState<Step[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [error, setError] = useState("");
  const current = steps ?? (detail.data ? stepsOf(detail.data.policy) : []);
  const ask = helpText("approval-policy.change");

  const close = () => {
    setSteps(null);
    setProblem(null);
    setError("");
    onClose();
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!policy || !detail.data) return;
    const empty = current.findIndex((s) => (s.who === "roles" && s.roles.length === 0) || (s.who === "users" && s.users.length === 0));
    if (empty >= 0) {
      setError(`Step ${empty + 1}: choose at least one ${current[empty]?.who === "roles" ? "role" : "member"}, or let anyone who may approve sign it.`);
      return;
    }
    setError("");
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const body = {
      steps: current.map((s) => ({ quorum: s.quorum, ...(s.who === "roles" ? { roles: s.roles } : {}), ...(s.who === "users" ? { users: s.users } : {}) })),
    };
    const etag = detail.data.etag;
    const out = await guarded(confirm, () =>
      api.PATCH("/v1/approval-policies/{object_type}", { params: { path: { object_type: policy.object_type }, header: { "If-Match": etag, "Idempotency-Key": key } }, body }),
    );
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    await Promise.all(["approval-policies", "approval-policy", "approvals"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
    onAsked(out.data as unknown as Approval);
    close();
  };

  return (
    <Dialog
      open={policy !== null}
      onOpenChange={(o) => !o && close()}
      dismissible={!busy}
      wide
      eyebrow="Approval policies"
      title={policy ? `Who approves: ${objectLabel(policy.object_type)}` : "Change the policy"}
      description="Steps are taken in order: step 2 starts when step 1 has all its approvals. A policy only narrows who may approve; it never adds anyone."
    >
      {detail.isError && <Refusal problem={problemOf(detail.error)} title="The policy could not be read" />}
      {problem && <Refusal problem={problem} title="The change was not asked for" />}
      {detail.data && (
        <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
          {current.map((s, i) => (
            <StepEditor
              key={i}
              n={i + 1}
              step={s}
              members={members.data ?? []}
              onChange={(next) => setSteps(current.map((x, j) => (j === i ? next : x)))}
              onRemove={current.length > 1 ? () => setSteps(current.filter((_, j) => j !== i)) : undefined}
            />
          ))}
          {error && (
            <p className="fy-error" role="alert">
              {error}
            </p>
          )}
          <div className="fy-row fy-row--wrap justify-between">
            <Button type="button" size="sm" disabled={current.length >= 5} onClick={() => setSteps([...current, { quorum: 1, who: "anyone", roles: [], users: [] }])}>
              <Icon of={Plus} />
              Add a step
            </Button>
            <Button variant="primary" type="submit" disabled={busy} title={ask.tooltip}>
              <Icon of={Stamp} />
              {busy ? "Asking…" : "Ask for this policy"}
            </Button>
          </div>
          {(detail.data.policy.history ?? []).length > 0 && (
            <section className="fy-stack fy-stack--2" aria-labelledby="policy-history">
              <h3 className="fy-h3" id="policy-history">
                Earlier versions
              </h3>
              <ul className="fy-list rounded-md border border-border" aria-labelledby="policy-history">
                {(detail.data.policy.history ?? []).map((h) => (
                  <li key={h.number} className="fy-list__item">
                    <span className="fy-list__main">
                      <span className="fy-list__title whitespace-normal">
                        Version {h.number}: {(h.steps ?? []).map((s, i) => `step ${i + 1}, ${stepWho(s)}`).join("; ")}
                      </span>
                      <span className="fy-list__meta">Since {date(h.changed_at)}</span>
                    </span>
                    <Link className="fy-small" to="/approvals/$approvalId" params={{ approvalId: h.request_id }}>
                      The approval
                    </Link>
                  </li>
                ))}
              </ul>
            </section>
          )}
        </form>
      )}
    </Dialog>
  );
}

// ApprovalPolicies is Settings › Approval policies (docs/design/screens ApprovalPolicies; FR-APR-01,
// FR-APR-09): who approves each kind of change, as ordered steps; a change is itself a request, which
// an Owner or Controller who did not ask approves. The policy for approval policies is fixed.
export function ApprovalPolicies() {
  const list = useQuery({ queryKey: ["approval-policies"], queryFn: listPolicies });
  const waiting = useQuery({ queryKey: ["approvals", "policies"], queryFn: waitingChanges });
  const [editing, setEditing] = useState<Policy | null>(null);
  const [asked, setAsked] = useState<Approval | null>(null);
  const change = helpText("approval-policy.change");
  const pendingFor = (p: Policy) => (waiting.data ?? []).find((a) => a.object_key === p.object_type);

  return (
    <SettingsPage title="Approval policies" screen="approval-policies" lead="Who approves each kind of change. Until you change one, one approval from anyone who may approve it, other than the person who asked.">
      {asked && (
        <div className="fy-banner fy-banner--info" role="status">
          <Icon of={Hourglass} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">Waiting for approval</span>
            <span className="fy-small">
              {asked.summary}. The policy has not changed yet.{" "}
              <Link to="/approvals/$approvalId" params={{ approvalId: asked.id }}>
                View the request
              </Link>
            </span>
          </div>
        </div>
      )}
      {list.isError && <Refusal problem={problemOf(list.error)} title="The policies could not be listed" />}
      {list.isPending && <p className="fy-secondary">Loading the policies…</p>}
      {list.data && (
        <section className="fy-card" aria-labelledby="policies-title">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="policies-title">
              Kinds of change
            </h2>
            <span className="fy-badge fy-badge--quiet">{list.data.length}</span>
          </div>
          <ul className="fy-list" aria-labelledby="policies-title">
            {list.data.map((p) => {
              const w = pendingFor(p);
              return (
                <li key={p.object_type} className="fy-list__item">
                  <span className="fy-list__icon">
                    <Icon of={p.fixed ? Lock : Stamp} size={20} />
                  </span>
                  <span className="fy-list__main">
                    <span className="fy-list__title whitespace-normal">{objectLabel(p.object_type)}</span>
                    <span className="fy-list__meta whitespace-normal">{(p.steps ?? []).map((s, i) => `Step ${i + 1}: ${stepWho(s)}`).join(" · ")}</span>
                    <span className="fy-list__meta whitespace-normal">
                      {p.built_in ? "Built-in policy" : `Version ${p.number}`} · approvers hold A on "{p.row}"
                    </span>
                  </span>
                  {w ? (
                    <Link className="fy-chip fy-chip--neutral" to="/approvals/$approvalId" params={{ approvalId: w.id }}>
                      <Icon of={Hourglass} />
                      Change waiting
                    </Link>
                  ) : p.fixed ? (
                    <span className="fy-tag">Fixed</span>
                  ) : (
                    <Button size="sm" onClick={() => setEditing(p)} aria-label={`${change.label}: ${objectLabel(p.object_type)}`} title={change.tooltip}>
                      <Icon of={Pencil} />
                      <span className="fy-from-medium">{change.label}</span>
                    </Button>
                  )}
                </li>
              );
            })}
          </ul>
        </section>
      )}
      <ChangePolicy policy={editing} onClose={() => setEditing(null)} onAsked={setAsked} />
    </SettingsPage>
  );
}
