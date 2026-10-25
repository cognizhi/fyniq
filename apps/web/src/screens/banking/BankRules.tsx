import { useQuery, useQueryClient } from "@tanstack/react-query";
import { BadgeCheck, CircleSlash, FlaskConical, Hourglass, Lightbulb, Pencil, Plus, TriangleAlert, Zap } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { AiBadge } from "../../components/ai/AiBadge";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { BankTabs, makers } from "./BankAccounts";
import { useContacts, usePostingAccounts } from "./LineDialogs";

type Rule = components["schemas"]["BankRule"];
type Conditions = components["schemas"]["BankRuleConditions"];
type Action = components["schemas"]["BankRuleAction"];
type Draft = components["schemas"]["BankRuleDraft"];

// conditionWords says what a rule's conditions are, in words.
export function conditionWords(c: Conditions, accounts: Map<string, string>): string {
  const out: string[] = [];
  if (c.direction) out.push(c.direction === "in" ? "money in" : "money out");
  if (c.description_contains) out.push(`description contains "${c.description_contains}"`);
  if (c.description_regex) out.push(`description matches /${c.description_regex}/`);
  if (c.counterparty_contains) out.push(`counterparty contains "${c.counterparty_contains}"`);
  if (c.amount_min && c.amount_max) out.push(`${c.amount_min} to ${c.amount_max}`);
  else if (c.amount_min) out.push(`at least ${c.amount_min}`);
  else if (c.amount_max) out.push(`at most ${c.amount_max}`);
  if (c.currency) out.push(`in ${c.currency}`);
  if (c.money_account_id) out.push(`on ${accounts.get(c.money_account_id) ?? "an account"}`);
  return out.length ? out.join(", ") : "every line";
}

export interface RuleForm {
  name: string;
  position: string;
  active: boolean;
  when: Conditions;
  then: Action;
}

// checkRule reads the rule form: a name, a position, at least one condition, an account, and amounts as plain decimals;
// every problem at once (ENG-23).
export function checkRule(f: RuleForm): { draft?: Draft; errors: Record<string, string> } {
  const errors: Record<string, string> = {};
  if (!f.name.trim()) errors.name = "Name the rule, such as what it codes.";
  if (!/^[1-9]\d{0,4}$/.test(f.position.trim())) errors.position = "Enter its place in the order: 1 is first.";
  const w = f.when;
  if (!w.description_contains && !w.description_regex && !w.counterparty_contains && !w.direction && !w.amount_min && !w.amount_max && !w.currency && !w.money_account_id)
    errors.when = "Give the rule at least one condition.";
  for (const k of ["amount_min", "amount_max"] as const)
    if (w[k] && !/^\d{1,16}(\.\d{1,4})?$/.test(w[k]!)) errors[k] = "Enter the amount's size as a plain decimal, such as 500.00.";
  if (!f.then.account_id) errors.account = "Choose the account the lines go to.";
  if (Object.keys(errors).length > 0) return { errors };
  const clean = Object.fromEntries(Object.entries(w).filter(([, v]) => v !== "" && v !== undefined)) as Conditions;
  const then = Object.fromEntries(Object.entries(f.then).filter(([, v]) => v !== "" && v !== undefined)) as Action;
  return { errors, draft: { name: f.name.trim(), position: Number(f.position.trim()), active: f.active, when: clean, then } };
}

