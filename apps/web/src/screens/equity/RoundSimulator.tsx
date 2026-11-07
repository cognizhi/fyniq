import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowDownRight, ArrowUpRight, CircleCheck, FlaskConical, Handshake, Hourglass, PencilLine, Plus, Save, Send, Trash2, TriangleAlert, UserRound } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { api, newKey } from "../../api/client";
import { fieldName, Refusal, sentence } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { typed } from "../../format/decimal";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { setMode, useMode } from "../../mode";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { EquityTabs, listHolders } from "./Holders";
import { formWords, listInstruments } from "./instrumentWords";
import { useCapital } from "./OptionForms";
import { count } from "./optionWords";
import {
  blankRound,
  formOf,
  listScenarios,
  modelRound,
  points,
  roundBody,
  sameScenario,
  saveScenario,
  scenarioOf,
  type Basis,
  type RoundDraft,
  type RoundForm,
  type RoundHolder,
  type RoundOutcome,
  type RoundScenario,
} from "./roundWords";

// The roles that save a scenario and ask for its approval (the Accountant's C/P on the cap table, Appendix D). Anyone
// who reads the whole cap table tries rounds in the sandbox.
const drafters = ["accountant"];

const today = () => new Date().toISOString().slice(0, 10);

function pct(p?: string): string {
  return p ? `${p}%` : "–";
}

function sgd(m: { amount: string; currency: string } | undefined): string {
  return m ? `${m.currency === "SGD" ? "S$" : `${m.currency} `}${formatAmount(m.amount)}` : "–";
}

// shown is an amount as typed, grouped for reading when it is one.
function shown(v: string): string {
  const t = typed(v);
  return t ? formatAmount(t) : v || "0";
}

// ready says whether a scenario has what a round needs before it is worth trying: a price or a valuation, and someone
// putting money in. Until then the outcome teaches what to enter instead of listing problems.
function ready(d: RoundDraft): boolean {
  return Boolean((d.price || d.pre_money) && d.investors.some((i) => i.holder_id && i.amount));
}

// nextName is the name a new scenario starts with: the next letter after those saved.
function nextName(saved: RoundScenario[] | undefined): string {
  return `Scenario ${String.fromCharCode(65 + Math.min(25, saved?.length ?? 0))}`;
}

