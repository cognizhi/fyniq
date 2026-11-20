import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, CirclePause, CircleX, Pause, Play, Plus, Archive } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { SettingsPage } from "./SettingsPage";

type Rule = components["schemas"]["AutomationRule"];
type Answer = { data?: unknown; error?: unknown; response: Response };

// The kinds of document a rule may watch, in words (the API accepts every kind whose postings tell people; these are the ones people ask for).
export const documentKinds: Record<string, string> = {
  bill: "A supplier bill",
  vendor_credit: "A supplier credit",
  bill_payment: "A payment of bills",
  invoice: "An invoice",
  credit_note: "A credit note",
  customer_receipt: "A customer receipt",
  write_off: "A write-off",
  journal: "A manual journal",
};

const states: Record<Rule["state"], { label: string; tone: string; icon: typeof CircleCheck }> = {
  active: { label: "Active", tone: "good", icon: CircleCheck },
  paused: { label: "Paused", tone: "warning", icon: CirclePause },
  archived: { label: "Archived", tone: "neutral", icon: CircleX },
};

async function listRules(): Promise<Rule[]> {
  const r = await api.GET("/v1/automation-rules", { params: { query: { page_size: 100 } } });
  if (r.error) throw r.error;
  return r.data.items ?? [];
}

async function listMembers() {
  const r = await api.GET("/v1/members", { params: { query: { page_size: 200 } } });
  return r.data?.items ?? [];
}

