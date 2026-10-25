import { useInfiniteQuery, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearch } from "@tanstack/react-router";
import { ArrowDownLeft, ArrowLeftRight, ArrowUpRight, ChevronLeft, ChevronRight, CircleCheck, Copy, Files, PartyPopper, Split, Table, TriangleAlert, Wand2 } from "lucide-react";
import { useId, useState } from "react";
import { api, etagOf } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { negate, sign } from "../../format/decimal";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { BankTabs, makers } from "./BankAccounts";
import { acceptRequest, found, rule, send, suggestionWords, type Line, type Suggestion } from "./lineActions";
import { CategoriseDialog, MatchDialog, TransferDialog } from "./LineDialogs";

type Account = components["schemas"]["MoneyAccount"];
type Dialogs = { kind: "change" | "split" | "transfer" | "match"; line: Line; etag: string } | null;

// accept accepts a suggestion for a line: the rule's action is read first when it is a rule. Basic's card and Expert's
// grid both call it, so the same action sends the same request in both modes (FR-UX-24, QA-UI-04).
export async function accept(line: Line, etag: string, s: Suggestion) {
  const r = acceptRequest(line, s, s.kind === "categorised" && s.rule_id ? await rule(s.rule_id) : undefined);
  if (!r) throw new Error("the suggestion cannot be accepted as it is");
  return send(line, etag, r);
}

function useDetail(id: string | undefined) {
  return useQuery({
    queryKey: ["bank-line", id],
    enabled: Boolean(id),
    queryFn: async () => {
      const { data, error, response } = await api.GET("/v1/bank-lines/{line_id}", { params: { path: { line_id: id! } } });
      if (error) throw error;
      return { line: data, etag: etagOf(response) };
    },
  });
}