// RoundSimulator is Equity › Round simulator (docs/design/screens RoundSimulator; FR-EQ-21, FR-EQ-27, design D8): a
// sandbox that models a priced round against today's cap table and posts nothing. Basic asks how much is raised, from
// whom, at what value and with how many more options, and shows who would own what; Expert adds the price basis, the
// top-up's timing, every investor's money with their pro-rata right, the SAFEs and notes converting and the pro-forma
// cap table. Saved scenarios sit side by side; an approved one drafts its share issues.
export function RoundSimulator() {
  const { session } = useSignedIn();
  const expert = useMode(session.roles) === "expert";
  const confirm = useConfirm();
  const queries = useQueryClient();
  const mayDraft = (session.roles ?? []).some((r) => drafters.includes(r));
  const scenarios = useQuery({ queryKey: ["round-scenarios"], queryFn: listScenarios });
  const capital = useCapital();
  const [picked, setPicked] = useState<string | null>(null);
  const saved = scenarios.data ?? [];
  const tab = picked ?? saved[0]?.id ?? "new";
  const one = useQuery({ queryKey: ["round-scenario", tab], queryFn: () => scenarioOf(tab), enabled: tab !== "new" });
  const [form, setForm] = useState<RoundForm | null>(null);
  const [status, setStatus] = useState("");
  const [problem, setProblem] = useState<{ p: ApiProblem; title: string } | null>(null);
  const [busy, setBusy] = useState(false);

  // The form follows the tab: a saved scenario as it was saved, or a new one from a blank.
  const loaded = tab === "new" ? "new" : one.data ? `${one.data.s.id}@${one.data.s.version}` : "";
  useEffect(() => {
    if (tab === "new") setForm(blankRound(today(), "", nextName(scenarios.data)));
    else if (one.data) setForm(formOf(one.data.s));
    setProblem(null);
    // The form is reset only when another scenario, or another version of it, is loaded.
  }, [loaded]);

  const body = form ? roundBody({ ...form, draft: { ...form.draft, class_account_id: form.draft.class_account_id || capital[0]?.id || "" } }) : null;
  const key = body ? JSON.stringify(body) : "";
  // The sandbox: the scenario as it is on the screen, modelled after a pause in typing (modelRound saves nothing).
  const [tried, setTried] = useState<{ outcome?: RoundOutcome; problem?: ApiProblem; busy: boolean }>({ busy: false });
  useEffect(() => {
    if (!body || !body.class_account_id || !ready(body)) return setTried({ busy: false });
    let stale = false;
    setTried((p) => ({ ...p, busy: true }));
    const timer = setTimeout(async () => {
      const r = await modelRound({ ...body, name: body.name || "New scenario" });
      if (stale) return;
      setTried(r.data ? { outcome: r.data, busy: false } : { problem: problemOf(r.error), busy: false });
    }, 400);
    return () => {
      stale = true;
      clearTimeout(timer);
    };
    // body is derived from key.
  }, [key]);

  const current = one.data?.s;
  const dirty = !current || !form || !sameScenario(form, current);
  const approved = current?.status === "approved";
  const saveH = helpText("round-scenario.save");
  const askH = helpText("round-scenario.ask");
  const set = (d: Partial<RoundDraft>) => form && setForm({ ...form, draft: { ...form.draft, ...d } });

  const refresh = async () => {
    await queries.invalidateQueries({ queryKey: ["round-scenarios"] });
    await queries.invalidateQueries({ queryKey: ["round-scenario"] });
  };
  const save = async () => {
    if (!body) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () => saveScenario(body, current && one.data ? { id: current.id, etag: one.data.etag } : undefined));
    setBusy(false);
    if (!out.data) return setProblem({ p: problemOf(out.error), title: "The scenario was not saved" });
    await refresh();
    setPicked(out.data.id);
    setStatus(`Saved ${out.data.name}. It is a sandbox: nothing is posted or recorded.`);
  };
  const ask = async () => {
    if (!current || !one.data) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/round-scenarios/{scenario_id}/approval", { params: { path: { scenario_id: current.id }, header: { "If-Match": one.data.etag, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem({ p: problemOf(out.error), title: "The scenario was not sent for approval" });
    await refresh();
    setStatus("Sent for approval. Once another Owner or the Controller approves it, its share issues are drafted.");
  };

  const actions = mayDraft && form && !approved && (
    <>
      <Button variant={expert ? "secondary" : "primary"} onClick={() => void save()} disabled={busy || !dirty} title={saveH.tooltip}>
        <Icon of={Save} />
        {saveH.label}
      </Button>
      {expert && current && !current.approval_id && (
        <Button variant="primary" onClick={() => void ask()} disabled={busy || dirty} title={dirty ? "Save the changes first, then send it for approval." : askH.tooltip}>
          <Icon of={Send} />
          {askH.label}
        </Button>
      )}
      {current?.approval_id && (
        <Link className="fy-btn fy-btn--secondary" to="/approvals/$approvalId" params={{ approvalId: current.approval_id }}>
          <Icon of={Hourglass} />
          Waiting for approval
        </Link>
      )}
    </>
  );

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Equity</span>
          <h1 className="fy-h1" title={helpText("equity.rounds").tooltip}>
            Round simulator
          </h1>
          <p className="fy-secondary">Try a funding round and see what everyone would own. Based on the cap table at the end of the day before the round.</p>
        </div>
        {actions && <div className="fy-pagehead__actions">{actions}</div>}
      </div>
      <EquityTabs current="rounds" />
      <div className="fy-banner fy-banner--info">
        <Icon of={FlaskConical} size={20} />
        <div className="fy-banner__text">
          <span className="fy-strong">Sandbox: nothing here is posted or recorded</span>
          <span className="fy-small">
            {expert
              ? "Scenarios never change the cap table or the books. Once another Owner or the Controller approves a scenario, it drafts its share issues, which then need their own approval and ACRA steps."
              : "Scenarios never change the cap table or the books. A real round is recorded later as share issues, with approvals and ACRA steps."}
          </span>
        </div>
      </div>
      {scenarios.isError && <Refusal problem={problemOf(scenarios.error)} title="The round simulator could not be shown" />}
      <p className="fy-small" role="status">
        {status}
      </p>
      {problem && <Refusal problem={problem.p} title={problem.title} />}
      {scenarios.data && (
        <div className="fy-row fy-row--wrap" style={{ gap: "var(--space-3)" }}>
          <div className="fy-segmented" role="radiogroup" aria-label="Scenario">
            {saved.map((s) => (
              <button key={s.id} type="button" role="radio" aria-checked={tab === s.id} onClick={() => setPicked(s.id)}>
                {s.name}
              </button>
            ))}
            {(tab === "new" || saved.length === 0) && (
              <button type="button" role="radio" aria-checked={tab === "new"} onClick={() => setPicked("new")}>
                {form && tab === "new" ? form.draft.name || "New scenario" : "New scenario"}
              </button>
            )}
          </div>
          {tab !== "new" && (
            <Button variant="ghost" size="sm" onClick={() => setPicked("new")}>
              <Icon of={Plus} />
              New scenario
            </Button>
          )}
          {current && <ScenarioStatus s={current} />}
        </div>
      )}
      {current && approved && (
        <div className="fy-banner fy-banner--good">
          <Icon of={CircleCheck} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">
              Approved{current.approved_by ? ` by ${current.approved_by.name}` : ""}
              {current.approved_at ? ` on ${date(current.approved_at)}` : ""}
            </span>
            <span className="fy-small">
              Its share issues are drafted: see <Link to="/equity/transactions">Transactions</Link>. It changes no more; to try other numbers, start a new scenario.
            </span>
          </div>
        </div>
      )}
      {form && (
        <div className="fy-split fy-split--wide-aside">
          <div className="fy-stack fy-stack--6">
            {expert ? (
              <ExpertInputs form={form} setForm={setForm} set={set} outcome={tried.outcome} mayDraft={mayDraft} locked={approved} />
            ) : (
              <BasicInputs form={form} set={set} locked={approved} />
            )}
            <Outcome tried={tried} draft={body} expert={expert} />
          </div>
          <aside className="fy-stack fy-stack--6" aria-label="Compare scenarios">
            <SideBySide saved={saved} mayDraft={mayDraft} />
          </aside>
        </div>
      )}
    </div>
  );
}

