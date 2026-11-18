import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Ban, ChevronLeft, CircleCheck, CircleDashed, Hourglass, ListChecks, Power, PowerOff, ShieldCheck } from "lucide-react";
import { useState } from "react";
import { api, etagOf } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { roleLabel } from "./words";

type TaskType = components["schemas"]["AITaskTypeView"];
type ProposalRow = components["schemas"]["AIProposalSummary"];

const setting = "ai.task_types";

// The states a proposal moves through, as the history filters and shows them (§9.5).
const states = ["pending", "approved", "executed", "rejected", "expired", "invalid", "stale", "failed"] as const;
const stateWords: Record<string, string> = {
  draft: "Being checked",
  invalid: "Invalid",
  pending: "Waiting",
  approved: "Approved",
  rejected: "Rejected",
  expired: "Expired",
  superseded: "Superseded",
  executing: "Running",
  executed: "Executed",
  stale: "Stale",
  failed: "Failed",
};
const tierWords: Record<number, string> = { 1: "Tier 1 · routine", 2: "Tier 2 · posts or changes records", 3: "Tier 3" };

// The never-list in words (§9.6; proposal.Refuse): what no task type can ever propose, whatever is turned on.
const never = [
  "Approving or rejecting anything",
  "Releasing or making a payment",
  "Changing a payee's bank details",
  "Changing users, roles or the team mode",
  "Changing approval policies, the AI's task types or its settings",
  "Changing statutory parameters",
  "Reopening a period",
  "Voiding or deleting a posted record",
  "Filing or lodging with IRAS, CPF or ACRA",
  "Exporting the company's data",
  "Changing the audit trail",
  "Issuing or transferring shares, or changing holders, instruments or investor rights",
  "Repaying, converting or waiving a director's balance",
  "Changing officers or registers, or marking an obligation as met",
];

async function taskTypes() {
  const r = await api.GET("/v1/ai/task-types");
  if (r.error) throw r.error;
  return r.data.task_types;
}

async function proposals(status: string) {
  const r = await api.GET("/v1/ai/proposals", { params: { query: { page_size: 50, ...(status ? { status: status as (typeof states)[number] } : {}) } } });
  if (r.error) throw r.error;
  return r.data.items;
}

async function enabled() {
  const r = await api.GET("/v1/organisation/settings/{key}", { params: { path: { key: setting } } });
  if (r.error) throw r.error;
  return { value: r.data.value, etag: etagOf(r.response) || `"${r.data.version}"` };
}

// aiSwitch is the AI switch's value (AI-13), or "" for a person who may not read settings.
async function aiSwitch() {
  const r = await api.GET("/v1/organisation/settings/{key}", { params: { path: { key: "features.ai_assistant" } } });
  return r.data?.value ?? "";
}