// Categorise is Banking › Categorise (docs/design/screens CategoriseCards and CategoriseGrid; FR-BNK-04…09, FR-UX-24;
// p1-bank-import-reconcile task 10.3): the waiting lines, one at a time in Basic, as a grid in Expert's grid view.
// Suggestions are never applied by themselves: a person accepts one, changes it or skips. Reconcile now runs the
// system's exact matching over the lines that arrived by feed (FR-BNK-14, rev 0.143).
export function Categorise() {
  const { session, mode } = useSignedIn();
  const search = useSearch({ strict: false }) as { view?: string; account?: string };
  const may = (session.roles ?? []).some((r) => makers.includes(r));
  const queries = useQueryClient();
  const [account, setAccount] = useState(search.account ?? "");
  const [done, setDone] = useState(0);
  const [skipped, setSkipped] = useState<string[]>([]);
  const [dialog, setDialog] = useState<Dialogs>(null);
  const [notice, setNotice] = useState("");
  const accounts = useQuery({
    queryKey: ["money-accounts", "all"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/money-accounts", { params: { query: { page_size: 100 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const waiting = useInfiniteQuery({
    queryKey: ["bank-lines", "waiting"],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET("/v1/bank-lines", { params: { query: { status: "waiting", page_size: 100, cursor: pageParam } } });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.next_cursor,
  });
  const all = waiting.data?.pages.flatMap((p) => p.items ?? []) ?? [];
  const lines = all.filter((l) => !account || l.money_account_id === account);
  const names = new Map((accounts.data ?? []).map((a) => [a.id, a]));
  const grid = mode === "expert" && search.view === "grid";
  const refresh = () => {
    for (const key of [["bank-lines"], ["money-accounts"], ["bank-line"], ["cash-position"], ["bank-signals"]]) void queries.invalidateQueries({ queryKey: key });
  };
  const acted = (n = 1) => {
    setDone((d) => d + n);
    setDialog(null);
    refresh();
  };
  const h = helpText("banking.categorise");

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Banking</span>
          <h1 className="fy-h1">Categorise</h1>
          <p className="fy-secondary">{grid ? "Work the queue in bulk: accept, split, transfer and match." : "One line at a time: accept the suggestion, change it, or skip for now."}</p>
        </div>
        <div className="fy-pagehead__actions">
          {may && (
            <ReconcileNow
              account={account}
              onDone={(n, words) => {
                setNotice(words);
                if (n > 0) acted(n);
              }}
            />
          )}
          {mode === "expert" &&
            (grid ? (
              <Link className="fy-btn fy-btn--ghost" to="/banking/categorise">
                One line at a time
              </Link>
            ) : (
              <Link className="fy-btn fy-btn--ghost" to="/banking/categorise" search={{ view: "grid" }} title={helpText("banking.categorise-grid").tooltip}>
                <Icon of={Table} />
                Grid view
              </Link>
            ))}
        </div>
      </div>
      <ReviewBanner screen={grid ? "categorise-expert" : "categorise-basic"} />
      <BankTabs />
      {notice && (
        <p className="fy-small fy-row gap-2" role="status">
          <Icon of={Wand2} />
          {notice}
        </p>
      )}
      <div className="fy-stack fy-stack--2">
        <div className="fy-row fy-row--between fy-row--wrap">
          <span className="fy-strong" title={h.tooltip}>
            {done} done · {lines.length} {lines.length === 1 ? "line waits" : "lines wait"}
          </span>
        </div>
        <div className="fy-chipset" role="group" aria-label="Account">
          <button type="button" className="fy-filter" aria-pressed={account === ""} onClick={() => setAccount("")}>
            All accounts · {all.length}
          </button>
          {(accounts.data ?? [])
            .filter((a) => all.some((l) => l.money_account_id === a.id))
            .map((a) => (
              <button key={a.id} type="button" className="fy-filter" aria-pressed={account === a.id} onClick={() => setAccount(a.id)}>
                {a.name} · {all.filter((l) => l.money_account_id === a.id).length}
              </button>
            ))}
        </div>
      </div>
      {waiting.isError && <Refusal problem={problemOf(waiting.error)} title="The waiting lines could not be listed" />}
      {waiting.isPending && <p className="fy-secondary">Loading the waiting lines…</p>}
      {waiting.data && lines.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={PartyPopper} size={24} />
          <h2 className="fy-h3">All caught up</h2>
          <p className="fy-secondary">No line waits{account ? " on this account" : ""}. New lines arrive with each statement, report or sync.</p>
        </div>
      )}
      {lines.length > 0 &&
        (grid ? (
          <Grid lines={lines} names={names} may={may} onDialog={setDialog} onDone={acted} />
        ) : (
          <Cards lines={lines.filter((l) => !skipped.includes(l.id)).concat(lines.filter((l) => skipped.includes(l.id)))} names={names} may={may} expert={mode === "expert"} onDialog={setDialog} onDone={() => acted()} onSkip={(id) => setSkipped([...skipped.filter((x) => x !== id), id])} />
        ))}
      {waiting.hasNextPage && (
        <div>
          <button type="button" className="fy-btn fy-btn--secondary" onClick={() => void waiting.fetchNextPage()} disabled={waiting.isFetchingNextPage}>
            Show more lines
          </button>
        </div>
      )}
      {dialog?.kind === "change" && <CategoriseDialog line={dialog.line} etag={dialog.etag} split={false} onClose={() => setDialog(null)} onDone={() => acted()} />}
      {dialog?.kind === "split" && <CategoriseDialog line={dialog.line} etag={dialog.etag} split onClose={() => setDialog(null)} onDone={() => acted()} />}
      {dialog?.kind === "match" && <MatchDialog line={dialog.line} etag={dialog.etag} onClose={() => setDialog(null)} onDone={() => acted()} />}
      {dialog?.kind === "transfer" && <TransferDialog line={dialog.line} accounts={accounts.data ?? []} waiting={all} onClose={() => setDialog(null)} onDone={() => acted()} />}
    </div>
  );
}

// ReconcileNow runs auto-reconcile over the waiting lines as the system principal, within the organisation's limits
// (FR-BNK-14; rev 0.143): what is left stays, counted by why.
function ReconcileNow({ account, onDone }: { account: string; onDone: (reconciled: number, words: string) => void }) {
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("banking.reconcile-now");
  const why: Record<string, string> = { no_match: "no exact match", ambiguous: "more than one match", line_cap: "over the line limit", rule_cap: "over a rule's daily limit", no_rate: "no rate", refused: "refused" };
  const go = async () => {
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/bank-lines:auto-reconcile", { body: { money_account_id: account || undefined } });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    const left = Object.entries(out.data.left ?? {})
      .filter(([, n]) => n > 0)
      .map(([k, n]) => `${n} ${why[k] ?? k}`);
    const n = out.data.reconciled;
    onDone(n, `${n === 1 ? "1 line" : `${n} lines`} reconciled by Fyniq itself.${left.length ? ` Left for you: ${left.join(", ")}.` : ""}`);
  };
  return (
    <>
      <button type="button" className="fy-btn fy-btn--secondary" onClick={() => void go()} disabled={busy} title={h.tooltip}>
        <Icon of={Wand2} />
        {h.label}
      </button>
      {problem && <Refusal problem={problem} title="Reconcile now did not run" />}
    </>
  );
}