function ScenarioStatus({ s }: { s: RoundScenario }) {
  if (s.status === "approved") {
    return (
      <span className="fy-chip fy-chip--good">
        <Icon of={CircleCheck} />
        Approved
      </span>
    );
  }
  return s.approval_id ? (
    <span className="fy-chip fy-chip--neutral">
      <Icon of={Hourglass} />
      Waiting for approval
    </span>
  ) : (
    <span className="fy-chip fy-chip--neutral">
      <Icon of={PencilLine} />
      Draft
    </span>
  );
}

// useInvestors are the contacts who may put money in: those marked investor or shareholder.
function useInvestors() {
  const holders = useQuery({ queryKey: ["holders"], queryFn: listHolders });
  return (holders.data ?? []).filter((h) => h.investor || h.shareholder);
}

// BasicInputs are the round in a few plain fields (§7.8): how much is raised and from whom, the value before the money
// and more options for employees. What only Expert edits shows read-only under "Has advanced details" (FR-UX-25).
function BasicInputs({ form, set, locked }: { form: RoundForm; set: (d: Partial<RoundDraft>) => void; locked: boolean }) {
  const id = useId();
  const investors = useInvestors();
  const d = form.draft;
  const first = d.investors[0] ?? { holder_id: "", amount: "" };
  const setFirst = (x: Partial<typeof first>) => set({ investors: [{ ...first, ...x }, ...d.investors.slice(1)] });
  const advanced: string[] = [
    ...d.investors.slice(1).map((i) => `${investors.find((h) => h.contact_id === i.holder_id)?.name ?? "Another investor"} puts in S$${shown(i.amount)}`),
    ...(d.instruments.length > 0 ? [`${d.instruments.length} SAFE${d.instruments.length === 1 ? "" : "s"} or notes convert in the round`] : []),
    ...(d.top_up && d.top_up !== "0" && !d.top_up_before ? ["The extra options come after the money, so the new investors share them"] : []),
  ];
  return (
    <section className="fy-card" aria-labelledby={`${id}-title`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-title`}>
          The round
        </h2>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <div className="fy-form fy-form--2">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-raise`} title={helpText("round.raise").tooltip}>
              How much you raise
            </label>
            <div className="fy-inputgroup">
              <span className="fy-inputgroup__addon">S$</span>
              <input className="fy-input fy-num" id={`${id}-raise`} inputMode="decimal" value={first.amount} readOnly={locked} onChange={(e) => setFirst({ amount: e.target.value })} />
            </div>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-from`}>
              From
            </label>
            <select className="fy-select" id={`${id}-from`} value={first.holder_id} disabled={locked} onChange={(e) => setFirst({ holder_id: e.target.value })} aria-describedby={`${id}-from-help`}>
              <option value="">Choose the investor</option>
              {investors.map((h) => (
                <option key={h.contact_id} value={h.contact_id}>
                  {h.name}
                </option>
              ))}
            </select>
            <span className="fy-help" id={`${id}-from-help`}>
              A contact marked investor; add a new one in Contacts first
            </span>
          </div>
          {form.basis === "price" ? (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-price`} title={helpText("round.price").tooltip}>
                Price per share
              </label>
              <div className="fy-inputgroup">
                <span className="fy-inputgroup__addon">S$</span>
                <input className="fy-input fy-num" id={`${id}-price`} inputMode="decimal" value={d.price ?? ""} readOnly={locked} onChange={(e) => set({ price: e.target.value })} />
              </div>
            </div>
          ) : (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-value`} title={helpText("round.pre-money").tooltip}>
                Value of the company before the money
              </label>
              <div className="fy-inputgroup">
                <span className="fy-inputgroup__addon">S$</span>
                <input className="fy-input fy-num" id={`${id}-value`} inputMode="decimal" value={d.pre_money ?? ""} readOnly={locked} onChange={(e) => set({ pre_money: e.target.value })}
                  aria-describedby={`${id}-value-help`} />
              </div>
              <span className="fy-help" id={`${id}-value-help`}>
                What the investor and you agree the company is worth, such as 10,000,000
              </span>
            </div>
          )}
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-pool`} title={helpText("round.top-up").tooltip}>
              More options for employees
            </label>
            <input className="fy-input fy-num" id={`${id}-pool`} inputMode="numeric" value={d.top_up ?? ""} readOnly={locked} onChange={(e) => set({ top_up: e.target.value })}
              aria-describedby={`${id}-pool-help`} />
            <span className="fy-help" id={`${id}-pool-help`}>
              Investors often ask for a bigger pool before they invest; leave it empty for none
            </span>
          </div>
        </div>
        {advanced.length > 0 && (
          <details className="fy-stack fy-stack--2">
            <summary>Has advanced details ({advanced.length})</summary>
            <ul className="fy-small fy-secondary">
              {advanced.map((a) => (
                <li key={a}>{a}</li>
              ))}
            </ul>
            <Button variant="ghost" size="sm" onClick={() => setMode("expert")}>
              Open in Expert mode
            </Button>
          </details>
        )}
      </div>
    </section>
  );
}