// BankRules is Banking › Rules (docs/design/screens BankRules; FR-BNK-05, FR-BNK-14; p1-bank-import-reconcile task 10.4,
// Expert): the rules in their order, each with what it meets and how it codes, made and changed with a back-test; a
// rule applies by itself only once another person approves it, and an AI-made rule never does by being made. Rules
// offered from lines coded the same way several times wait to be made into rules.
export function BankRules() {
  const { session } = useSignedIn();
  const may = (session.roles ?? []).some((r) => makers.includes(r));
  const queries = useQueryClient();
  const [editing, setEditing] = useState<{ rule?: Rule; form: RuleForm } | null>(null);
  const [notice, setNotice] = useState("");
  const rules = useQuery({
    queryKey: ["bank-rules"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/bank-rules", { params: { query: { page_size: 200 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const offers = useQuery({
    queryKey: ["bank-rule-offers"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/bank-rule-offers", { params: { query: { page_size: 20 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const chart = usePostingAccounts();
  const money = useQuery({
    queryKey: ["money-accounts", "all"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/money-accounts", { params: { query: { page_size: 100 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const accountNames = new Map((chart.data ?? []).map((a) => [a.id, `${a.code} ${a.name}`]));
  const moneyNames = new Map((money.data ?? []).map((a) => [a.id, a.name]));
  const items = rules.data ?? [];
  const next = String((items.at(-1)?.position ?? 0) + 1);
  const blank: RuleForm = { name: "", position: next, active: true, when: {}, then: { account_id: "" } };
  const done = (words: string) => {
    setEditing(null);
    setNotice(words);
    void queries.invalidateQueries({ queryKey: ["bank-rules"] });
    void queries.invalidateQueries({ queryKey: ["bank-rule-offers"] });
  };
  const h = helpText("banking.rules");

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Banking</span>
          <h1 className="fy-h1">Bank rules</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
        {may && (
          <div className="fy-pagehead__actions">
            <button type="button" className="fy-btn fy-btn--primary" onClick={() => setEditing({ form: blank })}>
              <Icon of={Plus} />
              New rule
            </button>
          </div>
        )}
      </div>
      <ReviewBanner screen="bank-rules" />
      <BankTabs />
      {notice && (
        <p className="fy-small" role="status">
          {notice}
        </p>
      )}
      {rules.isError && <Refusal problem={problemOf(rules.error)} title="The rules could not be listed" />}
      {rules.data && items.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={FlaskConical} size={24} />
          <h2 className="fy-h3">No rules yet</h2>
          <p className="fy-secondary">A rule codes the lines it meets, such as every AWS charge to Software. Test it against the lines held before you save it.</p>
        </div>
      )}
      {items.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-label="Bank rules">
            <thead>
              <tr>
                <th scope="col" className="fy-num">
                  Order
                </th>
                <th scope="col">Rule</th>
                <th scope="col">When</th>
                <th scope="col">Then</th>
                <th scope="col">Status</th>
                <th scope="col">Actions</th>
              </tr>
            </thead>
            <tbody>
              {items.map((r) => (
                <tr key={r.id}>
                  <td data-card="meta" className="fy-num">
                    {r.position}
                  </td>
                  <td data-card="title">
                    <span className="fy-strong">{r.name}</span> {r.from_ai_proposal && <AiBadge />}
                  </td>
                  <td data-card="meta">{conditionWords(r.when, moneyNames)}</td>
                  <td data-card="meta">
                    {accountNames.get(r.then.account_id) ?? "An account"}
                    {r.then.gst_code && <span className="fy-mono"> · {r.then.gst_code}</span>}
                  </td>
                  <td data-card="status">
                    <RuleStatus rule={r} />
                  </td>
                  <td>
                    {may && (
                      <span className="fy-row fy-row--wrap gap-1">
                        <button
                          type="button"
                          className="fy-btn fy-btn--ghost fy-btn--sm"
                          onClick={() => setEditing({ rule: r, form: { name: r.name, position: String(r.position), active: r.active, when: r.when, then: r.then } })}
                          aria-label={`Change ${r.name}`}
                        >
                          <Icon of={Pencil} />
                          Change
                        </button>
                        {!r.auto_apply && r.active && !r.problem && <AskAutoApply rule={r} onDone={done} />}
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {offers.data && offers.data.length > 0 && (
        <section className="fy-card" aria-labelledby="offers">
          <div className="fy-card__head">
            <h2 className="fy-h3 fy-row gap-2" id="offers">
              <Icon of={Lightbulb} />
              Rules to consider
            </h2>
            <span className="fy-small fy-muted">Lines coded the same way several times; none applies until you make it a rule.</span>
          </div>
          <ul className="fy-card__body fy-stack fy-stack--2">
            {offers.data.map((o, n) => (
              <li key={n} className="fy-row fy-row--between fy-row--wrap gap-2">
                <span>
                  {conditionWords(o.when, moneyNames)} → {accountNames.get(o.then.account_id) ?? "an account"} · {o.lines} lines
                </span>
                {may && (
                  <button type="button" className="fy-btn fy-btn--secondary fy-btn--sm" onClick={() => setEditing({ form: { name: "", position: next, active: true, when: o.when, then: o.then } })}>
                    Make it a rule
                  </button>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}
      {editing && <RuleDialog rule={editing.rule} initial={editing.form} accounts={chart.data ?? []} moneyAccounts={money.data ?? []} onClose={() => setEditing(null)} onDone={done} />}
    </div>
  );
}

function RuleStatus({ rule: r }: { rule: Rule }) {
  if (r.problem)
    return (
      <span className="fy-chip fy-chip--warning" title={r.problem}>
        <Icon of={TriangleAlert} />
        Does not read: {r.problem}
      </span>
    );
  if (!r.active)
    return (
      <span className="fy-chip fy-chip--neutral">
        <Icon of={CircleSlash} />
        Off
      </span>
    );
  if (r.auto_apply)
    return (
      <span className="fy-chip fy-chip--good">
        <Icon of={Zap} />
        Applies by itself
      </span>
    );
  return (
    <span className="fy-chip fy-chip--info">
      <Icon of={BadgeCheck} />
      Suggests
    </span>
  );
}

// AskAutoApply asks another person to let a rule apply by itself (FR-BNK-14, FR-APR-01), after confirming it's you; the
// approval shows the rule's back-test, and a change to the rule meanwhile stops it.
function AskAutoApply({ rule, onDone }: { rule: Rule; onDone: (words: string) => void }) {
  const confirm = useConfirm();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("bank-rule.auto-apply");
  const go = async () => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/bank-rules/{rule_id}/auto-apply-approval", { params: { path: { rule_id: rule.id }, header: { "Idempotency-Key": newKey(), "If-Match": `"${rule.version}"` } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(`Asked for approval to let ${rule.name} apply by itself. Another person who may approve it decides in their Inbox.`);
  };
  return (
    <>
      <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm" onClick={() => void go()} disabled={busy} title={h.tooltip}>
        <Icon of={Hourglass} />
        {h.label}
      </button>
      {problem && <Refusal problem={problem} title="The approval was not asked for" />}
    </>
  );
}

// RuleDialog makes or changes a rule, with its back-test against the lines held: how many it would have matched, and how
// they stand now.
function RuleDialog({
  rule,
  initial,
  accounts,
  moneyAccounts,
  onClose,
  onDone,
}: {
  rule?: Rule;
  initial: RuleForm;
  accounts: components["schemas"]["Account"][];
  moneyAccounts: components["schemas"]["MoneyAccount"][];
  onClose: () => void;
  onDone: (words: string) => void;
}) {
  const id = useId();
  const contacts = useContacts();
  const [f, setF] = useState<RuleForm>(initial);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [test, setTest] = useState<components["schemas"]["BankRuleBackTest"] | null>(null);
  const codes = useQuery({
    queryKey: [f.when.direction === "in" ? "sales-gst-codes" : "purchase-gst-codes", "today"],
    queryFn: async () => {
      const out = f.when.direction === "in" ? await api.GET("/v1/sales/gst-codes", {}) : await api.GET("/v1/purchases/gst-codes", {});
      if (out.error) throw out.error;
      return (out.data.codes ?? []).map((c) => c.code);
    },
  });
  const when = (k: keyof Conditions) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => {
    setF({ ...f, when: { ...f.when, [k]: e.target.value || undefined } });
    setTest(null);
  };
  const then = (k: keyof Action) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => setF({ ...f, then: { ...f.then, [k]: e.target.value || undefined } as Action });
  const backTest = async () => {
    setProblem(null);
    const out = await api.POST("/v1/bank-rules:back-test", { body: f.when });
    if (!out.data) return setProblem(problemOf(out.error));
    setTest(out.data);
  };
  const save = async () => {
    const { draft, errors: e } = checkRule(f);
    setErrors(e);
    if (!draft) return;
    setBusy(true);
    setProblem(null);
    const out = rule
      ? await api.PUT("/v1/bank-rules/{rule_id}", { params: { path: { rule_id: rule.id }, header: { "Idempotency-Key": newKey(), "If-Match": `"${rule.version}"` } }, body: draft })
      : await api.POST("/v1/bank-rules", { params: { header: { "Idempotency-Key": newKey() } }, body: draft });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(rule ? `${draft.name} is changed.${rule.auto_apply ? " It suggests only until another person approves it again." : ""}` : `${draft.name} is saved; it suggests from now on.`);
  };
  const err = (k: string) =>
    errors[k] ? (
      <span className="fy-error" id={`${id}-${k}-error`}>
        {errors[k]}
      </span>
    ) : null;
  const aria = (k: string) => (errors[k] ? { "aria-invalid": true as const, "aria-describedby": `${id}-${k}-error` } : {});
  const input = (k: keyof Conditions, label: string, extra: Record<string, unknown> = {}) => (
    <div className="fy-field">
      <label className="fy-label" htmlFor={`${id}-${k}`}>
        {label}
      </label>
      <input className="fy-input" id={`${id}-${k}`} value={(f.when[k] as string | undefined) ?? ""} maxLength={200} onChange={when(k)} {...aria(k)} {...extra} />
      {err(k)}
    </div>
  );
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={rule ? `Change ${rule.name}` : "New rule"}
      description="A rule suggests how to code the lines it meets; the first rule in the order that a line meets is the one suggested."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="secondary" onClick={() => void backTest()} disabled={busy}>
            Test it
          </Button>
          <Button variant="primary" onClick={() => void save()} disabled={busy}>
            Save the rule
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The rule was not saved" />}
      <div className="fy-row fy-row--wrap gap-3">
        <div className="fy-field fy-grow">
          <label className="fy-label" htmlFor={`${id}-name`}>
            Name
          </label>
          <input className="fy-input" id={`${id}-name`} value={f.name} maxLength={100} onChange={(e) => setF({ ...f, name: e.target.value })} {...aria("name")} />
          {err("name")}
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-position`}>
            Order
          </label>
          <input className="fy-input fy-num max-w-40" id={`${id}-position`} inputMode="numeric" value={f.position} onChange={(e) => setF({ ...f, position: e.target.value })} {...aria("position")} />
          {err("position")}
        </div>
      </div>
      <label className="fy-row gap-2">
        <input type="checkbox" checked={f.active} onChange={(e) => setF({ ...f, active: e.target.checked })} />
        On: it suggests
      </label>
      <fieldset className="fy-field">
        <legend className="fy-label">When a line…</legend>
        {errors.when && <p className="fy-error">{errors.when}</p>}
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-direction`}>
            Direction
          </label>
          <select className="fy-select" id={`${id}-direction`} value={f.when.direction ?? ""} onChange={when("direction")}>
            <option value="">Either</option>
            <option value="in">Money in</option>
            <option value="out">Money out</option>
          </select>
        </div>
        {input("description_contains", "Description contains")}
        {input("description_regex", "Description matches (a regular expression)", { className: "fy-input fy-mono" })}
        {input("counterparty_contains", "Counterparty contains")}
        <div className="fy-row fy-row--wrap gap-3">
          {input("amount_min", "At least", { inputMode: "decimal" })}
          {input("amount_max", "At most", { inputMode: "decimal" })}
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-account`}>
            On the money account
          </label>
          <select className="fy-select" id={`${id}-account`} value={f.when.money_account_id ?? ""} onChange={when("money_account_id")}>
            <option value="">Any</option>
            {moneyAccounts.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </select>
        </div>
      </fieldset>
      <fieldset className="fy-field">
        <legend className="fy-label">Then code it to…</legend>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-to`}>
            Account
          </label>
          <select className="fy-select" id={`${id}-to`} value={f.then.account_id} onChange={then("account_id")} {...aria("account")}>
            <option value="">Choose an account</option>
            {accounts.map((a) => (
              <option key={a.id} value={a.id}>
                {a.code} {a.name}
              </option>
            ))}
          </select>
          {err("account")}
        </div>
        <div className="fy-row fy-row--wrap gap-3">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-gst`}>
              GST code
            </label>
            <select className="fy-select" id={`${id}-gst`} value={f.then.gst_code ?? ""} onChange={then("gst_code")}>
              <option value="">The account's default</option>
              {(codes.data ?? []).map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </select>
          </div>
          <div className="fy-field fy-grow">
            <label className="fy-label" htmlFor={`${id}-contact`}>
              Contact
            </label>
            <select className="fy-select" id={`${id}-contact`} value={f.then.contact_id ?? ""} onChange={then("contact_id")}>
              <option value="">None</option>
              {(contacts.data ?? []).map((c) => (
                <option key={c.id} value={c.id}>
                  {c.legal_name}
                </option>
              ))}
            </select>
          </div>
        </div>
      </fieldset>
      {test && (
        <div className="fy-banner fy-banner--info" role="status">
          <Icon of={FlaskConical} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">
              It would have matched {test.matched} of the {test.examined} lines held.
            </span>
            {test.sample.length > 0 && (
              <ul className="fy-small">
                {test.sample.map((l) => (
                  <li key={l.id}>
                    {date(l.booking_date)} · <span className="fy-mono">{l.description ?? ""}</span> · {formatAmount(l.amount.amount)} {l.amount.currency}
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      )}
      <p className="fy-small fy-muted">Saving a rule makes it suggest. It posts by itself only once another person approves its request to apply by itself, and then within the organisation's limits.</p>
    </Dialog>
  );
}