function LineHead({ line, account }: { line: Line; account?: Account }) {
  const incoming = sign(line.amount.amount) > 0;
  return (
    <div className="fy-stack fy-stack--2">
      <span className="fy-tile__label fy-row gap-2">
        <Icon of={incoming ? ArrowDownLeft : ArrowUpRight} />
        {incoming ? "Money in" : "Money out"}
      </span>
      <span className="fy-tile__value fy-num">
        {formatAmount(incoming ? line.amount.amount : negate(line.amount.amount))}
        <small>{line.amount.currency}</small>
      </span>
      <p className="fy-mono fy-plain">{line.description ?? ""}</p>
      <span className="fy-small fy-muted">
        {[line.counterparty, line.reference].filter(Boolean).join(" · ")}
        {line.counterparty || line.reference ? " · " : ""}
        {account?.name ?? "A money account"} · {date(line.booking_date)}. The description is as the bank sent it.
      </span>
    </div>
  );
}

function Flags({ line }: { line: Line }) {
  if (line.status === "possible_duplicate")
    return (
      <div className="fy-banner fy-banner--warning" role="note">
        <Icon of={Copy} size={20} />
        <div className="fy-banner__text">
          <span className="fy-strong">It may be a line already held</span>
          <span className="fy-small">
            It looks like another line without the bank's reference or a running balance to prove it.{" "}
            <Link to="/banking/accounts/$accountId" params={{ accountId: line.money_account_id }}>
              Compare them on the account
            </Link>{" "}
            before you categorise it.
          </span>
        </div>
      </div>
    );
  if (line.status === "suspense")
    return (
      <div className="fy-banner fy-banner--warning" role="note">
        <Icon of={TriangleAlert} size={20} />
        <div className="fy-banner__text">
          <span className="fy-strong">In suspense</span>
          <span className="fy-small">It is posted to suspense (2999) until you categorise or match it; the close warns while suspense is not nil.</span>
        </div>
      </div>
    );
  return null;
}

// Cards is Basic's queue: one line at a time, its suggestions best first, accept, change or skip.
function Cards({
  lines,
  names,
  may,
  expert,
  onDialog,
  onDone,
  onSkip,
}: {
  lines: Line[];
  names: Map<string, Account>;
  may: boolean;
  expert: boolean;
  onDialog: (d: Dialogs) => void;
  onDone: () => void;
  onSkip: (id: string) => void;
}) {
  const id = useId();
  const [at, setAt] = useState(0);
  const i = Math.min(at, lines.length - 1);
  const current = lines[i]!;
  const detail = useDetail(current.id);
  const [picked, setPicked] = useState<number>(0);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const suggestions = detail.data?.line.suggestions ?? [];
  const chosen = suggestions[Math.min(picked, suggestions.length - 1)];
  const a = helpText("bank-line.accept");
  const go = async () => {
    if (!chosen || !detail.data) return;
    setBusy(true);
    setProblem(null);
    try {
      const out = await accept(detail.data.line, detail.data.etag, chosen);
      if (!out.data) setProblem(problemOf(out.error));
      else {
        setPicked(0);
        onDone();
      }
    } finally {
      setBusy(false);
    }
  };
  const move = (to: number) => {
    setAt(Math.max(0, Math.min(lines.length - 1, to)));
    setPicked(0);
    setProblem(null);
  };
  return (
    <section className="fy-card" aria-labelledby={`${id}-title`}>
      <div className="fy-card__head">
        <div className="fy-grow fy-stack gap-0">
          <h2 className="fy-h3" id={`${id}-title`}>
            {names.get(current.money_account_id)?.name ?? "A money account"} · {date(current.booking_date)}
          </h2>
          <span className="fy-small fy-muted">
            Line {i + 1} of {lines.length}
          </span>
        </div>
        <button type="button" className="fy-iconbtn" aria-label="Previous line" disabled={i === 0} onClick={() => move(i - 1)}>
          <Icon of={ChevronLeft} />
        </button>
        <button type="button" className="fy-iconbtn" aria-label="Next line" disabled={i === lines.length - 1} onClick={() => move(i + 1)}>
          <Icon of={ChevronRight} />
        </button>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <LineHead line={current} account={names.get(current.money_account_id)} />
        <Flags line={current} />
        {problem && <Refusal problem={problem} title="The line was not reconciled" />}
        {detail.isError && <Refusal problem={problemOf(detail.error)} title="The line could not be read" />}
        <fieldset className="fy-fieldset fy-stack fy-stack--2">
          <legend className="fy-h3">What is this?</legend>
          {detail.data && suggestions.length === 0 && <p className="fy-secondary">No suggestion: nothing open matches it and no rule meets it. Change it to choose its category.</p>}
          {suggestions.map((s, n) => (
            <button key={`${s.kind}-${s.document_id ?? s.rule_id}`} className="fy-choice" type="button" role="radio" aria-checked={n === picked} onClick={() => setPicked(n)}>
              <span className="fy-radio" />
              <span className="fy-stack fy-grow gap-1">
                <span className="fy-strong">{suggestionWords(current, s)}</span>
                <span className="fy-chip fy-chip--info">
                  <Icon of={CircleCheck} />
                  {found[s.found]}
                </span>
              </span>
            </button>
          ))}
        </fieldset>
        {!expert && <p className="fy-small fy-muted">You can split a line over several accounts, or mark it as a transfer, in Expert mode.</p>}
      </div>
      {may && (
        <div className="fy-card__foot fy-row fy-row--wrap gap-2">
          <button type="button" className="fy-btn fy-btn--primary" onClick={() => void go()} disabled={busy || !chosen} title={a.tooltip}>
            {a.label}
          </button>
          <button type="button" className="fy-btn fy-btn--secondary" disabled={!detail.data} onClick={() => detail.data && onDialog({ kind: "change", line: detail.data.line, etag: detail.data.etag })}>
            Change
          </button>
          {expert && detail.data && <ExpertActions line={detail.data.line} etag={detail.data.etag} onDialog={onDialog} onDone={onDone} onProblem={setProblem} />}
          <button
            type="button"
            className="fy-btn fy-btn--ghost"
            onClick={() => {
              onSkip(current.id);
              setPicked(0);
            }}
          >
            Skip
          </button>
        </div>
      )}
    </section>
  );
}