// ExpertInputs are every input of a round (FR-EQ-21): its day and class, the price from a pre-money valuation or a
// price per share, a top-up before or after the money, each investor's money with their pro-rata right, the SAFEs and
// notes converting, and, to save it, its name and the resolutions its issues will name.
function ExpertInputs({ form, setForm, set, outcome, mayDraft, locked }: {
  form: RoundForm;
  setForm: (f: RoundForm) => void;
  set: (d: Partial<RoundDraft>) => void;
  outcome?: RoundOutcome;
  mayDraft: boolean;
  locked: boolean;
}) {
  const id = useId();
  const investors = useInvestors();
  const capital = useCapital();
  const instruments = useQuery({ queryKey: ["instruments"], queryFn: listInstruments });
  const converting = (instruments.data ?? []).filter((i) => i.status === "recorded" && !i.conversion && i.trigger === "priced_round");
  const d = form.draft;
  const setInvestor = (k: number, x: Partial<RoundDraft["investors"][number]>) => set({ investors: d.investors.map((i, j) => (j === k ? { ...i, ...x } : i)) });
  const proRata = (holder: string): RoundHolder | undefined => outcome?.holders.find((h) => h.holder_id === holder && h.fully_diluted_before !== "0");
  const basis = (b: Basis) => setForm({ ...form, basis: b });
  return (
    <section className="fy-card" aria-labelledby={`${id}-title`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-title`}>
          Inputs{d.name ? `, ${d.name}` : ""}
        </h2>
      </div>
      <fieldset className="fy-card__body fy-stack fy-stack--6" disabled={locked} style={{ border: 0, margin: 0, minWidth: 0 }}>
        <div className="fy-form fy-form--2">
          {mayDraft && (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-name`}>
                Scenario name
              </label>
              <input className="fy-input" id={`${id}-name`} maxLength={200} value={d.name} onChange={(e) => set({ name: e.target.value })} />
            </div>
          )}
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-on`} title={helpText("round.on").tooltip}>
              Round takes effect on
            </label>
            <input className="fy-input" id={`${id}-on`} type="date" value={d.on} onChange={(e) => set({ on: e.target.value })} aria-describedby={`${id}-on-help`} />
            <span className="fy-help" id={`${id}-on-help`}>
              It dilutes the cap table at the end of the day before
            </span>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-class`}>
              Shares of the class of
            </label>
            <select className="fy-select" id={`${id}-class`} value={d.class_account_id || capital[0]?.id || ""} onChange={(e) => set({ class_account_id: e.target.value })}>
              {capital.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.code} {a.name}
                </option>
              ))}
            </select>
          </div>
        </div>
        <div className="fy-row fy-row--wrap" style={{ gap: "var(--space-3)" }}>
          <span className="fy-label" id={`${id}-basis`} title={helpText("round.basis").tooltip}>
            Set the price from
          </span>
          <div className="fy-segmented" role="radiogroup" aria-labelledby={`${id}-basis`}>
            {(["pre_money", "price"] as const).map((b) => (
              <button key={b} type="button" role="radio" aria-checked={form.basis === b} onClick={() => basis(b)} disabled={locked}>
                {b === "pre_money" ? "Pre-money valuation" : "Price per share"}
              </button>
            ))}
          </div>
        </div>
        <div className="fy-form fy-form--2">
          {form.basis === "pre_money" ? (
            <>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-pre`} title={helpText("round.pre-money").tooltip}>
                  Pre-money valuation
                </label>
                <div className="fy-inputgroup">
                  <span className="fy-inputgroup__addon">S$</span>
                  <input className="fy-input fy-num" id={`${id}-pre`} inputMode="decimal" value={d.pre_money ?? ""} onChange={(e) => set({ pre_money: e.target.value })} />
                </div>
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-dp`}>
                  Price to decimals
                </label>
                <input className="fy-input fy-num" id={`${id}-dp`} type="number" min={0} max={6} value={d.price_decimals} onChange={(e) => set({ price_decimals: Number(e.target.value || 0) })}
                  aria-describedby={`${id}-dp-help`} />
                <span className="fy-help" id={`${id}-dp-help`}>
                  The valuation ÷ the shares fully diluted is cut down to these, as the term sheet says [accountant to confirm]
                </span>
              </div>
            </>
          ) : (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-price`} title={helpText("round.price").tooltip}>
                Price per share
              </label>
              <div className="fy-inputgroup">
                <span className="fy-inputgroup__addon">S$</span>
                <input className="fy-input fy-num" id={`${id}-price`} inputMode="decimal" value={d.price ?? ""} onChange={(e) => set({ price: e.target.value })} />
              </div>
            </div>
          )}
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-top`} title={helpText("round.top-up").tooltip}>
              Option pool top-up
            </label>
            <input className="fy-input fy-num" id={`${id}-top`} inputMode="numeric" value={d.top_up ?? ""} onChange={(e) => set({ top_up: e.target.value })} />
            <div className="fy-segmented" role="radiogroup" aria-label="Top-up timing" style={{ alignSelf: "flex-start" }}>
              {[true, false].map((before) => (
                <button key={String(before)} type="button" role="radio" aria-checked={(d.top_up_before ?? false) === before} onClick={() => set({ top_up_before: before })} disabled={locked}>
                  {before ? "Before the money" : "After"}
                </button>
              ))}
            </div>
          </div>
        </div>
        <fieldset className="fy-fieldset">
          <legend title={helpText("round.investors").tooltip}>New money per investor</legend>
          <div className="fy-stack fy-stack--3">
            {d.investors.map((inv, k) => {
              const right = proRata(inv.holder_id);
              return (
                <div key={k} className="fy-form fy-form--2">
                  <div className="fy-field">
                    <label className="fy-label" htmlFor={`${id}-inv-${k}`}>
                      Investor {k + 1}
                    </label>
                    <select className="fy-select" id={`${id}-inv-${k}`} value={inv.holder_id} onChange={(e) => setInvestor(k, { holder_id: e.target.value })}>
                      <option value="">Choose the investor</option>
                      {investors.map((h) => (
                        <option key={h.contact_id} value={h.contact_id}>
                          {h.name}
                        </option>
                      ))}
                    </select>
                    {right && (
                      <span className="fy-help">
                        Pro-rata right: {count(right.pro_rata)} of the new shares [accountant to confirm against the side letter]
                      </span>
                    )}
                  </div>
                  <div className="fy-field">
                    <label className="fy-label" htmlFor={`${id}-amt-${k}`}>
                      Their money
                    </label>
                    <div className="fy-row" style={{ gap: "var(--space-2)" }}>
                      <div className="fy-inputgroup" style={{ flex: 1, minWidth: 0 }}>
                        <span className="fy-inputgroup__addon">S$</span>
                        <input className="fy-input fy-num" id={`${id}-amt-${k}`} inputMode="decimal" value={inv.amount} onChange={(e) => setInvestor(k, { amount: e.target.value })} />
                      </div>
                      {d.investors.length > 1 && (
                        <Button variant="ghost" size="icon" aria-label={`Remove investor ${k + 1}`} title={`Remove investor ${k + 1}`}
                          onClick={() => set({ investors: d.investors.filter((_, j) => j !== k) })}>
                          <Icon of={Trash2} />
                        </Button>
                      )}
                    </div>
                  </div>
                </div>
              );
            })}
            {d.investors.length < 50 && (
              <Button variant="ghost" size="sm" style={{ alignSelf: "flex-start" }} onClick={() => set({ investors: [...d.investors, { holder_id: "", amount: "" }] })}>
                <Icon of={Plus} />
                Add an investor
              </Button>
            )}
          </div>
        </fieldset>
        {converting.length > 0 && (
          <fieldset className="fy-fieldset">
            <legend title={helpText("round.conversions").tooltip}>SAFEs and notes converting</legend>
            <div className="fy-stack fy-stack--3">
              {converting.map((i) => {
                const on = d.instruments.includes(i.id);
                const conv = outcome?.conversions.find((c) => c.instrument_id === i.id);
                return (
                  <div key={i.id} className="fy-row fy-row--between fy-row--top" style={{ gap: "var(--space-4)" }}>
                    <div className="fy-stack fy-stack--2" style={{ gap: 2, minWidth: 0 }}>
                      <span className="fy-strong" id={`${id}-safe-${i.id}`}>
                        Convert {i.holder_name}'s {formWords[i.form]}
                      </span>
                      <span className="fy-small fy-muted">
                        {sgd({ amount: i.amount, currency: i.currency })}
                        {i.valuation_cap ? `, cap S$${formatAmount(i.valuation_cap)}` : ""}
                        {i.discount ? `, ${i.discount}% discount` : ""}.
                        {conv ? ` At this round: ${count(conv.shares)} shares at S$${conv.price}.` : " Over the shares issued before the round, as its terms say (D6)."}
                      </span>
                      {conv?.review && (
                        <span className="fy-small">
                          <Icon of={TriangleAlert} /> {sentence(conv.review)}
                        </span>
                      )}
                    </div>
                    <button className="fy-switch" type="button" role="switch" aria-checked={on} aria-labelledby={`${id}-safe-${i.id}`} disabled={locked}
                      onClick={() => set({ instruments: on ? d.instruments.filter((x) => x !== i.id) : [...d.instruments, i.id] })} />
                  </div>
                );
              })}
            </div>
          </fieldset>
        )}
        {mayDraft && (
          <div className="fy-form fy-form--2">
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-refs`} title={helpText("round.resolutions").tooltip}>
                Resolutions its issues will name
              </label>
              <input className="fy-input" id={`${id}-refs`} maxLength={500} value={d.resolution_refs ?? ""} onChange={(e) => set({ resolution_refs: e.target.value })}
                aria-describedby={`${id}-refs-help`} />
              <span className="fy-help" id={`${id}-refs-help`}>
                Needed to send it for approval, such as: Board resolution 5 Mar 2027
              </span>
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-note`}>
                Note (optional)
              </label>
              <input className="fy-input" id={`${id}-note`} maxLength={1000} value={d.note ?? ""} onChange={(e) => set({ note: e.target.value })} />
            </div>
          </div>
        )}
      </fieldset>
    </section>
  );
}