// Catalogue lists the release's task types by tier (§9.6): what each does, its limits, who approves it, whether this
// release can run it, and whether the organisation has it on; turning one on waits for a second approver (AI-32),
// turning it off takes effect at once.
function Catalogue() {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const tt = useQuery({ queryKey: ["ai-task-types"], queryFn: taskTypes });
  const on = useQuery({ queryKey: ["setting", setting], queryFn: enabled });
  const ai = useQuery({ queryKey: ["setting", "features.ai_assistant"], queryFn: aiSwitch });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [note, setNote] = useState<{ text: string; approval?: string } | null>(null);
  const [busy, setBusy] = useState("");
  const turn = helpText("ai-tasks.turn");
  if (tt.isPending || on.isPending) return <p className="fy-secondary">Loading the task types…</p>;
  if (tt.isError) return <Refusal problem={problemOf(tt.error)} title="The task types could not be shown" />;
  const current = (on.data?.value ?? "").split(",").filter(Boolean);
  const change = async (t: TaskType, enable: boolean) => {
    if (!on.data) return;
    setBusy(t.id);
    setProblem(null);
    setNote(null);
    const value = (enable ? [...current, t.id] : current.filter((x) => x !== t.id)).sort().join(",");
    const out = await guarded(confirm, () =>
      api.PATCH("/v1/organisation/settings/{key}", { params: { path: { key: setting }, header: { "If-Match": on.data.etag } }, body: { value } }),
    );
    setBusy("");
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    await Promise.all(["setting", "ai-task-types", "approvals"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
    if (out.response.status === 202) {
      const a = out.data as unknown as { id: string };
      setNote({ text: `Turning on ${t.name} waits for a second approver.`, approval: a.id });
    } else {
      setNote({ text: `${t.name} is ${enable ? "on" : "off"}.` });
    }
  };
  const tiers = [...new Set(tt.data.map((t) => t.tier))].sort();
  return (
    <div className="fy-stack fy-stack--6">
      <p className="fy-small" role="status">
        {note?.text}{" "}
        {note?.approval && (
          <Link to="/approvals/$approvalId" params={{ approvalId: note.approval }}>
            See the request
          </Link>
        )}
      </p>
      {problem && <Refusal problem={problem} title="The task type was not changed" />}
      {ai.data === "false" && (
        <div className="fy-banner fy-banner--warning">
          <Icon of={PowerOff} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">The AI is switched off</span>
            <span className="fy-small fy-secondary">It proposes nothing until an Owner switches it on again in AI settings.</span>
          </div>
        </div>
      )}
      {tt.data.length === 0 && <p className="fy-secondary">This release has no task type yet: the AI cannot propose anything.</p>}
      {tt.data.length > 0 && !tt.data.some((t) => t.on) && (
        <p className="fy-secondary">Every task type is off, as each starts: the AI proposes nothing until one is turned on.</p>
      )}
      {tiers.map((tier) => (
        <section key={tier} className="fy-card" aria-labelledby={`tier-${tier}`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`tier-${tier}`}>
              {tierWords[tier] ?? `Tier ${tier}`}
            </h2>
          </div>
          <ul className="fy-list">
            {tt.data
              .filter((t) => t.tier === tier)
              .map((t) => (
                <li key={t.id} className="fy-list__item">
                  <span className="fy-list__main">
                    <span className="fy-list__title">{t.name}</span>
                    <span className="fy-small fy-secondary">{t.description}</span>
                    <span className="fy-list__meta">
                      <span className="fy-mono fy-expert-only">{t.id} · </span>
                      At most {t.max_items} {t.max_items === 1 ? "item" : "items"}
                      {t.max_amount_total ? `, ${t.currency} ${t.max_amount_total} in all` : ""} · approved by {t.approvers.map(roleLabel).join(" or ")} · expires after{" "}
                      {t.expiry_hours} hours
                    </span>
                  </span>
                  <span className="fy-row">
                    {!t.built ? (
                      <span className="fy-chip fy-chip--neutral">Not in this release</span>
                    ) : t.on ? (
                      <>
                        <span className="fy-chip fy-chip--good">
                          <Icon of={ShieldCheck} />
                          On
                        </span>
                        <Button size="sm" variant="ghost" onClick={() => void change(t, false)} disabled={busy !== ""} title={turn.tooltip}>
                          <Icon of={PowerOff} />
                          Turn off
                        </Button>
                      </>
                    ) : (
                      <>
                        <span className="fy-chip fy-chip--neutral">Off</span>
                        <Button size="sm" variant="secondary" onClick={() => void change(t, true)} disabled={busy !== ""} title={turn.tooltip}>
                          <Icon of={Power} />
                          Turn on…
                        </Button>
                      </>
                    )}
                  </span>
                </li>
              ))}
          </ul>
        </section>
      ))}
      <section className="fy-card" aria-labelledby="never-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="never-title">
            Never proposed
          </h2>
        </div>
        <div className="fy-card__body fy-stack fy-stack--3">
          <p className="fy-small fy-secondary">Whatever is turned on, the AI can never propose these. A person does them, in Fyniq.</p>
          <ul className="fy-list fy-list--plain">
            {never.map((n) => (
              <li key={n} className="fy-row">
                <Icon of={Ban} />
                {n}
              </li>
            ))}
          </ul>
        </div>
      </section>
    </div>
  );
}

// History is every proposal you may see, newest first, filtered by state; each opens its review (§9.6).
function History() {
  const [state, setState] = useState("");
  const q = useQuery({ queryKey: ["ai-proposals", state], queryFn: () => proposals(state) });
  return (
    <div className="fy-stack fy-stack--4">
      <div className="fy-row fy-row--wrap" role="group" aria-label="Show proposals">
        {["", ...states].map((s) => (
          <Button key={s || "all"} size="sm" variant={state === s ? "primary" : "ghost"} aria-pressed={state === s} onClick={() => setState(s)}>
            {s ? stateWords[s] : "All"}
          </Button>
        ))}
      </div>
      {q.isPending && <p className="fy-secondary">Loading the proposals…</p>}
      {q.isError && <Refusal problem={problemOf(q.error)} title="The proposals could not be shown" />}
      {q.data && q.data.length === 0 && <p className="fy-secondary">No proposal {state ? `is ${stateWords[state]?.toLowerCase()}` : "yet"}.</p>}
      {q.data && q.data.length > 0 && (
        <ul className="fy-list">
          {q.data.map((p: ProposalRow) => (
            <li key={p.id} className="fy-list__item">
              <span className="fy-list__main">
                <Link className="fy-list__title" to="/inbox/proposals/$proposalId" params={{ proposalId: p.id }}>
                  {p.summary || p.task_name}
                </Link>
                <span className="fy-list__meta">
                  <span className="fy-mono fy-expert-only">{p.task_type} · </span>
                  {dateTime(p.proposed_at)}
                  {p.reason ? ` · ${p.reason}` : ""}
                </span>
              </span>
              <span className={`fy-chip ${p.status === "executed" ? "fy-chip--good" : p.status === "pending" ? "fy-chip--warning" : "fy-chip--neutral"}`}>
                <Icon of={p.status === "executed" ? CircleCheck : p.status === "pending" ? Hourglass : CircleDashed} />
                {stateWords[p.status] ?? p.status}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// AITasks is the AI tasks page (docs/design/screens AiTasks; §9.6, AI-32): what the assistant may propose for the
// organisation, the never-list, and every proposal it has made. Nothing changes until a person approves it.
export function AITasks() {
  const h = helpText("ai-tasks.page");
  const [tab, setTab] = useState<"proposals" | "catalogue">("proposals");
  return (
    <div className="fy-page fy-enter">
      <Link className="fy-btn fy-btn--ghost fy-btn--sm -ms-2 self-start" to="/inbox">
        <Icon of={ChevronLeft} />
        Inbox
      </Link>
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1" title={h.tooltip}>
            <Icon of={ListChecks} /> {h.label}
          </h1>
          <p className="fy-secondary">What the assistant may propose, and every proposal it has made. Nothing changes until a person approves it.</p>
        </div>
      </div>
      <div className="fy-tabs" role="tablist" aria-label="AI tasks">
        <button type="button" role="tab" className="fy-tab" aria-selected={tab === "proposals"} onClick={() => setTab("proposals")}>
          Proposals
        </button>
        <button type="button" role="tab" className="fy-tab" aria-selected={tab === "catalogue"} onClick={() => setTab("catalogue")}>
          Catalogue
        </button>
      </div>
      <div role="tabpanel">{tab === "proposals" ? <History /> : <Catalogue />}</div>
    </div>
  );
}