// ExpertActions are the line's Expert actions: split it, mark it as a transfer, match it to documents, or put it in
// suspense until its purpose is known.
function ExpertActions({ line, etag, onDialog, onDone, onProblem }: { line: Line; etag: string; onDialog: (d: Dialogs) => void; onDone: () => void; onProblem: (p: ApiProblem | null) => void }) {
  const [busy, setBusy] = useState(false);
  const suspend = async () => {
    setBusy(true);
    onProblem(null);
    const out = await send(line, etag, { action: "suspend" });
    setBusy(false);
    if (!out.data) return onProblem(problemOf(out.error));
    onDone();
  };
  return (
    <>
      <button type="button" className="fy-btn fy-btn--ghost" onClick={() => onDialog({ kind: "split", line, etag })}>
        <Icon of={Split} />
        Split
      </button>
      <button type="button" className="fy-btn fy-btn--ghost" onClick={() => onDialog({ kind: "transfer", line, etag })}>
        <Icon of={ArrowLeftRight} />
        Transfer
      </button>
      <button type="button" className="fy-btn fy-btn--ghost" onClick={() => onDialog({ kind: "match", line, etag })}>
        <Icon of={Files} />
        Match
      </button>
      {line.status !== "suspense" && (
        <button type="button" className="fy-btn fy-btn--ghost" onClick={() => void suspend()} disabled={busy} title={helpText("bank-line.suspense").tooltip}>
          <Icon of={TriangleAlert} />
          Suspense
        </button>
      )}
    </>
  );
}