// Part is one holder's (or the pool's) slice of a stacked bar: its colour follows the entity, never its rank.
interface Part {
  key: string;
  name: string;
  colour: number;
  before?: string;
  after?: string;
  you?: boolean;
}

// partsOf are the holders and the pool as the chart and the table show them: holders in the server's order, each keeping
// one colour in both bars, the pool after them; past seven, the rest are Other.
function partsOf(o: RoundOutcome): { parts: Part[]; other: RoundHolder[] } {
  const held = o.holders.filter((h) => h.fully_diluted_before !== "0" || h.fully_diluted_after !== "0");
  const pool = o.unallocated_before !== "0" || o.unallocated_after !== "0";
  const room = pool ? 7 : 8;
  const shown = held.length > room ? held.slice(0, room - 1) : held;
  const other = held.length > room ? held.slice(room - 1) : [];
  const parts: Part[] = shown.map((h, i) => ({ key: h.holder_id, name: h.holder_name, colour: i + 1, before: h.diluted_before, after: h.diluted_after, you: h.you }));
  if (pool) parts.push({ key: "pool", name: "Option pool, not granted", colour: parts.length + 1, before: o.pool_diluted_before, after: o.pool_diluted_after });
  return { parts, other };
}

// Bar is one stacked bar of the parts' percentages, as the server computed them; Other takes what the parts leave.
function Bar({ parts, side, other, label }: { parts: Part[]; side: "before" | "after"; other: boolean; label: string }) {
  let x = 0;
  const shares = parts.map((p) => ({ key: p.key, colour: `fy-c${p.colour}`, share: Number(p[side] ?? "0") }));
  if (other) shares.push({ key: "other", colour: "fy-cother", share: Math.max(0, 100 - shares.reduce((n, s) => n + s.share, 0)) });
  const drawn = shares
    .filter((s) => s.share > 0)
    .map((s, i, all) => {
      const out = { ...s, x, width: Math.max(0, s.share * 10 - (i < all.length - 1 ? 3 : 0)) };
      x += s.share * 10;
      return out;
    });
  return (
    <svg className="fy-chart" viewBox="0 0 1000 10" preserveAspectRatio="none" style={{ height: 28, borderRadius: 4 }} role="img" aria-label={label}>
      {drawn.map((s) => (
        <rect key={s.key} className={`fy-bar ${s.colour}`} x={s.x} y={0} width={s.width} height={10} />
      ))}
    </svg>
  );
}