// WorkflowRules is Settings › Workflow rules (docs/design/screens WorkflowRules; FR-AUT-03): "when a bill of at least S$5,000 is posted, tell these members". A rule only tells people:
// there is no action that posts, approves, pays or files. Off until an Owner turns it on with a second approver, and the API says so in plain words when it is not.
export function WorkflowRules() {
  const id = useId();
  const queries = useQueryClient();
  const rules = useQuery({ queryKey: ["automation-rules"], queryFn: listRules });
  const members = useQuery({ queryKey: ["members", "rules"], queryFn: listMembers });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [adding, setAdding] = useState(false);
  const [form, setForm] = useState<{ name: string; kind: string; amount: string; who: string[] }>({ name: "", kind: "bill", amount: "", who: [] });
  const h = helpText("settings.workflow-rules");
  const names = new Map((members.data ?? []).map((m) => [m.user_id, m.display_name]));

  const run = async (call: () => Promise<Answer>): Promise<boolean> => {
    setBusy(true);
    setProblem(null);
    const r = await call();
    setBusy(false);
    if (r.error) {
      setProblem(problemOf(r.error));
      return false;
    }
    await queries.invalidateQueries({ queryKey: ["automation-rules"] });
    return true;
  };
  const add = async () => {
    const key = newKey();
    const ok = await run(() =>
      api.POST("/v1/automation-rules", {
        params: { header: { "Idempotency-Key": key } },
        body: { name: form.name.trim(), recipients: form.who, ...(form.kind ? { document_type: form.kind } : {}), ...(form.amount.trim() ? { amount_at_least: form.amount.trim() } : {}) },
      }),
    );
    if (ok) {
      setAdding(false);
      setForm({ name: "", kind: "bill", amount: "", who: [] });
    }
  };
  const setState = (r: Rule, state: Rule["state"]) => {
    const key = newKey();
    return run(() => api.PATCH("/v1/automation-rules/{rule_id}", { params: { path: { rule_id: r.id }, header: { "If-Match": `"${r.version}"`, "Idempotency-Key": key } }, body: { state } }));
  };

  const list = rules.data ?? [];
  return (
    <SettingsPage title={h.label} lead={h.tooltip} screen="workflow-rules">
      {problem && !adding && <Refusal problem={problem} title="That was not changed" />}
      {rules.error && <Refusal problem={problemOf(rules.error)} title="The rules could not be read" />}
      <section className="fy-card" aria-labelledby={`${id}-rules`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-rules`}>
            Rules
          </h2>
          <Button size="sm" variant="primary" onClick={() => setAdding(true)} disabled={busy} title={helpText("workflow-rules.add").tooltip}>
            <Icon of={Plus} />
            {helpText("workflow-rules.add").label}
          </Button>
        </div>
        <div className="fy-card__body">
          {rules.isSuccess && list.length === 0 && <p className="fy-secondary">No rule yet. Add one, such as telling the Controller when a bill of S$5,000 or more is posted.</p>}
          <ul className="fy-list">
            {list.map((r) => {
              const s = states[r.state];
              return (
                <li key={r.id} className="fy-list__item fy-stack fy-stack--2">
                  <div className="fy-row fy-row--wrap gap-2">
                    <span className="fy-list__title">{r.name}</span>
                    <span className={`fy-chip fy-chip--${s.tone}`}>
                      <Icon of={s.icon} />
                      {s.label}
                    </span>
                  </div>
                  <span className="fy-small fy-secondary">
                    When {r.document_type ? (documentKinds[r.document_type] ?? r.document_type).toLowerCase() : "any document"} is posted
                    {r.amount_at_least ? ` with debits of at least ${r.amount_at_least.replace(/(\.\d\d)00$/, "$1")}` : ""}, tell {r.recipients.map((u) => names.get(u) ?? "a member").join(", ")}.
                  </span>
                  {r.state !== "archived" && (
                    <div className="fy-row fy-row--wrap gap-2">
                      {r.state === "active" ? (
                        <Button size="sm" variant="secondary" onClick={() => setState(r, "paused")} disabled={busy} aria-label={`Pause: ${r.name}`}>
                          <Icon of={Pause} />
                          Pause
                        </Button>
                      ) : (
                        <Button size="sm" variant="secondary" onClick={() => setState(r, "active")} disabled={busy} aria-label={`Resume: ${r.name}`}>
                          <Icon of={Play} />
                          Resume
                        </Button>
                      )}
                      <Button size="sm" variant="ghost" onClick={() => setState(r, "archived")} disabled={busy} aria-label={`Archive: ${r.name}`}>
                        <Icon of={Archive} />
                        Archive
                      </Button>
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
        </div>
      </section>
      <p className="fy-small fy-secondary">A rule only tells people. It never posts, approves, pays or files anything, and each person it names must be allowed to see the document it is about.</p>

      <Dialog
        open={adding}
        onOpenChange={(o) => !o && setAdding(false)}
        dismissible={!busy}
        wide
        eyebrow="Workflow rules"
        title={helpText("workflow-rules.add").label}
        description="Tell members when a document is posted. Nothing else happens."
        footer={
          <>
            <Button variant="ghost" onClick={() => setAdding(false)} disabled={busy}>
              Cancel
            </Button>
            <Button variant="primary" onClick={add} disabled={busy || !form.name.trim() || form.who.length === 0}>
              {helpText("workflow-rules.add").label}
            </Button>
          </>
        }
      >
        {problem && <Refusal problem={problem} title="The rule was not added" />}
        <div className="fy-stack fy-stack--3">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-name`}>
              Name
            </label>
            <input className="fy-input" id={`${id}-name`} value={form.name} maxLength={100} placeholder="Large bills" onChange={(e) => setForm({ ...form, name: e.target.value })} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-kind`}>
              When this is posted
            </label>
            <select className="fy-select" id={`${id}-kind`} value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value })}>
              <option value="">Any document</option>
              {Object.entries(documentKinds).map(([k, label]) => (
                <option key={k} value={k}>
                  {label}
                </option>
              ))}
            </select>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-amount`}>
              With debits of at least (optional)
            </label>
            <input className="fy-input fy-num" id={`${id}-amount`} inputMode="decimal" value={form.amount} placeholder="5000.00" onChange={(e) => setForm({ ...form, amount: e.target.value })} aria-describedby={`${id}-amount-help`} />
            <span className="fy-help" id={`${id}-amount-help`}>
              In the base currency, as a decimal such as 5000.00. Leave it empty for any amount.
            </span>
          </div>
          <fieldset className="fy-field">
            <legend className="fy-label">Tell</legend>
            {(members.data ?? []).map((m) => (
              <label key={m.user_id} className="fy-row gap-2">
                <input type="checkbox" checked={form.who.includes(m.user_id)} onChange={(e) => setForm({ ...form, who: e.target.checked ? [...form.who, m.user_id] : form.who.filter((x) => x !== m.user_id) })} />
                <span>{m.display_name}</span>
              </label>
            ))}
          </fieldset>
        </div>
      </Dialog>
    </SettingsPage>
  );
}