// Grid is Expert's grid view: every waiting line with its best suggestion; accept one, or the selection at once, each
// line in its own request; open a line's other actions.
function Grid({ lines, names, may, onDialog, onDone }: { lines: Line[]; names: Map<string, Account>; may: boolean; onDialog: (d: Dialogs) => void; onDone: (n: number) => void }) {
  const [selected, setSelected] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const details = useQueries({
    queries: lines.map((l) => ({
      queryKey: ["bank-line", l.id],
      queryFn: async () => {
        const { data, error, response } = await api.GET("/v1/bank-lines/{line_id}", { params: { path: { line_id: l.id } } });
        if (error) throw error;
        return { line: data, etag: etagOf(response) };
      },
    })),
  });
  const byId = new Map(details.flatMap((d) => (d.data ? [[d.data.line.id, d.data] as const] : [])));
  const run = async (ids: string[]) => {
    setBusy(true);
    setProblem(null);
    let ok = 0;
    const refused: string[] = [];
    for (const id of ids) {
      const d = byId.get(id);
      const top = d?.line.suggestions[0];
      if (!d || !top) {
        refused.push("no suggestion");
        continue;
      }
      try {
        const out = await accept(d.line, d.etag, top);
        if (out.data) ok++;
        else {
          const p = problemOf(out.error);
          refused.push(p.code ?? "refused");
          if (ids.length === 1) setProblem(p);
        }
      } catch {
        refused.push("no suggestion");
      }
    }
    setBusy(false);
    setSelected([]);
    setResult(`${ok === 1 ? "1 line" : `${ok} lines`} reconciled.${refused.length ? ` ${refused.length} left: ${[...new Set(refused)].join(", ")}.` : ""}`);
    if (ok > 0) onDone(ok);
  };
  const all = lines.length > 0 && selected.length === lines.length;
  return (
    <div className="fy-stack fy-stack--3">
      {problem && <Refusal problem={problem} title="The line was not reconciled" />}
      {result && (
        <p className="fy-small" role="status">
          {result}
        </p>
      )}
      {may && (
        <div className="fy-row gap-2">
          <button type="button" className="fy-btn fy-btn--primary" disabled={busy || selected.length === 0} onClick={() => void run(selected)}>
            Accept the selected suggestions ({selected.length})
          </button>
        </div>
      )}
      <div className="fy-tablewrap">
        <table className="fy-table" aria-label="Waiting lines">
          <thead>
            <tr>
              <th scope="col">
                <input type="checkbox" aria-label="Select every line" checked={all} onChange={(e) => setSelected(e.target.checked ? lines.map((l) => l.id) : [])} />
              </th>
              <th scope="col">Booked</th>
              <th scope="col">Account</th>
              <th scope="col">Description</th>
              <th scope="col" className="fy-num">
                Amount
              </th>
              <th scope="col">Suggestion</th>
              <th scope="col">Actions</th>
            </tr>
          </thead>
          <tbody>
            {lines.map((l) => {
              const d = byId.get(l.id);
              const top = d?.line.suggestions[0];
              return (
                <tr key={l.id}>
                  <td>
                    <input
                      type="checkbox"
                      aria-label={`Select the line of ${date(l.booking_date)}, ${formatAmount(l.amount.amount)}`}
                      checked={selected.includes(l.id)}
                      onChange={(e) => setSelected(e.target.checked ? [...selected, l.id] : selected.filter((x) => x !== l.id))}
                    />
                  </td>
                  <td>{date(l.booking_date)}</td>
                  <td>{names.get(l.money_account_id)?.name ?? ""}</td>
                  <td>
                    <span className="fy-mono">{l.description ?? ""}</span>
                    {l.status === "possible_duplicate" && (
                      <span className="fy-chip fy-chip--warning">
                        <Icon of={Copy} />
                        Possible duplicate
                      </span>
                    )}
                    {l.status === "suspense" && (
                      <span className="fy-chip fy-chip--warning">
                        <Icon of={TriangleAlert} />
                        In suspense
                      </span>
                    )}
                  </td>
                  <td className="fy-num">{formatAmount(l.amount.amount)}</td>
                  <td>{d ? top ? suggestionWords(l, top) : "None" : "…"}</td>
                  <td>
                    {may && d && (
                      <span className="fy-row fy-row--wrap gap-1">
                        <button type="button" className="fy-btn fy-btn--secondary fy-btn--sm" disabled={busy || !top} onClick={() => void run([l.id])} aria-label={`Accept the suggestion for the line of ${date(l.booking_date)}`}>
                          Accept
                        </button>
                        <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm" onClick={() => onDialog({ kind: "change", line: d.line, etag: d.etag })}>
                          Change
                        </button>
                        <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm" onClick={() => onDialog({ kind: "split", line: d.line, etag: d.etag })}>
                          Split
                        </button>
                        <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm" onClick={() => onDialog({ kind: "transfer", line: d.line, etag: d.etag })}>
                          Transfer
                        </button>
                        <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm" onClick={() => onDialog({ kind: "match", line: d.line, etag: d.etag })}>
                          Match
                        </button>
                      </span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <p className="fy-small fy-muted">Each line posts in its own request: one refused never stops the others. Suggestions are never applied by themselves.</p>
    </div>
  );
}