// Outcome is what the round gives (FR-EQ-21, FR-EQ-27): who would own what, before and after, fully diluted; in Basic
// what you would own and what the new investor gets; in Expert the price, the valuations, the new shares and the
// pro-forma cap table with each holder's points.
function Outcome({ tried, draft, expert }: { tried: { outcome?: RoundOutcome; problem?: ApiProblem; busy: boolean }; draft: RoundDraft | null; expert: boolean }) {
  const id = useId();
  const [view, setView] = useState<"chart" | "table">("chart");
  const o = tried.outcome;
  if (tried.problem) {
    const fields = (tried.problem.errors ?? []).filter((e) => e.message);
    return (
      <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-labelledby={`${id}-cant`}>
        <h2 className="fy-h3" id={`${id}-cant`}>
          <Icon of={TriangleAlert} /> The round cannot be worked out yet
        </h2>
        <ul className="fy-small fy-secondary" aria-live="polite">
          {fields.length === 0 && <li>{sentence(tried.problem.detail ?? "Fyniq could not model it")}</li>}
          {tried.problem.code && <li className="fy-muted">({tried.problem.code})</li>}
          {fields.map((e) => (
            <li key={`${e.field}-${e.message}`}>
              {fieldName(e.field ?? "")}: {e.message}. {e.fix ? sentence(e.fix) : ""}
            </li>
          ))}
        </ul>
      </section>
    );
  }
  if (!o) {
    return (
      <section className="fy-card fy-card--pad" aria-label="Who would own what">
        <p className="fy-small fy-muted" role="status">
          {tried.busy ? "Working it out…" : "Enter the value before the money (or a price per share), and who puts in how much, to see what everyone would own."}
        </p>
      </section>
    );
  }
  const { parts, other } = partsOf(o);
  const you = o.holders.find((h) => h.you);
  const words = (side: "before" | "after") =>
    `${side === "before" ? "Now" : "After the round"}: ` + parts.filter((p) => p[side]).map((p) => `${p.name} ${pct(p[side])}`).join(", ") + (other.length ? `, and ${other.length} others` : "") + ".";
  const newMoney = (holder: string) => o.issues.find((i) => i.holder_id === holder)?.amount;
  return (
    <>
      <section className="fy-card" aria-labelledby={`${id}-title`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-title`} title={helpText("round.outcome").tooltip}>
            Who would own what
          </h2>
          <div className="fy-segmented" role="radiogroup" aria-label="Show as">
            {(["chart", "table"] as const).map((k) => (
              <button key={k} type="button" role="radio" aria-checked={view === k} onClick={() => setView(k)}>
                {k === "chart" ? "Chart" : "Table"}
              </button>
            ))}
          </div>
        </div>
        <div className="fy-card__body fy-stack fy-stack--3" aria-busy={tried.busy}>
          {view === "chart" ? (
            <>
              <p className="fy-small fy-muted">Now, fully diluted · {count(o.fully_diluted_before)} shares</p>
              <Bar parts={parts} side="before" other={other.length > 0} label={words("before")} />
              <p className="fy-small fy-muted">After the round · {count(o.fully_diluted_after)} shares</p>
              <Bar parts={parts} side="after" other={other.length > 0} label={words("after")} />
              <div className="fy-legend">
                {parts.map((p) => (
                  <span key={p.key}>
                    <i className={`fy-legend__key fy-swatch-${p.colour}`} aria-hidden="true" />
                    {p.name}
                    {p.you ? " (you)" : ""}{" "}
                    <span className="fy-num fy-strong">{p.before ? `${p.before} → ${pct(p.after)}` : pct(p.after)}</span>
                  </span>
                ))}
                {other.length > 0 && (
                  <span>
                    <i className="fy-legend__key fy-swatch-other" aria-hidden="true" />
                    Other, {other.length} holders
                  </span>
                )}
              </div>
            </>
          ) : (
            <table className="fy-table fy-table--compact" aria-label="Who would own what">
              <thead>
                <tr>
                  <th scope="col">Holder</th>
                  <th scope="col" className="fy-num">
                    Now %
                  </th>
                  <th scope="col" className="fy-num">
                    After %
                  </th>
                </tr>
              </thead>
              <tbody>
                {[...o.holders.map((h) => ({ key: h.holder_id, name: h.holder_name + (h.you ? " (you)" : ""), before: h.diluted_before, after: h.diluted_after })),
                  ...(o.pool_diluted_before || o.pool_diluted_after ? [{ key: "pool", name: "Option pool, not granted", before: o.pool_diluted_before, after: o.pool_diluted_after }] : [])].map((r) => (
                  <tr key={r.key}>
                    <td>{r.name}</td>
                    <td className="fy-num">{r.before ?? "–"}</td>
                    <td className="fy-num">{r.after ?? "–"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </section>
      {!expert && (
        <div className="fy-grid fy-grid--2">
          {you && (
            <div className="fy-tile">
              <span className="fy-tile__label" title={helpText("round.you").tooltip}>
                <Icon of={UserRound} />
                You would own
              </span>
              <span className="fy-tile__value fy-num">
                {you.diluted_after ?? "–"}
                <small>%</small>
              </span>
              <span className="fy-tile__meta">
                {you.dilution && you.dilution !== "0.00" && (
                  <span className={`fy-delta ${you.dilution.startsWith("-") ? "fy-delta--up" : "fy-delta--down"}`}>
                    <Icon of={you.dilution.startsWith("-") ? ArrowUpRight : ArrowDownRight} />
                    {points(you.dilution)} points
                  </span>
                )}
                from {pct(you.diluted_before)} today
              </span>
              <span className="fy-tile__meta">Still {count(you.fully_diluted_after)} shares and options, fully diluted</span>
            </div>
          )}
          {o.issues[0] && (
            <div className="fy-tile">
              <span className="fy-tile__label">
                <Icon of={Handshake} />
                What the new money buys
              </span>
              <span className="fy-tile__value fy-num">{count(o.issues[0].shares)}</span>
              <span className="fy-tile__meta">
                shares for {o.issues[0].holder_name} at S${o.price} each for {sgd(o.issues[0].amount)}
              </span>
              {o.issues.length > 1 && <span className="fy-tile__meta">and {o.issues.length - 1} more investors: see them in Expert mode</span>}
              {o.conversions.map((c) => (
                <span key={c.instrument_id} className="fy-tile__meta">
                  {c.holder_name}'s SAFE or note becomes {count(c.shares)} shares at S${c.price}
                </span>
              ))}
            </div>
          )}
        </div>
      )}
      {expert && (
        <section className="fy-stack fy-stack--3" aria-labelledby={`${id}-pf`}>
          <div className="fy-grid fy-grid--2">
            <div className="fy-tile">
              <span className="fy-tile__label">Price per share</span>
              <span className="fy-tile__value fy-num">
                {o.price}
                <small>{o.currency}</small>
              </span>
              <span className="fy-tile__meta">
                {draft?.pre_money
                  ? `S$${formatAmount(draft.pre_money)} ÷ ${count(o.fully_diluted_before)} shares fully diluted${draft.top_up && draft.top_up_before ? ` and the top-up of ${count(draft.top_up)}` : ""}, cut down to ${draft.price_decimals} decimals`
                  : "As typed"}
              </span>
            </div>
            <div className="fy-tile">
              <span className="fy-tile__label">Post-money valuation</span>
              <span className="fy-tile__value fy-num">
                {formatAmount(o.post_money.amount)}
                <small>{o.currency}</small>
              </span>
              <span className="fy-tile__meta">
                Pre-money {sgd(o.pre_money)} · {sgd(o.raised)} new money
              </span>
            </div>
            <div className="fy-tile">
              <span className="fy-tile__label">New shares</span>
              <span className="fy-tile__value fy-num">{count(o.new_shares)}</span>
              <span className="fy-tile__meta">
                {o.issues.length} allotment{o.issues.length === 1 ? "" : "s"} for cash
                {o.conversions.length > 0 ? ` · ${o.conversions.length} conversion${o.conversions.length === 1 ? "" : "s"}` : ""}
              </span>
            </div>
            <div className="fy-tile">
              <span className="fy-tile__label">Option pool to grant</span>
              <span className="fy-tile__value fy-num">{count(o.unallocated_after)}</span>
              <span className="fy-tile__meta">From {count(o.unallocated_before)} before the round</span>
            </div>
          </div>
          <h2 className="fy-h3" id={`${id}-pf`} title={helpText("round.pro-forma").tooltip}>
            Pro-forma cap table, fully diluted
          </h2>
          <div className="fy-tablewrap">
            <table className="fy-table fy-statement" aria-labelledby={`${id}-pf`}>
              <thead>
                <tr>
                  <th scope="col">Holder</th>
                  <th scope="col" className="fy-num">
                    Shares now
                  </th>
                  <th scope="col" className="fy-num">
                    Now %
                  </th>
                  <th scope="col" className="fy-num">
                    Shares after
                  </th>
                  <th scope="col" className="fy-num">
                    After %
                  </th>
                  <th scope="col" className="fy-num">
                    Points
                  </th>
                  <th scope="col" className="fy-num">
                    New money ({o.currency})
                  </th>
                </tr>
              </thead>
              <tbody>
                {o.holders.map((h) => {
                  const m = newMoney(h.holder_id);
                  return (
                    <tr key={h.holder_id}>
                      <td style={{ whiteSpace: "nowrap" }}>
                        {h.holder_name}
                        {h.you ? " (you)" : ""}
                      </td>
                      <td className="fy-num">{h.fully_diluted_before === "0" ? "–" : count(h.fully_diluted_before)}</td>
                      <td className="fy-num">{h.diluted_before ?? "–"}</td>
                      <td className="fy-num">{count(h.fully_diluted_after)}</td>
                      <td className="fy-num">{h.diluted_after ?? "–"}</td>
                      <td className="fy-num">{points(h.dilution)}</td>
                      <td className="fy-num">{m ? formatAmount(m.amount) : "–"}</td>
                    </tr>
                  );
                })}
                {(o.unallocated_before !== "0" || o.unallocated_after !== "0") && (
                  <tr>
                    <td style={{ whiteSpace: "nowrap" }}>Option pool, not granted</td>
                    <td className="fy-num">{count(o.unallocated_before)}</td>
                    <td className="fy-num">{o.pool_diluted_before ?? "–"}</td>
                    <td className="fy-num">{count(o.unallocated_after)}</td>
                    <td className="fy-num">{o.pool_diluted_after ?? "–"}</td>
                    <td className="fy-num">{points(o.pool_dilution)}</td>
                    <td className="fy-num">–</td>
                  </tr>
                )}
              </tbody>
              <tfoot>
                <tr>
                  <td>Total</td>
                  <td className="fy-num">{count(o.fully_diluted_before)}</td>
                  <td className="fy-num" />
                  <td className="fy-num">{count(o.fully_diluted_after)}</td>
                  <td className="fy-num" />
                  <td className="fy-num" />
                  <td className="fy-num">{formatAmount(o.raised.amount)}</td>
                </tr>
              </tfoot>
            </table>
          </div>
          <p className="fy-small fy-muted">
            Shares for cash are whole shares only (RND-SHARE):{" "}
            {o.issues
              .map((i) => `${sgd({ amount: i.amount.amount, currency: i.amount.currency })} buys ${i.holder_name} ${count(i.shares)}${i.left.amount !== "0.00" && i.left.amount !== "0" ? `, and ${sgd(i.left)} of their money buys no whole share` : ""}`)
              .join("; ")}
            . Points are the change in each part fully diluted, worked out exactly and shown to 2 decimals.
          </p>
        </section>
      )}
    </>
  );
}

// SideBySide compares the saved scenarios (FR-EQ-21): the value before the money, the money raised, the price, the
// top-up and each holder's part fully diluted after, as the server worked each out against today's books.
function SideBySide({ saved, mayDraft }: { saved: RoundScenario[]; mayDraft: boolean }) {
  const id = useId();
  const shown = saved.filter((s) => s.outcome).slice(0, 4);
  const names: { key: string; name: string; you: boolean }[] = [];
  for (const s of shown) {
    for (const h of s.outcome?.holders ?? []) {
      if (!names.some((n) => n.key === h.holder_id)) names.push({ key: h.holder_id, name: h.holder_name, you: Boolean(h.you) });
    }
  }
  names.sort((a, b) => Number(b.you) - Number(a.you));
  const cell = (s: RoundScenario, holder: string) => {
    const h = s.outcome?.holders.find((x) => x.holder_id === holder);
    return h ? pct(h.diluted_after) : "–";
  };
  return (
    <section className="fy-card" aria-labelledby={`${id}-title`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-title`} title={helpText("round.side-by-side").tooltip}>
          Side by side
        </h2>
      </div>
      {shown.length === 0 ? (
        <p className="fy-card__body fy-small fy-muted">
          {mayDraft ? "Save a scenario to compare it here with others." : "No scenario is saved yet. An Accountant saves the ones worth comparing."}
        </p>
      ) : (
        <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
          <table className="fy-table fy-table--compact" aria-labelledby={`${id}-title`}>
            <thead>
              <tr>
                <td />
                {shown.map((s) => (
                  <th key={s.id} scope="col" className="fy-num">
                    {s.name}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              <tr>
                <th scope="row">Value before the money</th>
                {shown.map((s) => (
                  <td key={s.id} className="fy-num">
                    {s.outcome ? formatAmount(s.outcome.pre_money.amount) : "–"}
                  </td>
                ))}
              </tr>
              <tr>
                <th scope="row">Money raised</th>
                {shown.map((s) => (
                  <td key={s.id} className="fy-num">
                    {s.outcome ? formatAmount(s.outcome.raised.amount) : "–"}
                  </td>
                ))}
              </tr>
              <tr>
                <th scope="row">Price per share</th>
                {shown.map((s) => (
                  <td key={s.id} className="fy-num">
                    {s.outcome?.price ?? "–"}
                  </td>
                ))}
              </tr>
              <tr>
                <th scope="row">Pool top-up</th>
                {shown.map((s) => (
                  <td key={s.id} className="fy-num">
                    {s.top_up ? `${count(s.top_up)} ${s.top_up_before ? "before" : "after"}` : "0"}
                  </td>
                ))}
              </tr>
              {names.map((n) => (
                <tr key={n.key}>
                  <th scope="row" className={n.you ? "fy-strong" : undefined}>
                    {n.you ? "You" : n.name}
                  </th>
                  {shown.map((s) => (
                    <td key={s.id} className={`fy-num${n.you ? " fy-strong" : ""}`}>
                      {cell(s, n.key)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
